package workitem

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

func TestPlanPullRequestTransitionLifecycleEdges(t *testing.T) {
	cfg := config.DefaultConfig()
	work := PullRequestEvent{
		Repository: "example/repo",
		Action:     "closed",
		Number:     42,
		Title:      "Implement queue Refs #9, #3",
		Body:       "Refs #9 and #7",
		BaseRef:    cfg.Repository.StagingBranch,
		HeadRef:    cfg.Repository.WorkBranchPrefix + "queue",
		Merged:     true,
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
		}), wantFlow: policy.PRFlowPromotion},
		{name: "merged direct base PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = cfg.Repository.BaseBranch
			event.HeadRef = "feature/direct"
		}), wantFlow: policy.PRFlowDirectBase},
		{name: "merged invalid direct work PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = cfg.Repository.BaseBranch
		}), wantFlow: policy.PRFlowInvalidDirectWork},
		{name: "merged unsupported target PR", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.BaseRef = "release"
		}), wantFlow: policy.PRFlowUnsupportedTarget},
		{name: "merged work PR without references", event: mutateEvent(work, func(event *PullRequestEvent) {
			event.Title = "Implement queue"
			event.Body = "No issue reference"
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
		Repository: "example/repo",
		Action:     "closed",
		Number:     42,
		Title:      "Tracks #20 and #4",
		Body:       "Refs #1\nTracks #12 and #4",
		BaseRef:    cfg.Repository.StagingBranch,
		HeadRef:    cfg.Repository.WorkBranchPrefix + "queue",
		BaseSHA:    "base-1",
		HeadSHA:    "head-1",
		State:      "closed",
		Merged:     true,
	}

	got := PlanPullRequestTransition(event, cfg)
	want := []TransitionOperation{
		{ID: "issue-4-awaiting-review", IssueNumber: 4, Action: TransitionActionAddLabels, Label: "workflow:review"},
		{ID: "issue-12-awaiting-review", IssueNumber: 12, Action: TransitionActionAddLabels, Label: "workflow:review"},
		{ID: "issue-20-awaiting-review", IssueNumber: 20, Action: TransitionActionAddLabels, Label: "workflow:review"},
	}
	if !reflect.DeepEqual(got.Operations, want) {
		t.Fatalf("operations = %+v, want %+v", got.Operations, want)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := `{"schemaVersion":1,"kind":"workItemTransitionPlan","repository":"example/repo","eventAction":"closed","pullRequestNumber":42,"flow":"work","operations":[{"id":"issue-4-awaiting-review","issueNumber":4,"action":"add_labels","label":"workflow:review"},{"id":"issue-12-awaiting-review","issueNumber":12,"action":"add_labels","label":"workflow:review"},{"id":"issue-20-awaiting-review","issueNumber":20,"action":"add_labels","label":"workflow:review"}],"warnings":[]}`
	if string(encoded) != wantJSON {
		t.Fatalf("JSON = %s\nwant = %s", encoded, wantJSON)
	}
}

func mutateEvent(event PullRequestEvent, mutate func(*PullRequestEvent)) PullRequestEvent {
	mutate(&event)
	return event
}
