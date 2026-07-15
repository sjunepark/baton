package workitem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

func TestPlanPullRequestTransitionLifecycleEdges(t *testing.T) {
	cfg := config.DefaultConfig()
	work := PullRequestEvent{
		Repository:             "example/repo",
		Action:                 "closed",
		Number:                 42,
		Title:                  "Implement queue Refs #9, #3",
		Body:                   "Refs #9 and #7",
		BaseRef:                cfg.Repository.StagingBranch,
		HeadRef:                cfg.Repository.WorkBranchPrefix + "queue",
		BaseRepositoryFullName: "example/repo",
		HeadRepositoryFullName: "example/repo",
		Merged:                 true,
		DeliveryRecordDigest:   "sha256:record",
		IssueReferences: []DeliveryIssueReference{
			{Number: 3, NodeID: "I_3", OwnershipDigest: "sha256:3"},
			{Number: 7, NodeID: "I_7", OwnershipDigest: "sha256:7"},
			{Number: 9, NodeID: "I_9", OwnershipDigest: "sha256:9"},
		},
	}
	tests := []struct {
		name           string
		event          PullRequestEvent
		wantFlow       policy.PRFlow
		wantOperations int
		wantWarnings   int
	}{
		{name: "merged closed work PR", event: work, wantFlow: policy.PRFlowWork, wantOperations: 3},
		{name: "closed unmerged work PR", event: mutateEvent(work, func(event *PullRequestEvent) { event.Merged = false }), wantFlow: policy.PRFlowWork},
		{name: "reopened work PR", event: mutateEvent(work, func(event *PullRequestEvent) { event.Action = "reopened" }), wantFlow: policy.PRFlowWork},
		{name: "opened work PR", event: mutateEvent(work, func(event *PullRequestEvent) { event.Action = "opened" }), wantFlow: policy.PRFlowWork},
		{name: "merged promotion PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = cfg.Repository.BaseBranch
			event.HeadRef = cfg.Repository.StagingBranch
		}), wantFlow: policy.PRFlowPromotion, wantWarnings: 1},
		{name: "merged unmanaged direct base PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = cfg.Repository.BaseBranch
			event.HeadRef = "feature/direct"
		}), wantFlow: policy.PRFlowUnmanaged},
		{name: "merged misrouted work PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = cfg.Repository.BaseBranch
		}), wantFlow: policy.PRFlowMisroutedWork},
		{name: "merged misrouted target PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = "release"
		}), wantFlow: policy.PRFlowMisroutedWork},
		{name: "merged work PR without references", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.DeliveryRecordDigest = ""
			event.IssueReferences = nil
		}), wantFlow: policy.PRFlowWork, wantWarnings: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := PlanPullRequestTransition(test.event, cfg)
			if got.Flow != test.wantFlow {
				t.Fatalf("flow = %q, want %q", got.Flow, test.wantFlow)
			}
			if len(got.Operations) != test.wantOperations {
				t.Fatalf("operations = %+v, want count %d", got.Operations, test.wantOperations)
			}
			if len(got.Warnings) != test.wantWarnings {
				t.Fatalf("warnings = %v, want count %d", got.Warnings, test.wantWarnings)
			}
		})
	}
}

