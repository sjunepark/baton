package queue

import (
	"encoding/json"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestBuildSnapshotEligibility(t *testing.T) {
	cfg := config.DefaultConfig()
	snapshot := BuildSnapshot("example-org/example-repo", cfg,
		[]Issue{
			{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial"}},
			{Number: 2, URL: "https://example/2", Labels: []string{"agent:ready-bounded", "needs:discussion"}},
			{Number: 3, URL: "https://example/3", Labels: []string{"bug"}},
			{Number: 4, URL: "https://example/4", Labels: []string{"agent:investigate-only"}},
		},
		[]PullRequest{{Number: 10, Body: "Refs #1", BaseRef: "agent", HeadRef: "agent-work/1"}},
	)
	if snapshot.Issues[0].Eligible {
		t.Fatalf("issue with linked PR should not be eligible: %#v", snapshot.Issues[0])
	}
	if snapshot.Issues[1].Eligible {
		t.Fatalf("skip label issue should not be eligible: %#v", snapshot.Issues[1])
	}
	if snapshot.Issues[2].Eligible {
		t.Fatalf("missing implementation label should not be eligible: %#v", snapshot.Issues[2])
	}
	if !snapshot.Issues[3].Eligible || snapshot.Issues[3].Action != "issue-investigation" {
		t.Fatalf("investigation issue should be eligible: %#v", snapshot.Issues[3])
	}
	if snapshot.Issues[3].LinkedPRs == nil || len(snapshot.Issues[3].LinkedPRs) != 0 {
		t.Fatalf("empty linked PRs should be an empty array, got %#v", snapshot.Issues[3].LinkedPRs)
	}
	if snapshot.Counts.TotalIssues != 4 || snapshot.Counts.EligibleIssues != 1 || snapshot.Counts.SkippedIssues != 3 || snapshot.Counts.OpenPullRequests != 1 {
		t.Fatalf("counts = %#v", snapshot.Counts)
	}
	if snapshot.Counts.EligibleByAction["issue-investigation"] != 1 {
		t.Fatalf("eligible by action = %#v", snapshot.Counts.EligibleByAction)
	}
	if len(snapshot.Help) == 0 {
		t.Fatal("snapshot help should include next commands")
	}
}

func TestBuildSnapshotInvestigationHelpDoesNotSuggestWorkBranch(t *testing.T) {
	cfg := config.DefaultConfig()
	snapshot := BuildSnapshot("example-org/example-repo", cfg,
		[]Issue{{Number: 4, URL: "https://example/4", Labels: []string{"agent:investigate-only"}}},
		nil,
	)
	if len(snapshot.Help) < 2 || snapshot.Help[1] != "Run `baton next --action issue-investigation --format toon` to select investigation work." {
		t.Fatalf("help = %#v", snapshot.Help)
	}
}

func TestRecommendNextReturnsFailingPRCandidatesBeforeIssues(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true}},
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 12, URL: "https://example/pr/12", BaseRef: "agent", HeadRef: "agent-work/12", CheckState: "failure"},
		}, {
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", BaseRef: "agent", HeadRef: "agent-work/10", CheckState: "failure"},
		}, {
			PullRequest: PullRequest{Number: 11, URL: "https://example/pr/11", BaseRef: "agent", HeadRef: "agent-work/11", CheckState: "pending"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "pr-followup" || next.Reason != "failing-checks" || len(next.Candidates) != 2 || !next.SelectionRequired {
		t.Fatalf("next = %#v", next)
	}
	if next.Candidates[0].Number != 10 || next.Candidates[1].Number != 12 {
		t.Fatalf("candidates not sorted by number: %#v", next.Candidates)
	}
}

func TestRecommendNextReturnsSuccessfulPRCandidatesBeforeIssues(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-implementation"}},
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", BaseRef: "main", HeadRef: "agent", CheckState: "success"},
		}, {
			PullRequest: PullRequest{Number: 11, URL: "https://example/pr/11", BaseRef: "main", HeadRef: "agent", CheckState: "unknown"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "pr-followup" || next.Reason != "ready-for-review" || len(next.Candidates) != 1 || next.Candidates[0].Number != 10 {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextReturnsPendingPRCandidatesWhenNoFailures(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 12, URL: "https://example/pr/12", CheckState: "success"},
		}, {
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", CheckState: "pending"},
		}, {
			PullRequest: PullRequest{Number: 11, URL: "https://example/pr/11", CheckState: "pending"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "pr-followup" || next.Reason != "pending-checks" || len(next.Candidates) != 2 {
		t.Fatalf("next = %#v", next)
	}
	if next.Candidates[0].Number != 10 || next.Candidates[1].Number != 11 {
		t.Fatalf("candidates not sorted by number: %#v", next.Candidates)
	}
}

func TestRecommendNextPrefersBranchHealthBeforeIssue(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		BranchHealth:  &BranchHealth{Ref: "agent", SHA: "abc", CheckState: "failure"},
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-implementation"}},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "branch-health" || len(next.Candidates) != 1 || next.Candidates[0].Type != "branch" || next.Candidates[0].Ref != "agent" {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextPrefersBranchHealthBeforePRFollowup(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		BranchHealth:  &BranchHealth{Ref: "agent", SHA: "abc", CheckState: "pending"},
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", BaseRef: "agent", HeadRef: "agent-work/1", CheckState: "failure"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "branch-health" {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextIssueImplementationCandidates(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues: []IssueState{
			{Issue: Issue{Number: 3, URL: "https://example/3", Title: "Third"}, Eligible: true, Action: "issue-implementation"},
			{Issue: Issue{Number: 1, URL: "https://example/1", Title: "First"}, Eligible: true, Action: "issue-implementation"},
		},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "issue-implementation" || next.Reason != "eligible-issue" || len(next.Candidates) != 2 || !next.SelectionRequired {
		t.Fatalf("next = %#v", next)
	}
	if next.Candidates[0].Number != 1 || next.Candidates[1].Number != 3 {
		t.Fatalf("candidates not sorted by number: %#v", next.Candidates)
	}
}

func TestRecommendNextImplementationIssuesOutrankInvestigationIssues(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues: []IssueState{
			{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-investigation"},
			{Issue: Issue{Number: 2, URL: "https://example/2"}, Eligible: true, Action: "issue-implementation"},
		},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "issue-implementation" || len(next.Candidates) != 1 || next.Candidates[0].Number != 2 {
		t.Fatalf("next = %#v", next)
	}
	if next.SelectionReason != "implementation-work-precedes-investigation" || len(next.DeferredEligibleItems) != 1 || next.DeferredEligibleItems[0].Number != 1 {
		t.Fatalf("deferred selection metadata = %#v", next)
	}
}

func TestRecommendNextForRequestedInvestigationAction(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues: []IssueState{
			{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-investigation"},
			{Issue: Issue{Number: 2, URL: "https://example/2"}, Eligible: true, Action: "issue-implementation"},
		},
	}
	next := RecommendNextInvestigation(snapshot)
	if next.SelectedAction != "issue-investigation" || next.Reason != "requested-action" || len(next.Candidates) != 1 || next.Candidates[0].Number != 1 {
		t.Fatalf("next = %#v", next)
	}
	if len(next.DeferredEligibleItems) != 0 {
		t.Fatalf("requested action should not defer unrelated work: %#v", next.DeferredEligibleItems)
	}
}

func TestRecommendNextIssueInvestigationCandidates(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues: []IssueState{
			{Issue: Issue{Number: 2, URL: "https://example/2"}, Eligible: true, Action: "issue-investigation"},
			{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-investigation"},
		},
	}
	next := RecommendNext(snapshot)
	if next.SelectedAction != "issue-investigation" || next.Reason != "eligible-investigation" || len(next.Candidates) != 2 {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextNoWorkHasEmptyCandidates(t *testing.T) {
	next := RecommendNext(Snapshot{SchemaVersion: 1, Kind: "queueSnapshot", Repo: "example-org/example-repo"})
	if next.SelectedAction != "none" || next.SelectionRequired || len(next.Candidates) != 0 {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextJSONOmitsLegacySingularFields(t *testing.T) {
	next := RecommendNext(Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example-org/example-repo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-implementation"}},
	})
	content, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["kind"] != "nextCandidates" || fields["schemaVersion"].(float64) != 2 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields["selectedAction"] != "issue-implementation" {
		t.Fatalf("selectedAction missing: %s", content)
	}
	if _, ok := fields["action"]; ok {
		t.Fatalf("legacy action field present: %s", content)
	}
	if _, ok := fields["pr"]; ok {
		t.Fatalf("legacy pr field present: %s", content)
	}
	if _, ok := fields["issue"]; ok {
		t.Fatalf("legacy issue field present: %s", content)
	}
}
