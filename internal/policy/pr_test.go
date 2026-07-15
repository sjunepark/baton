package policy

import (
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
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
			name: "agent work PR with one managed issue passes",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123)},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:   PRFlowWork,
			wantErrors: []string{},
		},
		{
			name: "non prefixed PR is unmanaged",
			input: PRPolicyInput{
				PullRequest:      workPullRequestWithHead("Refs #123", "agent/123-issue-policy"),
				ReferencedIssues: []ReferencedIssue{issue(123)},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:   PRFlowUnmanaged,
			wantErrors: []string{},
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
			name: "agent work PR requires referenced issue facts",
			input: PRPolicyInput{
				PullRequest:    workPullRequest("Refs #123"),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "#123 could not be loaded for managed-issue validation.",
		},
		{
			name: "agent work PR requires managed issue ownership",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{{Number: 123, Ownership: IssueOwnershipDecision{SchemaVersion: 1, Source: IssueOwnershipNone}}},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "#123 is not a Baton-managed issue.",
		},
		{
			name: "agent work PR rejects invalid ownership",
			input: PRPolicyInput{
				PullRequest: workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{{Number: 123, Ownership: IssueOwnershipDecision{
					SchemaVersion: 1, Source: IssueOwnershipInvalid, Errors: []string{"record identity mismatch"},
				}}},
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "#123 has invalid managed-issue ownership: record identity mismatch.",
		},
		{
			name: "agent work PR rejects closing keywords",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\n\nCloses #123"),
				ReferencedIssues: []ReferencedIssue{issue(123)},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Work PRs into agent must use Refs #123, not closing keywords.",
		},
		{
			name: "multi issue work requires only durable ownership",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123\nRefs #124"),
				ReferencedIssues: []ReferencedIssue{issue(123), issue(124)},
				CommitMessages:   []string{"Document issue policy"},
			},
			wantFlow:   PRFlowWork,
			wantErrors: []string{},
		},
		{
			name: "unmanaged direct base branch PR is skipped",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Refs #123", "feature"),
				CommitMessages: []string{"fix lint"},
			},
			wantFlow:   PRFlowUnmanaged,
			wantErrors: []string{},
		},
		{
			name: "agent-work PR into base branch must target staging branch first",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Refs #123", "agent-work/123-issue-policy"),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowMisroutedWork,
			wantErrorSubstr: "Baton work PRs from agent-work/* must target agent before promotion to main.",
		},
		{
			name: "partial optional promotion closing references are rejected",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #124", "agent"),
				PromotionFacts: completePromotionFacts(123, 124),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "Promotion closing references into main are optional presentation, but when present must exactly match the sealed plan; missing: #123; extra: none.",
		},
		{
			name: "issue-backed promotion needs no closing keyword",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Promote sealed Baton work", "agent"),
				PromotionFacts: completePromotionFacts(123, 124),
				CommitMessages: []string{"Document issue policy"},
			},
			wantFlow:   PRFlowPromotion,
			wantErrors: []string{},
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
			name: "unrelated optional closing issue disagrees with promotion",
			input: PRPolicyInput{
				PullRequest:    promotionPullRequest("Closes #999", "agent"),
				PromotionFacts: completePromotionFacts(123),
				CommitMessages: []string{"Promote agent work"},
			},
			wantFlow:        PRFlowPromotion,
			wantErrorSubstr: "Promotion closing references into main are optional presentation, but when present must exactly match the sealed plan; missing: #123; extra: #999.",
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
			wantErrorSubstr: "Promotion evidence is incomplete; Baton requires an exact sealed delivery plan with matching cursor and coverage.",
		},
		{
			name: "commit subjects must be meaningful",
			input: PRPolicyInput{
				PullRequest:      workPullRequest("Refs #123"),
				ReferencedIssues: []ReferencedIssue{issue(123)},
				CommitMessages:   []string{"fix lint"},
			},
			wantFlow:        PRFlowWork,
			wantErrorSubstr: "Commit subject is too vague to keep permanently: \"fix lint\".",
		},
		{
			name: "agent work PR fails closed at commit cap",
			input: PRPolicyInput{
				PullRequest:             workPullRequest("Refs #123"),
				ReferencedIssues:        []ReferencedIssue{issue(123)},
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

func TestPromotionPolicyRejectsUnincorporatedOrUnknownBaseIntegration(t *testing.T) {
	cfg := config.DefaultConfig()
	for _, state := range []delivery.BaseIntegrationState{delivery.BaseDirectWorkPending, delivery.BaseIntegrationDiverged, delivery.BaseIntegrationUnknown} {
		t.Run(string(state), func(t *testing.T) {
			facts := completePromotionFacts()
			facts.BaseIntegration.State = state
			decision := ComputePullRequestPolicy(PRPolicyInput{
				PullRequest:    PullRequest{Number: 50, BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch, BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo"},
				PromotionFacts: facts, CommitMessages: []string{"Promote"}, Policy: cfg,
			})
			if len(decision.Errors) == 0 || !strings.Contains(decision.Errors[0], string(state)) {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestComputePullRequestPolicyIgnoresDirectBaseBranchPRs(t *testing.T) {
	cfg := config.DefaultConfig()

	decision := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:    promotionPullRequest("Refs #123", "feature"),
		CommitMessages: []string{"Document issue policy"},
		Policy:         cfg,
	})

	if decision.Flow != PRFlowUnmanaged {
		t.Fatalf("flow = %q, want %q", decision.Flow, PRFlowUnmanaged)
	}
	if len(decision.Errors) != 0 {
		t.Fatalf("errors = %#v, want unmanaged no-op", decision.Errors)
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
		ReferencedIssues: []ReferencedIssue{issue(123)},
		CommitMessages:   []string{"Document issue policy"},
		Policy:           cfg,
	})
	assertStringSlices(t, work.Errors, []string{})

	promotion := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:    promotionPullRequest("Finishes #124", "agent"),
		PromotionFacts: completePromotionFacts(123),
		CommitMessages: []string{"Document issue policy"},
		Policy:         cfg,
	})
	if !containsString(promotion.Errors, "Promotion closing references into main are optional presentation, but when present must exactly match the sealed plan; missing: #123; extra: #124.") {
		t.Fatalf("errors = %#v, want configured closing keyword error", promotion.Errors)
	}
}

func TestComputePullRequestPolicyHonorsDisabledCommitCapRule(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PRPolicy.FailWhenCommitListingReachesCap = false

	decision := ComputePullRequestPolicy(PRPolicyInput{
		PullRequest:             workPullRequest("Refs #123\nRefs #124"),
		ReferencedIssues:        []ReferencedIssue{issue(123), issue(124)},
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

func issue(number int) ReferencedIssue {
	return ReferencedIssue{Number: number, Ownership: IssueOwnershipDecision{SchemaVersion: 1, Managed: true, Source: IssueOwnershipRecord}}
}

func completePromotionFacts(issueNumbers ...int) *PromotionFacts {
	return &PromotionFacts{
		ExpectedIssues: issueNumbers, Complete: true, Source: "sealedDeliveryPlan",
		PlanDigest: "sha256:plan", CursorDigest: "sha256:cursor", CoverageDigest: "sha256:coverage",
		BaseIntegration: delivery.BaseIntegrationFacts{State: delivery.BaseIntegrated, ObservedBaseSHA: strings.Repeat("a", 40), ObservedStagingSHA: strings.Repeat("b", 40)},
	}
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
