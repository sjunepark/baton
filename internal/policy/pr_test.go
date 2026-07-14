package policy

import (
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestComputePullRequestPolicy(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name            string
		input           PRPolicyInput
		wantFlow        PRFlow
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
			wantFlow:   PRFlowWork,
			wantErrors: []string{},
		},
		{
			name: "agent work PR requires work branch prefix",
			input: PRPolicyInput{
				PullRequest:      workPullRequestWithHead("Refs #123", "agent/123-issue-policy"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Work PR branches into agent must start with agent-work/; agent/... is reserved by the shared staging branch.",
		},
		{
			name: "agent work PR requires refs",
			input: PRPolicyInput{
				PullRequest:    workPullRequest("No linked issue."),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Work PRs into agent must reference at least one issue with Refs #123.",
		},
		{
			name: "agent work PR rejects closing keywords",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\n\nCloses #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Work PRs into agent must use Refs #123, not closing keywords.",
		},
		{
			name: "agent work PR requires implementation labels",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:investigate-only")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "#123 must have one of: agent:ready-trivial, agent:ready-bounded.",
		},
		{
			name: "agent work PR rejects skip labels",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-bounded", "needs:discussion")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "#123 has skip label needs:discussion.",
		},
		{
			name: "multi issue all trivial rejected",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\nRefs #124"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial"), issue(124, "agent:ready-trivial")},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Multi-issue PRs cannot be all-trivial in v1; split them or use bounded review.",
		},
		{
			name: "direct base branch PR is skipped by default",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Refs #123", "feature"),
				CommitMessages: []string{"fix lint"},
			},
			wantFlow:   PRFlowDirectBase,
			wantErrors: []string{},
		},
		{
			name: "agent-work PR into base branch must target staging branch first",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Refs #123", "agent-work/123-issue-policy"),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowInvalidDirectWork,
			wantErrorSubstr: "Baton work PRs from agent-work/* must target agent before promotion to main.",
		},
		{
			name: "issue-backed promotion requires every expected issue",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #124", "agent"),
				PromotionFacts: completePromotionFacts(123, 124),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "Promotion PRs into main must close every included Baton issue with Closes; missing: #123.",
		},
		{
			name: "issue-backed promotion with every expected issue passes",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #124, #123", "agent"),
				PromotionFacts: completePromotionFacts(123, 124),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:   PRFlowPromotion,
			wantErrors: []string{},
		},
		{
			name: "manual-only promotion needs no closing keyword",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Manual maintenance", "agent"),
				PromotionFacts: completePromotionFacts(),
				CommitMessages: []string{"Maintain deployment files"},
			},
			wantFlow:   PRFlowPromotion,
			wantErrors: []string{},
		},
		{
			name: "unrelated closing issue does not satisfy promotion",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #999", "agent"),
				PromotionFacts: completePromotionFacts(123),
				CommitMessages: []string{"Promote agent work"},
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "Promotion PRs into main must close every included Baton issue with Closes; missing: #123.",
		},
		{
			name: "mixed promotion requires only Baton issues",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Includes manual changes. Closes #123", "agent"),
				PromotionFacts: completePromotionFacts(123),
				CommitMessages: []string{"Promote mixed changes"},
			},
			wantFlow:   PRFlowPromotion,
			wantErrors: []string{},
		},
		{
			name: "incomplete promotion evidence fails explicitly",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #123", "agent"),
				PromotionFacts: &PromotionFacts{ExpectedIssues: []int{123}},
				CommitMessages: []string{"Promote agent work"},
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "Promotion evidence is incomplete; Baton could not verify every included work PR and its issue references between the promotion base and head revisions.",
		},
		{
			name: "commit subjects must be meaningful",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
				CommitMessages:   []string{"fix lint"},
			},
			wantFlow:        PRFlowWork,
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
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "PR commit listing reached GitHub API cap of 250 commits; commit hygiene cannot be fully verified.",
		},
		{
			name: "promotion PR fails closed at commit cap",
			input: PRPolicyInput{
				PullRequest:             promotionPullRequest("Closes #123", "agent"),
				PromotionFacts:          completePromotionFacts(123),
				CommitMessages:          meaningfulCommits(250),
				CommitListingReachedCap: true,
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "PR commit listing reached GitHub API cap of 250 commits; commit hygiene cannot be fully verified.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.Policy = cfg
			decision := ComputePullRequestPolicy(tt.input)
			if tt.wantFlow != "" && decision.Flow != tt.wantFlow {
				t.Fatalf("flow = %q, want %q", decision.Flow, tt.wantFlow)
			}
			if tt.wantErrors != nil {
				assertStringSlices(t, decision.Errors, tt.wantErrors)
			}
			if tt.wantErrorSubstr != "" && !containsString(decision.Errors, tt.wantErrorSubstr) {
				t.Fatalf("errors = %#v, want %q", decision.Errors, tt.wantErrorSubstr)
			}
		})
	}
}

