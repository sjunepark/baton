package snapshot

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/queue"
)

func TestBuildRepositorySnapshotRecommendationPolicy(t *testing.T) {
	for _, test := range []struct {
		name           string
		pullRequests   []PullRequestFacts
		issues         []queue.Issue
		wantOutcome    Outcome
		wantAction     Action
		wantReason     string
		wantCandidates int
	}{
		{name: "failed required check", pullRequests: []PullRequestFacts{pullRequestFact(1, "failure", "approved", RequiredCheckState{Context: "test", Found: true, State: "failed"})}, wantOutcome: OutcomeActionable, wantAction: ActionPullRequestFollowUp, wantReason: "failed_required_check", wantCandidates: 1},
		{name: "missing required check", pullRequests: []PullRequestFacts{pullRequestFact(1, "success", "approved", RequiredCheckState{Context: "test", State: "missing"})}, wantOutcome: OutcomeBlocked, wantReason: "missing_required_check", wantCandidates: 1},
		{name: "pending checks", pullRequests: []PullRequestFacts{pullRequestFact(1, "pending", "approved", RequiredCheckState{Context: "test", Found: true, State: "pending"})}, wantOutcome: OutcomeWaiting, wantReason: "pending_required_check", wantCandidates: 1},
		{name: "awaiting review", pullRequests: []PullRequestFacts{pullRequestFact(1, "success", "review_required")}, wantOutcome: OutcomeWaiting, wantReason: "awaiting_review", wantCandidates: 1},
		{name: "green no feedback", pullRequests: []PullRequestFacts{pullRequestFact(1, "success", "approved")}, wantOutcome: OutcomeHumanChoiceRequired, wantReason: "ready_for_human_disposition", wantCandidates: 1},
		{name: "eligible issue bypasses waiting pull request", pullRequests: []PullRequestFacts{pullRequestFact(1, "success", "approved")}, issues: []queue.Issue{{Number: 7, Labels: []string{"agent:ready-bounded"}}}, wantOutcome: OutcomeActionable, wantAction: ActionIssueImplementation, wantReason: "eligible-issue", wantCandidates: 1},
		{name: "multiple actionable pull requests", pullRequests: []PullRequestFacts{pullRequestFact(2, "failure", "approved"), pullRequestFact(1, "failure", "approved")}, wantOutcome: OutcomeHumanChoiceRequired, wantAction: ActionPullRequestFollowUp, wantReason: "failing_checks", wantCandidates: 2},
		{name: "single issue", issues: []queue.Issue{{Number: 7, Labels: []string{"agent:ready-bounded"}}}, wantOutcome: OutcomeActionable, wantAction: ActionIssueImplementation, wantReason: "eligible-issue", wantCandidates: 1},
		{name: "tied issues", issues: []queue.Issue{{Number: 7, Labels: []string{"agent:ready-bounded"}}, {Number: 8, Labels: []string{"agent:ready-bounded"}}}, wantOutcome: OutcomeHumanChoiceRequired, wantAction: ActionIssueImplementation, wantReason: "eligible-issue", wantCandidates: 2},
		{name: "no work", wantOutcome: OutcomeIdle, wantReason: "no eligible issue or PR follow-up", wantCandidates: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := buildSnapshotForTest(t, Complete, nil, test.pullRequests, test.issues)
			if result.Recommendation.Outcome != test.wantOutcome || len(result.Recommendation.Candidates) != test.wantCandidates {
				t.Fatalf("recommendation = %+v", result.Recommendation)
			}
			if test.wantAction == "" {
				if result.Recommendation.Action != nil {
					t.Fatalf("action = %v, want nil", *result.Recommendation.Action)
				}
			} else if result.Recommendation.Action == nil || *result.Recommendation.Action != test.wantAction {
				t.Fatalf("action = %v, want %s", result.Recommendation.Action, test.wantAction)
			}
			if test.wantReason != "" && !strings.Contains(strings.Join(candidateAndRecommendationReasons(result.Recommendation), ","), test.wantReason) {
				t.Fatalf("reasons = %+v, want %q", candidateAndRecommendationReasons(result.Recommendation), test.wantReason)
			}
		})
	}
}

