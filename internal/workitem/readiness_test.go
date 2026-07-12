package workitem

import (
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestClassifyIssueStateTable(t *testing.T) {
	policy := config.DefaultConfig().IssuePolicy
	for _, test := range []struct {
		name  string
		facts IssueFacts
		state State
		ready bool
	}{
		{name: "ready", facts: IssueFacts{Open: true, Labels: []string{"agent:ready-bounded"}}, state: StateReady, ready: true},
		{name: "active work PR", facts: IssueFacts{Open: true, Labels: []string{"agent:ready-bounded"}, LinkedWorkPRs: []int{12}}, state: StateActiveWorkPR},
		{name: "awaiting review", facts: IssueFacts{Open: true, Labels: []string{"agent:ready-bounded", "needs:review"}}, state: StateAwaitingReview},
		{name: "merged history backstop", facts: IssueFacts{Open: true, Labels: []string{"agent:ready-bounded"}, MergedWorkPRs: []int{11}}, state: StateAwaitingReview},
		{name: "blocked", facts: IssueFacts{Open: true, Labels: []string{"agent:ready-bounded", "needs:discussion"}}, state: StateBlocked},
		{name: "closed", facts: IssueFacts{Labels: []string{"agent:ready-bounded"}}, state: StatePromotedOrClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyIssue(test.facts, policy)
			if got.State != test.state || got.Eligible != test.ready {
				t.Fatalf("ClassifyIssue() = %+v", got)
			}
		})
	}
}

func TestClassifyIssueKeepsAgentModeDistinctFromWorkflowState(t *testing.T) {
	policy := config.DefaultConfig().IssuePolicy
	got := ClassifyIssue(IssueFacts{Open: true, Labels: []string{"agent:ready-trivial", policy.AwaitingReviewLabel}}, policy)
	if got.State != StateAwaitingReview || got.Action != "issue-implementation" || got.Eligible {
		t.Fatalf("ClassifyIssue() = %+v", got)
	}
}
