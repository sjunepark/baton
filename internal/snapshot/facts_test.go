package snapshot

import (
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/gh"
)

func TestRequiredCheckStatesMatchContextAndIntegration(t *testing.T) {
	checks := []gh.CheckState{
		{Name: "test", AppID: 7, Status: "completed", Conclusion: "success"},
		{Name: "test", AppID: 42, Status: "completed", Conclusion: "failure"},
		{Name: "lint", Status: "in_progress"},
	}
	required := []gh.RequiredCheck{{Context: "test", IntegrationID: 42}, {Context: "lint"}, {Context: "missing"}}

	states := RequiredCheckStates(checks, required)
	if len(states) != 3 || !states[0].Found || states[0].State != "failed" || !states[1].Found || states[1].State != "pending" || states[2].Found || states[2].State != "missing" {
		t.Fatalf("states = %+v", states)
	}
}

func TestRequiredCheckStatesRequireEveryMatchingSourceAndAcceptGitHubSuccessConclusions(t *testing.T) {
	for _, test := range []struct {
		name   string
		checks []gh.CheckState
		want   string
	}{
		{name: "duplicate failure", checks: []gh.CheckState{{Name: "test", Conclusion: "success"}, {Name: "test", Status: "failure"}}, want: "failed"},
		{name: "neutral and skipped", checks: []gh.CheckState{{Name: "test", Conclusion: "neutral"}, {Name: "test", Conclusion: "skipped"}}, want: "passed"},
		{name: "pending wins", checks: []gh.CheckState{{Name: "test", Conclusion: "success"}, {Name: "test", Status: "pending"}}, want: "pending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			states := RequiredCheckStates(test.checks, []gh.RequiredCheck{{Context: "test"}})
			if len(states) != 1 || states[0].State != test.want {
				t.Fatalf("states = %+v", states)
			}
		})
	}
}

func TestMergeRequiredChecksPrefersEffectiveRuleIdentity(t *testing.T) {
	merged := MergeRequiredChecks(
		[]gh.RequiredCheck{{Context: "test", IntegrationID: 42}},
		[]gh.RequiredCheck{{Context: "test"}, {Context: "legacy"}},
	)
	if len(merged) != 2 || merged[0].IntegrationID != 42 || merged[1].Context != "legacy" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestMergeRequiredChecksKeepsIndependentAppIdentities(t *testing.T) {
	merged := MergeRequiredChecks(
		[]gh.RequiredCheck{{Context: "test"}, {Context: "test", IntegrationID: 42}},
		[]gh.RequiredCheck{{Context: "test"}, {Context: "test", IntegrationID: 7}},
	)
	if len(merged) != 2 || merged[0].IntegrationID != 42 || merged[1].IntegrationID != 7 {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestEvaluateReviewsUsesLatestHumanDecisiveReviewOnCurrentRevision(t *testing.T) {
	earlier := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	reviews := []gh.PullRequestReview{
		{ID: 3, State: "APPROVED", CommitSHA: "head", SubmittedAt: later, Author: gh.Actor{Login: "alice", Type: "User"}},
		{ID: 1, State: "CHANGES_REQUESTED", CommitSHA: "head", SubmittedAt: earlier, Author: gh.Actor{Login: "alice", Type: "User"}},
		{ID: 4, State: "COMMENTED", CommitSHA: "head", SubmittedAt: later.Add(time.Minute), Author: gh.Actor{Login: "alice", Type: "User"}},
		{ID: 5, State: "APPROVED", CommitSHA: "head", SubmittedAt: later, Author: gh.Actor{Login: "ci", Type: "Bot"}},
		{ID: 6, State: "APPROVED", CommitSHA: "old", SubmittedAt: later, Author: gh.Actor{Login: "bob", Type: "User"}},
	}
	rules := gh.BranchRules{RequiredApprovingReviewCount: 2, DismissStaleReviews: true}

	state := EvaluateReviews("head", rules, reviews, []gh.ReviewRequest{{Kind: "team", Team: "maintainers"}})
	if state.Approvals != 1 || state.ChangesRequested != 0 || state.RequestedTeams != 1 || state.State != "review_required" {
		t.Fatalf("state = %+v", state)
	}
}

func TestEvaluateReviewsReportsCurrentChangesRequest(t *testing.T) {
	state := EvaluateReviews("head", gh.BranchRules{}, []gh.PullRequestReview{{ID: 1, State: "CHANGES_REQUESTED", CommitSHA: "head", Author: gh.Actor{Login: "alice", Type: "User"}}}, nil)
	if state.State != "changes_requested" || state.ChangesRequested != 1 {
		t.Fatalf("state = %+v", state)
	}
}
