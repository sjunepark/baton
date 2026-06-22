package queue

import (
	"testing"

	"github.com/sejunpark/baton/internal/config"
)

func TestBuildSnapshotEligibility(t *testing.T) {
	cfg := config.DefaultCreoCompat()
	snapshot := BuildSnapshot("open-creo/creo", cfg,
		[]Issue{
			{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial"}},
			{Number: 2, URL: "https://example/2", Labels: []string{"agent:ready-bounded", "needs:discussion"}},
			{Number: 3, URL: "https://example/3", Labels: []string{"bug"}},
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
