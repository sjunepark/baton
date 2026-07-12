package workitem

import (
	"fmt"
	"sort"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

const TransitionActionAddLabels = "add_labels"

// PullRequestEvent contains the immutable event facts used to plan work-item
// transitions. Revision fields are retained so the applying workflow can bind
// the reviewed plan to current GitHub state without changing planner output.
type PullRequestEvent struct {
	Repository string `json:"repository"`
	Action     string `json:"action"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	BaseRef    string `json:"baseRef"`
	HeadRef    string `json:"headRef"`
	BaseSHA    string `json:"baseSha,omitempty"`
	HeadSHA    string `json:"headSha,omitempty"`
	State      string `json:"state,omitempty"`
	Merged     bool   `json:"merged"`
}

type TransitionOperation struct {
	ID          string `json:"id"`
	IssueNumber int    `json:"issueNumber"`
	Action      string `json:"action"`
	Label       string `json:"label"`
}

type TransitionPlan struct {
	SchemaVersion     int                   `json:"schemaVersion"`
	Kind              string                `json:"kind"`
	Repository        string                `json:"repository"`
	EventAction       string                `json:"eventAction"`
	PullRequestNumber int                   `json:"pullRequestNumber"`
	Flow              policy.PRFlow         `json:"flow"`
	Operations        []TransitionOperation `json:"operations"`
	Warnings          []string              `json:"warnings"`
}

// PlanPullRequestTransition plans the single persisted work-item transition:
// a merged work PR marks each referenced issue as awaiting review. Other
// lifecycle states are derived from GitHub facts and need no mutation.
func PlanPullRequestTransition(event PullRequestEvent, cfg config.Config) TransitionPlan {
	flow := policy.ClassifyPullRequestFlow(policy.PullRequest{
		Number:  event.Number,
		Title:   event.Title,
		Body:    event.Body,
		BaseRef: event.BaseRef,
		HeadRef: event.HeadRef,
	}, cfg)
	plan := TransitionPlan{
		SchemaVersion:     1,
		Kind:              "workItemTransitionPlan",
		Repository:        event.Repository,
		EventAction:       event.Action,
		PullRequestNumber: event.Number,
		Flow:              flow,
		Operations:        []TransitionOperation{},
		Warnings:          []string{},
	}
	if event.Action != "closed" || !event.Merged || flow != policy.PRFlowWork {
		return plan
	}

	issueNumbers := uniqueIssueNumbers(append(
		policy.ExtractReferenceIssueNumbersForPolicy(event.Title, cfg.PRPolicy.RequiredReferenceKeyword),
		policy.ExtractReferenceIssueNumbersForPolicy(event.Body, cfg.PRPolicy.RequiredReferenceKeyword)...,
	))
	if len(issueNumbers) == 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"merged work PR does not reference an issue with %s #123",
			cfg.PRPolicy.RequiredReferenceKeyword,
		))
		return plan
	}
	for _, issueNumber := range issueNumbers {
		plan.Operations = append(plan.Operations, TransitionOperation{
			ID:          fmt.Sprintf("issue-%d-awaiting-review", issueNumber),
			IssueNumber: issueNumber,
			Action:      TransitionActionAddLabels,
			Label:       cfg.IssuePolicy.AwaitingReviewLabel,
		})
	}
	return plan
}

func uniqueIssueNumbers(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}
