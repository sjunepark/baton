package workitem

import (
	"fmt"
	"sort"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

const (
	TransitionActionAddLabels             = "add_labels"
	TransitionActionCloseIssue            = "close_issue"
	TransitionActionRemoveLabel           = "remove_label"
	TransitionActionAppendBaseIntegration = "append_base_integration"
	TransitionActionCommitPromotionCursor = "commit_promotion_cursor"
)

// PullRequestEvent contains the immutable event facts used to plan work-item
// transitions. Revision fields are retained so the applying workflow can bind
// the reviewed plan to current GitHub state without changing planner output.
type PullRequestEvent struct {
	Repository             string                   `json:"repository"`
	Action                 string                   `json:"action"`
	Number                 int                      `json:"number"`
	Title                  string                   `json:"title"`
	Body                   string                   `json:"body"`
	BaseRef                string                   `json:"baseRef"`
	HeadRef                string                   `json:"headRef"`
	BaseRepositoryFullName string                   `json:"baseRepositoryFullName"`
	HeadRepositoryFullName string                   `json:"headRepositoryFullName"`
	BaseSHA                string                   `json:"baseSha,omitempty"`
	HeadSHA                string                   `json:"headSha,omitempty"`
	MergeRevision          string                   `json:"mergeRevision,omitempty"`
	State                  string                   `json:"state,omitempty"`
	Merged                 bool                     `json:"merged"`
	DeliveryRecordDigest   string                   `json:"deliveryRecordDigest,omitempty"`
	IssueReferences        []DeliveryIssueReference `json:"issueReferences,omitempty"`
	Promotion              *PromotionTransition     `json:"promotion,omitempty"`
}

type DeliveryIssueReference struct {
	Number          int    `json:"number"`
	NodeID          string `json:"nodeId"`
	OwnershipDigest string `json:"ownershipDigest"`
}

type PromotionTransition struct {
	Committed             bool                     `json:"committed"`
	PlanDigest            string                   `json:"planDigest"`
	CursorDigest          string                   `json:"cursorDigest"`
	BaseIntegrationDigest string                   `json:"baseIntegrationDigest"`
	IssueReferences       []DeliveryIssueReference `json:"issueReferences"`
}

type TransitionOperation struct {
	ID              string `json:"id"`
	IssueNumber     int    `json:"issueNumber,omitempty"`
	Action          string `json:"action"`
	Label           string `json:"label,omitempty"`
	IssueNodeID     string `json:"issueNodeId,omitempty"`
	OwnershipDigest string `json:"ownershipDigest,omitempty"`
	RecordKind      string `json:"recordKind,omitempty"`
	RecordDigest    string `json:"recordDigest,omitempty"`
	CursorDigest    string `json:"cursorDigest,omitempty"`
}

type TransitionPlan struct {
	SchemaVersion         int                   `json:"schemaVersion"`
	Kind                  string                `json:"kind"`
	Repository            string                `json:"repository"`
	EventAction           string                `json:"eventAction"`
	PullRequestNumber     int                   `json:"pullRequestNumber"`
	Flow                  policy.PRFlow         `json:"flow"`
	Operations            []TransitionOperation `json:"operations"`
	Warnings              []string              `json:"warnings"`
	DeliveryRecordDigest  string                `json:"deliveryRecordDigest,omitempty"`
	PromotionPlanDigest   string                `json:"promotionPlanDigest,omitempty"`
	PromotionCursorDigest string                `json:"promotionCursorDigest,omitempty"`
	BaseIntegrationDigest string                `json:"baseIntegrationDigest,omitempty"`
}

// PlanPullRequestTransition plans the single persisted work-item transition:
// a merged work PR marks each referenced issue as awaiting review. Other
// lifecycle states are derived from GitHub facts and need no mutation.
func PlanPullRequestTransition(event PullRequestEvent, cfg config.Config) TransitionPlan {
	flow := policy.ClassifyPullRequestFlow(policy.PullRequest{
		Number:                 event.Number,
		Title:                  event.Title,
		Body:                   event.Body,
		BaseRef:                event.BaseRef,
		HeadRef:                event.HeadRef,
		BaseRepositoryFullName: event.BaseRepositoryFullName,
		HeadRepositoryFullName: event.HeadRepositoryFullName,
	}, cfg)
	plan := TransitionPlan{
		SchemaVersion:        4,
		Kind:                 "workItemTransitionPlan",
		Repository:           event.Repository,
		EventAction:          event.Action,
		PullRequestNumber:    event.Number,
		Flow:                 flow,
		Operations:           []TransitionOperation{},
		Warnings:             []string{},
		DeliveryRecordDigest: event.DeliveryRecordDigest,
	}
	if event.Action != "closed" || !event.Merged {
		return plan
	}
	if flow == policy.PRFlowPromotion {
		return planPromotionTransition(plan, event, cfg)
	}
	if flow != policy.PRFlowWork {
		return plan
	}

	if event.DeliveryRecordDigest == "" || len(event.IssueReferences) == 0 {
		plan.Warnings = append(plan.Warnings, "merged work PR has no committed delivery record with managed issue relationships")
		return plan
	}
	references := append([]DeliveryIssueReference(nil), event.IssueReferences...)
	sort.Slice(references, func(i, j int) bool { return references[i].Number < references[j].Number })
	seen := map[int]struct{}{}
	for _, reference := range references {
		if reference.Number <= 0 || reference.NodeID == "" || reference.OwnershipDigest == "" {
			plan.Warnings = append(plan.Warnings, "merged work PR has an incomplete delivery issue reference")
			plan.Operations = []TransitionOperation{}
			return plan
		}
		if _, duplicate := seen[reference.Number]; duplicate {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("merged work PR has duplicate delivery issue #%d", reference.Number))
			plan.Operations = []TransitionOperation{}
			return plan
		}
		seen[reference.Number] = struct{}{}
		plan.Operations = append(plan.Operations, TransitionOperation{
			ID:              fmt.Sprintf("issue-%d-awaiting-review", reference.Number),
			IssueNumber:     reference.Number,
			Action:          TransitionActionAddLabels,
			Label:           cfg.IssuePolicy.AwaitingReviewLabel,
			IssueNodeID:     reference.NodeID,
			OwnershipDigest: reference.OwnershipDigest,
		})
	}
	return plan
}

