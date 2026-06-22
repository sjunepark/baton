package policy

import (
	"testing"

	"github.com/sejunpark/baton/internal/config"
)

func TestComputePullRequestPolicy(t *testing.T) {
	cfg := config.DefaultCreoCompat()
	tests := []struct {
		name            string
		input           PRPolicyInput
		wantErrors      []string
		wantErrorSubstr string
	}{
		{
			name: "agent work PR with one ready trivial issue passes",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantErrors: []string{},
		},
		{
			name: "agent work PR requires refs",
			input: PRPolicyInput{
				PullRequest:    workPullRequest("No linked issue."),
				CommitMessages: []string{"Document issue policy"},
			},
			wantErrorSubstr: "Work PRs into agent must reference at least one issue with Refs #123.",
		},
		{
			name: "agent work PR rejects closing keywords",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\n\nCloses #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantErrorSubstr: "Work PRs into agent must use Refs #123, not closing keywords.",
		},
		{
			name: "agent work PR requires implementation labels",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:investigate-only")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantErrorSubstr: "#123 must have one of: agent:ready-trivial, agent:ready-bounded.",
		},
		{
			name: "agent work PR rejects skip labels",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-bounded", "needs:discussion")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantErrorSubstr: "#123 has skip label needs:discussion.",
		},
		{
			name: "multi issue all trivial rejected",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\nRefs #124"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial"), issue(124, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantErrorSubstr: "Multi-issue PRs cannot be all-trivial in v1; split them or use bounded review.",
		},
		{
			name: "promotion PR must come from agent and close issues",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Refs #123", "feature"),
				CommitMessages: []string{"Document issue policy"},
			},
			wantErrors: []string{
				"Promotion PRs into main must come from agent.",
				"Promotion PRs into main must close promoted issues with Closes #123.",
			},
		},
		{
			name: "promotion PR from agent with closing keywords passes",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #123", "agent"),
				CommitMessages: []string{"Document issue policy"},
			},
			wantErrors: []string{},
		},
		{
			name: "commit subjects must be meaningful",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"fix lint"},
			},
			wantErrorSubstr: "Commit subject is too vague to keep permanently: \"fix lint\".",
		},
		{
			name: "agent work PR fails closed at commit cap",
			input: PRPolicyInput{
				PullRequest:             workPullRequest("Refs #123"),
				ReferencedIssues:        []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:          meaningfulCommits(250),
				CommitListingReachedCap: true,
			},
			wantErrorSubstr: "PR commit listing reached GitHub API cap of 250 commits; commit hygiene cannot be fully verified.",
		},
		{
			name: "promotion PR fails closed at commit cap",
			input: PRPolicyInput{
				PullRequest:             promotionPullRequest("Closes #123", "agent"),
				CommitMessages:          meaningfulCommits(250),
				CommitListingReachedCap: true,
			},
			wantErrorSubstr: "PR commit listing reached GitHub API cap of 250 commits; commit hygiene cannot be fully verified.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.Policy = cfg
			decision := ComputePullRequestPolicy(tt.input)
			if tt.wantErrors != nil {
				assertStringSlices(t, decision.Errors, tt.wantErrors)
			}
			if tt.wantErrorSubstr != "" && !containsString(decision.Errors, tt.wantErrorSubstr) {
				t.Fatalf("errors = %#v, want %q", decision.Errors, tt.wantErrorSubstr)
			}
		})
	}
}

func TestIssueReferenceExtraction(t *testing.T) {
	assertInts(t, ExtractReferenceIssueNumbers("Refs #1, #2\nReferences #3 and #4"), []int{1, 2, 3, 4})
	assertInts(t, ExtractClosingIssueNumbers("Closes #5, #6\nFixes #7\nResolves #8 and #9"), []int{5, 6, 7, 8, 9})
}

func TestIsNoisyCommitSubject(t *testing.T) {
	cfg := config.DefaultCreoCompat()
	assertEqual(t, IsNoisyCommitSubject("fix lint", cfg.PRPolicy.NoisyCommitSubjects), true)
	assertEqual(t, IsNoisyCommitSubject("Fix issue policy docs", cfg.PRPolicy.NoisyCommitSubjects), false)
}

func workPullRequest(body string) PullRequest {
	return PullRequest{
		Number:                 10,
		Title:                  "Update issue policy",
		Body:                   body,
		BaseRef:                "agent",
		HeadRef:                "agent/123-issue-policy",
		BaseRepositoryFullName: "open-creo/creo",
		HeadRepositoryFullName: "open-creo/creo",
	}
}

func promotionPullRequest(body, headRef string) PullRequest {
	return PullRequest{
		Number:                 11,
		Title:                  "Promote agent to main",
		Body:                   body,
		BaseRef:                "main",
		HeadRef:                headRef,
		BaseRepositoryFullName: "open-creo/creo",
		HeadRepositoryFullName: "open-creo/creo",
	}
}

func issue(number int, labels ...string) ReferencedIssue {
	return ReferencedIssue{Number: number, Labels: labels}
}

func meaningfulCommits(count int) []string {
	commits := make([]string, count)
	for i := range commits {
		commits[i] = "Meaningful commit"
	}
	return commits
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func assertInts(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