func TestBuildRepositorySnapshotDegradesWithoutAction(t *testing.T) {
	result := buildSnapshotForTest(t, Degraded, []Warning{{Code: "rate_limited", Scope: "issues", Message: "rate limited", Retryable: true}}, nil, nil)
	if result.Recommendation.Outcome != OutcomeDegraded || result.Recommendation.Action != nil || len(result.Warnings) != 1 {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestBuildRepositorySnapshotPullRequestSignals(t *testing.T) {
	for _, test := range []struct {
		name        string
		mutate      func(*PullRequestFacts)
		wantOutcome Outcome
		wantReason  string
	}{
		{name: "changes requested", mutate: func(facts *PullRequestFacts) { facts.Review.State = "changes_requested" }, wantOutcome: OutcomeActionable, wantReason: "review_feedback"},
		{name: "unresolved human thread", mutate: func(facts *PullRequestFacts) { facts.ReviewThreads.Summary.HumanUnresolved = 1 }, wantOutcome: OutcomeActionable, wantReason: "review_feedback"},
		{name: "merge conflict", mutate: func(facts *PullRequestFacts) {
			facts.PullRequest.Mergeable, facts.PullRequest.MergeState = "conflicting", "dirty"
		}, wantOutcome: OutcomeActionable, wantReason: "merge_update_required"},
		{name: "strict behind", mutate: func(facts *PullRequestFacts) {
			facts.PullRequest.MergeState = "behind"
			facts.Rules.StrictRequiredChecks = true
		}, wantOutcome: OutcomeActionable, wantReason: "merge_update_required"},
		{name: "non-strict behind", mutate: func(facts *PullRequestFacts) { facts.PullRequest.MergeState = "behind" }, wantOutcome: OutcomeHumanChoiceRequired, wantReason: "ready_for_human_disposition"},
		{name: "draft", mutate: func(facts *PullRequestFacts) { facts.PullRequest.Draft = true }, wantOutcome: OutcomeWaiting, wantReason: "draft"},
		{name: "merge blocked", mutate: func(facts *PullRequestFacts) { facts.PullRequest.MergeState = "blocked" }, wantOutcome: OutcomeBlocked, wantReason: "merge_blocked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := pullRequestFact(7, "success", "approved")
			test.mutate(&facts)
			result := buildSnapshotForTest(t, Complete, nil, []PullRequestFacts{facts}, nil)
			if result.Recommendation.Outcome != test.wantOutcome || !strings.Contains(strings.Join(candidateAndRecommendationReasons(result.Recommendation), ","), test.wantReason) {
				t.Fatalf("recommendation = %+v", result.Recommendation)
			}
			if test.wantOutcome != OutcomeActionable && result.Recommendation.Action != nil {
				t.Fatalf("non-actionable outcome has action: %+v", result.Recommendation)
			}
		})
	}
}

func TestBuildRepositorySnapshotDerivesCompletenessFromChildFacts(t *testing.T) {
	warning := Warning{Code: "forbidden", Scope: "pullRequest:7:reviews", Message: "unavailable", HTTPStatus: 403}
	facts := pullRequestFact(7, "success", "approved")
	facts.Completeness = Degraded
	facts.Warnings = []Warning{warning}
	result := buildSnapshotForTest(t, Complete, []Warning{warning}, []PullRequestFacts{facts}, nil)
	if result.Completeness != Degraded || result.Recommendation.Outcome != OutcomeDegraded || len(result.Warnings) != 1 {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestPullRequestFollowUpInstructionsPreserveCallerSafetyBoundary(t *testing.T) {
	result := buildSnapshotForTest(t, Complete, nil, []PullRequestFacts{pullRequestFact(7, "failure", "approved")}, nil)
	instructions := strings.Join(result.Recommendation.Instructions, " ")
	if !strings.Contains(instructions, "caller-provided isolated checkout") || !strings.Contains(instructions, "Do not merge") {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestBuildRepositorySnapshotUnknownRequiredCheckDegrades(t *testing.T) {
	result := buildSnapshotForTest(t, Complete, nil, []PullRequestFacts{pullRequestFact(7, "success", "approved", RequiredCheckState{Context: "test", Found: true, State: "unknown"})}, nil)
	if result.Completeness != Degraded || result.Recommendation.Outcome != OutcomeDegraded || result.Recommendation.Action != nil || len(result.Warnings) != 1 {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestBuildRepositorySnapshotBranchHealthOutcome(t *testing.T) {
	for _, test := range []struct {
		state       string
		wantOutcome Outcome
		wantAction  bool
	}{{state: "failure", wantOutcome: OutcomeActionable, wantAction: true}, {state: "pending", wantOutcome: OutcomeWaiting}} {
		t.Run(test.state, func(t *testing.T) {
			cfg := config.DefaultConfig()
			queueProjection := queue.BuildSnapshotWithBranchHealth("example/repo", cfg, nil, nil, &queue.BranchHealth{Ref: "agent", SHA: "branch", CheckState: test.state})
			started := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
			result, err := Build(BuildInput{Facts: Acquisition{Repository: "example/repo", Completeness: Complete, Branches: []BranchFacts{{Branch: gh.Branch{Ref: "agent", SHA: "branch"}, Checks: gh.CheckRollup{State: test.state, Complete: true}, Completeness: Complete}}}, Queue: queueProjection, StartedAt: started, CompletedAt: started.Add(time.Second)})
			if err != nil {
				t.Fatal(err)
			}
			if result.Recommendation.Outcome != test.wantOutcome || (result.Recommendation.Action != nil) != test.wantAction {
				t.Fatalf("recommendation = %+v", result.Recommendation)
			}
		})
	}
}

func TestBuildRepositorySnapshotRequestedInvestigation(t *testing.T) {
	cfg := config.DefaultConfig()
	issue := queue.Issue{Number: 9, Labels: []string{"agent:investigate-only"}}
	queueProjection := queue.BuildSnapshot("example/repo", cfg, []queue.Issue{issue}, nil)
	started := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	result, err := Build(BuildInput{Facts: Acquisition{Repository: "example/repo", Completeness: Complete}, Queue: queueProjection, StartedAt: started, CompletedAt: started.Add(time.Second), RequestedAction: "issue-investigation"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recommendation.Outcome != OutcomeActionable || result.Recommendation.Action == nil || *result.Recommendation.Action != ActionIssueInvestigation || result.NextV2().SelectedAction != "issue-investigation" {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestBuildRepositorySnapshotCarriesRevisionBoundCandidateAndNonNullCollections(t *testing.T) {
	result := buildSnapshotForTest(t, Complete, nil, []PullRequestFacts{pullRequestFact(7, "failure", "approved")}, nil)
	candidate := result.Recommendation.Candidates[0]
	if candidate.Identity.Repository != "example/repo" || candidate.Identity.Number != 7 || candidate.Identity.HeadSHA != "head-7" || candidate.Identity.BaseSHA != "base-7" {
		t.Fatalf("candidate = %+v", candidate)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, expected := range []string{`"kind":"repositorySnapshot"`, `"warnings":[]`, `"deferredCandidates":[]`, `"startedAt":"2026-07-13T00:00:00Z"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("payload missing %s: %s", expected, text)
		}
	}
}

func TestBuildRepositorySnapshotRejectsDegradedStateWithoutEvidence(t *testing.T) {
	_, err := Build(BuildInput{Facts: Acquisition{Repository: "example/repo", Completeness: Degraded}, Queue: queue.Snapshot{}, StartedAt: time.Now(), CompletedAt: time.Now()})
	if err == nil {
		t.Fatal("expected degraded snapshot without warnings to fail")
	}
}

func buildSnapshotForTest(t *testing.T, completeness Completeness, warnings []Warning, pullRequests []PullRequestFacts, issues []queue.Issue) RepositorySnapshot {
	t.Helper()
	cfg := config.DefaultConfig()
	queuePullRequests := make([]queue.PullRequest, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		pr := pullRequest.PullRequest
		queuePullRequests = append(queuePullRequests, queue.PullRequest{Number: pr.Number, Title: pr.Title, URL: pr.URL, Body: pr.Body, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA, CheckState: pullRequest.Checks.State})
	}
	queueSnapshot := queue.BuildSnapshot("example/repo", cfg, issues, queuePullRequests)
	started := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	result, err := Build(BuildInput{
		Facts: Acquisition{Repository: "example/repo", Completeness: completeness, Warnings: warnings, PullRequests: pullRequests},
		Queue: queueSnapshot, StartedAt: started, CompletedAt: started.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func pullRequestFact(number int, checkState, reviewState string, required ...RequiredCheckState) PullRequestFacts {
	return PullRequestFacts{
		PullRequest: gh.PullRequest{Number: number, Title: "PR", URL: "https://example/pr", BaseRef: "agent", BaseSHA: "base-" + strconv.Itoa(number), HeadRef: "work", HeadSHA: "head-" + strconv.Itoa(number), Mergeable: "mergeable", MergeState: "clean"},
		Checks:      gh.CheckRollup{State: checkState, Complete: true}, RequiredChecks: required,
		ReviewThreads: gh.ReviewThreadResult{Complete: true}, Review: ReviewState{State: reviewState}, Completeness: Complete,
	}
}

func candidateAndRecommendationReasons(recommendation Recommendation) []string {
	result := append([]string{}, recommendation.Reasons...)
	for _, candidate := range recommendation.Candidates {
		result = append(result, candidate.Reasons...)
	}
	return result
}