func planPromotionTransition(plan TransitionPlan, event PullRequestEvent, cfg config.Config) TransitionPlan {
	facts := event.Promotion
	if facts == nil {
		plan.Warnings = append(plan.Warnings, "merged promotion has no exact sealed delivery plan")
		return plan
	}
	plan.PromotionPlanDigest = facts.PlanDigest
	plan.PromotionCursorDigest = facts.CursorDigest
	plan.BaseIntegrationDigest = facts.BaseIntegrationDigest
	if facts.PlanDigest == "" || facts.CursorDigest == "" || facts.BaseIntegrationDigest == "" {
		plan.Warnings = append(plan.Warnings, "merged promotion delivery commitment is incomplete")
		return plan
	}
	if facts.Committed {
		return plan
	}
	references := append([]DeliveryIssueReference(nil), facts.IssueReferences...)
	sort.Slice(references, func(i, j int) bool { return references[i].Number < references[j].Number })
	seen := map[int]struct{}{}
	for _, reference := range references {
		if reference.Number <= 0 || reference.NodeID == "" || reference.OwnershipDigest == "" {
			plan.Warnings = append(plan.Warnings, "merged promotion has an incomplete delivery issue reference")
			plan.Operations = []TransitionOperation{}
			return plan
		}
		if _, duplicate := seen[reference.Number]; duplicate {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("merged promotion has duplicate delivery issue #%d", reference.Number))
			plan.Operations = []TransitionOperation{}
			return plan
		}
		seen[reference.Number] = struct{}{}
		plan.Operations = append(plan.Operations,
			TransitionOperation{
				ID: fmt.Sprintf("issue-%d-delivered", reference.Number), IssueNumber: reference.Number,
				Action: TransitionActionCloseIssue, IssueNodeID: reference.NodeID, OwnershipDigest: reference.OwnershipDigest,
			},
			TransitionOperation{
				ID: fmt.Sprintf("issue-%d-awaiting-review-index", reference.Number), IssueNumber: reference.Number,
				Action: TransitionActionRemoveLabel, Label: cfg.IssuePolicy.AwaitingReviewLabel,
				IssueNodeID: reference.NodeID, OwnershipDigest: reference.OwnershipDigest,
			},
		)
	}
	plan.Operations = append(plan.Operations,
		TransitionOperation{
			ID: "promotion-base-integration", Action: TransitionActionAppendBaseIntegration,
			RecordKind: "baseIntegration", RecordDigest: facts.BaseIntegrationDigest,
		},
		TransitionOperation{
			ID: "promotion-cursor-commit", Action: TransitionActionCommitPromotionCursor,
			CursorDigest: facts.CursorDigest,
		},
	)
	return plan
}