func TestComputePullRequestPolicyCanRejectDirectBaseBranchPRs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PRPolicy.AllowDirectBaseBranchPRs = false

	decision := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:    promotionPullRequest("Refs #123", "feature"),
		CommitMessages: []string{"Document issue policy"},
		Policy:         cfg,
	})

	if decision.Flow != PRFlowDirectBase {
		t.Fatalf("flow = %q, want %q", decision.Flow, PRFlowDirectBase)
	}
	if !containsString(decision.Errors, "Direct PRs into main are disabled by Baton policy; target agent for Baton-managed work or use agent for promotion.") {
		t.Fatalf("errors = %#v, want direct-base policy error", decision.Errors)
	}
}

func TestIssueReferenceExtraction(t *testing.T) {
	assertInts(t, ExtractReferenceIssueNumbers("Refs #1, #2\nReferences #3 and #4"), []int{1, 2, 3, 4})
	assertInts(t, ExtractClosingIssueNumbers("Closes #5, #6\nFixes #7\nResolves #8 and #9"), []int{5, 6, 7, 8, 9})
}

func TestComputePullRequestPolicyUsesConfiguredKeywords(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PRPolicy.RequiredReferenceKeyword = "Relates"
	cfg.PRPolicy.ForbiddenClosingKeywords = []string{"Finishes"}

	work := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:      workPullRequest("Relates #123\n\nCloses #123"),
		ReferencedIssues: []ReferencedIssue{issue(123, "agent:ready-trivial")},
		CommitMessages:   []string{"Document issue policy"},
		Policy:           cfg,
	})
	assertStringSlices(t, work.Errors, []string{})

	promotion := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:    promotionPullRequest("Closes #123", "agent"),
		PromotionFacts: completePromotionFacts(123),
		CommitMessages: []string{"Document issue policy"},
		Policy:         cfg,
	})
	if !containsString(promotion.Errors, "Promotion PRs into main must close every included Baton issue with Finishes; missing: #123.") {
		t.Fatalf("errors = %#v, want configured closing keyword error", promotion.Errors)
	}
}

func TestComputePullRequestPolicyHonorsFalsePRPolicyOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PRPolicy.FailWhenCommitListingReachesCap = false
	cfg.PRPolicy.RejectAllTrivialMultiIssuePRs = false

	decision := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:             workPullRequest("Refs #123\nRefs #124"),
		ReferencedIssues:        []ReferencedIssue{issue(123, "agent:ready-trivial"), issue(124, "agent:ready-trivial")},
		CommitMessages:          meaningfulCommits(250),
		CommitListingReachedCap: true,
		Policy:                  cfg,
	})
	assertStringSlices(t, decision.Errors, []string{})
}

func TestIsNoisyCommitSubject(t *testing.T) {
	cfg := config.DefaultConfig()
	assertEqual(t, IsNoisyCommitSubject("fix lint", cfg.PRPolicy.NoisyCommitSubjects), true)
	assertEqual(t, IsNoisyCommitSubject("Fix issue policy docs", cfg.PRPolicy.NoisyCommitSubjects), false)
}

func workPullRequest(body string) PullRequest {
	return workPullRequestWithHead(body, "agent-work/123-issue-policy")
}

func workPullRequestWithHead(body, headRef string) PullRequest {
	return PullRequest{
		Number:                 10,
		Title:                  "Update issue policy",
		Body:                   body,
		BaseRef:                "agent",
		HeadRef:                headRef,
		BaseRepositoryFullName: "example-org/example-repo",
		HeadRepositoryFullName: "example-org/example-repo",
	}
}

func promotionPullRequest(body, headRef string) PullRequest {
	return PullRequest{
		Number:                 11,
		Title:                  "Promote agent to main",
		Body:                   body,
		BaseRef:                "main",
		HeadRef:                headRef,
		BaseRepositoryFullName: "example-org/example-repo",
		HeadRepositoryFullName: "example-org/example-repo",
	}
}

func issue(number int, labels ...string) ReferencedIssue {
	return ReferencedIssue{Number: number, Labels: labels}
}

func completePromotionFacts(issueNumbers ...int) *PromotionFacts {
	return &PromotionFacts{ExpectedIssues: issueNumbers, Complete: true}
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
