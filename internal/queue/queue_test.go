package queue

import (
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestBuildSnapshotEligibility(t *testing.T) {
	cfg := config.DefaultCreoCompat()
	snapshot := BuildSnapshot("open-creo/creo", cfg,
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
}

func TestRecommendNextPrefersPRFollowup(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "open-creo/creo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true}},
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", BaseRef: "agent", HeadRef: "agent-work/1", CheckState: "failure"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.Action != "pr-followup" || next.PR == nil || next.PR.Number != 10 {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextPrefersBranchHealthBeforeIssue(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "open-creo/creo",
		BranchHealth:  &BranchHealth{Ref: "agent", SHA: "abc", CheckState: "failure"},
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-implementation"}},
	}
	next := RecommendNext(snapshot)
	if next.Action != "branch-health" {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextPrefersBranchHealthBeforePRFollowup(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "open-creo/creo",
		BranchHealth:  &BranchHealth{Ref: "agent", SHA: "abc", CheckState: "pending"},
		PullRequests: []PullState{{
			PullRequest: PullRequest{Number: 10, URL: "https://example/pr/10", BaseRef: "agent", HeadRef: "agent-work/1", CheckState: "failure"},
		}},
	}
	next := RecommendNext(snapshot)
	if next.Action != "branch-health" {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextIssueImplementation(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "open-creo/creo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true}},
	}
	next := RecommendNext(snapshot)
	if next.Action != "issue-implementation" || next.Issue == nil || next.Issue.Number != 1 {
		t.Fatalf("next = %#v", next)
	}
}

func TestRecommendNextIssueInvestigation(t *testing.T) {
	snapshot := Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "open-creo/creo",
		Issues:        []IssueState{{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-investigation"}},
	}
	next := RecommendNext(snapshot)
	if next.Action != "issue-investigation" || next.Issue == nil || next.Issue.Number != 1 {
		t.Fatalf("next = %#v", next)
	}
}