func TestPlanPullRequestTransitionUsesConfiguredPolicyAndStableOrdering(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PRPolicy.RequiredReferenceKeyword = "Tracks"
	cfg.IssuePolicy.AwaitingReviewLabel = "workflow:review"
	event := PullRequestEvent{
		Repository:             "example/repo",
		Action:                 "closed",
		Number:                 42,
		Title:                  "Tracks #20 and #4",
		Body:                   "Refs #1\nTracks #12 and #4",
		BaseRef:                cfg.Repository.StagingBranch,
		HeadRef:                cfg.Repository.WorkBranchPrefix + "queue",
		BaseRepositoryFullName: "example/repo",
		HeadRepositoryFullName: "example/repo",
		BaseSHA:                "base-1",
		HeadSHA:                "head-1",
		State:                  "closed",
		Merged:                 true,
		DeliveryRecordDigest:   "sha256:record",
		IssueReferences: []DeliveryIssueReference{
			{Number: 20, NodeID: "I_20", OwnershipDigest: "sha256:20"},
			{Number: 4, NodeID: "I_4", OwnershipDigest: "sha256:4"},
			{Number: 12, NodeID: "I_12", OwnershipDigest: "sha256:12"},
		},
	}

	got := PlanPullRequestTransition(event, cfg)
	want := []TransitionOperation{
		{ID: "issue-4-awaiting-review", IssueNumber: 4, Action: TransitionActionAddLabels, Label: "workflow:review", IssueNodeID: "I_4", OwnershipDigest: "sha256:4"},
		{ID: "issue-12-awaiting-review", IssueNumber: 12, Action: TransitionActionAddLabels, Label: "workflow:review", IssueNodeID: "I_12", OwnershipDigest: "sha256:12"},
		{ID: "issue-20-awaiting-review", IssueNumber: 20, Action: TransitionActionAddLabels, Label: "workflow:review", IssueNodeID: "I_20", OwnershipDigest: "sha256:20"},
	}
	if !reflect.DeepEqual(got.Operations, want) {
		t.Fatalf("operations = %+v, want %+v", got.Operations, want)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"schemaVersion":4,"kind":"workItemTransitionPlan","repository":"example/repo","eventAction":"closed","pullRequestNumber":42,"flow":"work","operations":[{"id":"issue-4-awaiting-review","issueNumber":4,"action":"add_labels","label":"workflow:review","issueNodeId":"I_4","ownershipDigest":"sha256:4"},{"id":"issue-12-awaiting-review","issueNumber":12,"action":"add_labels","label":"workflow:review","issueNodeId":"I_12","ownershipDigest":"sha256:12"},{"id":"issue-20-awaiting-review","issueNumber":20,"action":"add_labels","label":"workflow:review","issueNodeId":"I_20","ownershipDigest":"sha256:20"}],"warnings":[],"deliveryRecordDigest":"sha256:record"}`
	if string(encoded) != wantJSON {
		t.Fatalf("JSON = %s\nwant = %s", encoded, wantJSON)
	}
}

func TestPlanPromotionTransitionClosesIssuesThenCommitsCursor(t *testing.T) {
	cfg := config.DefaultConfig()
	event := PullRequestEvent{
		Repository: "example/repo", Action: "closed", Number: 50, Merged: true,
		BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		Promotion: &PromotionTransition{
			PlanDigest: "sha256:plan", CursorDigest: "sha256:cursor", BaseIntegrationDigest: "sha256:integration",
			IssueReferences: []DeliveryIssueReference{
				{Number: 9, NodeID: "I_9", OwnershipDigest: "sha256:9"},
				{Number: 3, NodeID: "I_3", OwnershipDigest: "sha256:3"},
			},
		},
	}
	got := PlanPullRequestTransition(event, cfg)
	wantActions := []string{
		TransitionActionCloseIssue, TransitionActionRemoveLabel,
		TransitionActionCloseIssue, TransitionActionRemoveLabel,
		TransitionActionAppendBaseIntegration, TransitionActionCommitPromotionCursor,
	}
	if got.SchemaVersion != 4 || got.Flow != policy.PRFlowPromotion || len(got.Operations) != len(wantActions) {
		t.Fatalf("promotion plan = %+v", got)
	}
	for index, action := range wantActions {
		if got.Operations[index].Action != action {
			t.Fatalf("operation %d = %+v, want action %q", index, got.Operations[index], action)
		}
	}
	if got.Operations[0].IssueNumber != 3 || got.Operations[2].IssueNumber != 9 || got.Operations[len(got.Operations)-1].CursorDigest != "sha256:cursor" {
		t.Fatalf("promotion operation ordering = %+v", got.Operations)
	}
}

func TestPlanPromotionTransitionCommittedDuplicateHasNoIssueOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	event := PullRequestEvent{
		Repository: "example/repo", Action: "closed", Number: 50, Merged: true,
		BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		Promotion: &PromotionTransition{
			Committed: true, PlanDigest: "sha256:plan", CursorDigest: "sha256:cursor", BaseIntegrationDigest: "sha256:integration",
		},
	}
	got := PlanPullRequestTransition(event, cfg)
	if len(got.Operations) != 0 || len(got.Warnings) != 0 || got.PromotionPlanDigest != "sha256:plan" {
		t.Fatalf("duplicate promotion plan = %+v", got)
	}
}

func TestPlanPromotionTransitionRejectsIncompleteCommittedFacts(t *testing.T) {
	cfg := config.DefaultConfig()
	plan := PlanPullRequestTransition(PullRequestEvent{
		Repository: "example/repo", Action: "closed", Number: 50, Merged: true,
		BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		Promotion: &PromotionTransition{Committed: true},
	}, cfg)
	if len(plan.Operations) != 0 || len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "incomplete") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPromotionTransitionPublicJSONFixture(t *testing.T) {
	cfg := config.DefaultConfig()
	got := PlanPullRequestTransition(PullRequestEvent{
		Repository: "example/repo", Action: "closed", Number: 50, Merged: true,
		BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		Promotion: &PromotionTransition{
			PlanDigest: "sha256:plan", CursorDigest: "sha256:cursor", BaseIntegrationDigest: "sha256:integration",
			IssueReferences: []DeliveryIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: "sha256:ownership"}},
		},
	}, cfg)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "work-item-transition-v4-promotion.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != strings.TrimSpace(string(want)) {
		t.Fatalf("JSON = %s\nwant = %s", encoded, want)
	}
}

func mutateEvent(event PullRequestEvent, mutate func(*PullRequestEvent)) PullRequestEvent {
	mutate(&event)
	return event
}
