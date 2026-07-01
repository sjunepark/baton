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

func TestPriorityPolicyDecisions(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name                      string
		snapshot                  func(t *testing.T) Snapshot
		recommend                 func(Snapshot) NextCandidates
		wantSnapshotPriorityLabel string
		wantSelectedAction        string
		wantCandidates            []int
		wantCandidateLabels       []string
		wantDeferred              []int
		wantDeferredLabels        []string
		wantSelectionReason       string
	}{
		{
			name: "build snapshot uses highest configured priority label",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial", "priority:p3", "priority:p0"}}},
					nil,
				)
			},
			wantSnapshotPriorityLabel: "priority:p0",
		},
		{
			name: "implementation returns only highest priority tied candidates",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{
						{Number: 3, URL: "https://example/3", Title: "P0 third", Labels: []string{"agent:ready-trivial", "priority:p0"}},
						{Number: 1, URL: "https://example/1", Title: "P2 first", Labels: []string{"agent:ready-trivial", "priority:p2"}},
						{Number: 5, URL: "https://example/5", Title: "P0 fifth", Labels: []string{"agent:ready-bounded", "priority:p0"}},
					},
					nil,
				)
			},
			wantSelectedAction:  "issue-implementation",
			wantCandidates:      []int{3, 5},
			wantCandidateLabels: []string{"priority:p0", "priority:p0"},
			wantDeferred:        []int{1},
			wantDeferredLabels:  []string{"priority:p2"},
			wantSelectionReason: "issue-priority-precedes-lower-priority-work",
		},
		{
			name: "unprioritized issues sort after configured priorities",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{
						{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial"}},
						{Number: 2, URL: "https://example/2", Labels: []string{"agent:ready-trivial", "priority:p1"}},
					},
					nil,
				)
			},
			wantCandidates:      []int{2},
			wantCandidateLabels: []string{"priority:p1"},
			wantDeferred:        []int{1},
			wantDeferredLabels:  []string{""},
		},
		{
			name: "implementation tier outranks higher priority investigation",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{
						{Number: 1, URL: "https://example/1", Labels: []string{"agent:investigate-only", "priority:p0"}},
						{Number: 2, URL: "https://example/2", Labels: []string{"agent:ready-trivial", "priority:p2"}},
					},
					nil,
				)
			},
			wantSelectedAction: "issue-implementation",
			wantCandidates:     []int{2},
			wantDeferred:       []int{1},
		},
		{
			name: "branch health outranks issue priority",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshotWithBranchHealth("example-org/example-repo", cfg,
					[]Issue{{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial", "priority:p0"}}},
					nil,
					&BranchHealth{Ref: "agent", SHA: "abc", CheckState: "failure"},
				)
			},
			wantSelectedAction: "branch-health",
		},
		{
			name: "pr followup outranks issue priority",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial", "priority:p0"}}},
					[]PullRequest{{Number: 10, URL: "https://example/pr/10", BaseRef: "agent", HeadRef: "agent-work/10", CheckState: "success"}},
				)
			},
			wantSelectedAction: "pr-followup",
			wantCandidates:     []int{10},
		},
		{
			name: "requested investigation returns only highest priority candidates",
			snapshot: func(t *testing.T) Snapshot {
				return BuildSnapshot("example-org/example-repo", cfg,
					[]Issue{
						{Number: 1, URL: "https://example/1", Labels: []string{"agent:investigate-only", "priority:p3"}},
						{Number: 2, URL: "https://example/2", Labels: []string{"agent:investigate-only", "priority:p1"}},
					},
					nil,
				)
			},
			recommend:          RecommendNextInvestigation,
			wantSelectedAction: "issue-investigation",
			wantCandidates:     []int{2},
			wantDeferred:       []int{1},
		},
		{
			name: "json round trip preserves configured priority rank",
			snapshot: func(t *testing.T) Snapshot {
				custom := config.DefaultConfig()
				custom.IssuePolicy.PriorityLabels = map[string]string{
					"P0": "severity:blocker",
					"P2": "severity:normal",
				}
				custom.IssuePolicy.ControlledLabelGroups["priority"] = []string{"severity:blocker", "severity:normal"}
				snapshot := BuildSnapshot("example-org/example-repo", custom,
					[]Issue{
						{Number: 1, URL: "https://example/1", Labels: []string{"agent:ready-trivial", "severity:normal"}},
						{Number: 2, URL: "https://example/2", Labels: []string{"agent:ready-trivial", "severity:blocker"}},
					},
					nil,
				)
				content, err := json.Marshal(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				var roundTripped Snapshot
				if err := json.Unmarshal(content, &roundTripped); err != nil {
					t.Fatal(err)
				}
				return roundTripped
			},
			wantCandidates:      []int{2},
			wantCandidateLabels: []string{"severity:blocker"},
			wantDeferred:        []int{1},
			wantDeferredLabels:  []string{"severity:normal"},
		},
		{
			name: "manual default priority labels derive rank from priority label",
			snapshot: func(t *testing.T) Snapshot {
				return Snapshot{
					SchemaVersion: 1,
					Kind:          "queueSnapshot",
					Repo:          "example-org/example-repo",
					Issues: []IssueState{
						{Issue: Issue{Number: 1, URL: "https://example/1"}, Eligible: true, Action: "issue-implementation", PriorityLabel: "priority:p2"},
						{Issue: Issue{Number: 2, URL: "https://example/2"}, Eligible: true, Action: "issue-implementation", PriorityLabel: "priority:p0"},
					},
				}
			},
			wantCandidates:      []int{2},
			wantCandidateLabels: []string{"priority:p0"},
			wantDeferred:        []int{1},
			wantDeferredLabels:  []string{"priority:p2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := tt.snapshot(t)
			if tt.wantSnapshotPriorityLabel != "" && snapshot.Issues[0].PriorityLabel != tt.wantSnapshotPriorityLabel {
				t.Fatalf("priority label = %q, want %q", snapshot.Issues[0].PriorityLabel, tt.wantSnapshotPriorityLabel)
			}
			if tt.wantCandidates == nil && tt.wantSelectedAction == "" && tt.wantDeferred == nil {
				return
			}

			recommend := tt.recommend
			if recommend == nil {
				recommend = RecommendNext
			}
			next := recommend(snapshot)
			if tt.wantSelectedAction != "" && next.SelectedAction != tt.wantSelectedAction {
				t.Fatalf("selected action = %q, want %q: %#v", next.SelectedAction, tt.wantSelectedAction, next)
			}
			assertCandidateNumbers(t, next.Candidates, tt.wantCandidates)
			assertCandidatePriorityLabels(t, next.Candidates, tt.wantCandidateLabels)
			assertCandidateNumbers(t, next.DeferredEligibleItems, tt.wantDeferred)
			assertCandidatePriorityLabels(t, next.DeferredEligibleItems, tt.wantDeferredLabels)
			if tt.wantSelectionReason != "" && next.SelectionReason != tt.wantSelectionReason {
				t.Fatalf("selection reason = %q, want %q", next.SelectionReason, tt.wantSelectionReason)
			}
		})
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

func assertCandidateNumbers(t *testing.T, candidates []NextCandidate, want []int) {
	t.Helper()
	if want == nil {
		return
	}
	if len(candidates) != len(want) {
		t.Fatalf("candidate numbers = %#v, want %#v", candidateNumbers(candidates), want)
	}
	for i := range want {
		if candidates[i].Number != want[i] {
			t.Fatalf("candidate numbers = %#v, want %#v", candidateNumbers(candidates), want)
		}
	}
}

func assertCandidatePriorityLabels(t *testing.T, candidates []NextCandidate, want []string) {
	t.Helper()
	if want == nil {
		return
	}
	if len(candidates) != len(want) {
		t.Fatalf("candidate priority labels = %#v, want %#v", candidatePriorityLabels(candidates), want)
	}
	for i := range want {
		if candidates[i].PriorityLabel != want[i] {
			t.Fatalf("candidate priority labels = %#v, want %#v", candidatePriorityLabels(candidates), want)
		}
	}
}

func candidateNumbers(candidates []NextCandidate) []int {
	values := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Number)
	}
	return values
}

func candidatePriorityLabels(candidates []NextCandidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.PriorityLabel)
	}
	return values
}
