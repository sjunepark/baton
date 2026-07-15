package policy

import (
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestClassifyIssueOwnership(t *testing.T) {
	cfg := config.DefaultConfig()
	record := NewManagedIssueRecord("I_7", 7)
	trusted := IssueOwnershipComment{ID: 1, Body: RenderManagedIssueRecord(record), AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot"}
	tests := []struct {
		name       string
		input      IssueOwnershipInput
		managed    bool
		source     IssueOwnershipSource
		diagnostic string
		errorText  string
	}{
		{name: "trusted record survives body and label edits", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Body: "body was replaced", Comments: []IssueOwnershipComment{trusted}}, managed: true, source: IssueOwnershipRecord},
		{name: "equivalent duplicates remain managed", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Comments: []IssueOwnershipComment{trusted, {ID: 2, Body: trusted.Body, AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot"}}}, managed: true, source: IssueOwnershipRecord, diagnostic: "duplicate equivalent"},
		{name: "untrusted marker is ignored", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Comments: []IssueOwnershipComment{{ID: 3, Body: trusted.Body, AuthorLogin: "contributor", AuthorType: "User"}}}, source: IssueOwnershipNone, diagnostic: "ignored untrusted"},
		{name: "contradictory trusted records fail closed", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Comments: []IssueOwnershipComment{trusted, {ID: 4, Body: RenderManagedIssueRecord(NewManagedIssueRecord("I_other", 8)), AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot"}}}, source: IssueOwnershipInvalid, errorText: "identity does not match"},
		{name: "index without record fails closed", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Labels: []string{ManagedIssueIndexLabel}}, source: IssueOwnershipInvalid, errorText: "index exists without"},
		{name: "unacquired comments do not make the index falsely invalid", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Labels: []string{ManagedIssueIndexLabel}, CommentsUnavailable: true}, source: IssueOwnershipUnavailable, diagnostic: "not acquired"},
		{name: "legacy form fingerprint is bounded migration evidence", input: IssueOwnershipInput{IssueNodeID: "I_7", IssueNumber: 7, Body: issueBody(issueBodyInput{}), Policy: cfg.IssuePolicy}, managed: true, source: IssueOwnershipLegacyFingerprint, diagnostic: "requires record backfill"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := ClassifyIssueOwnership(test.input)
			if decision.Managed != test.managed || decision.Source != test.source {
				t.Fatalf("decision = %+v", decision)
			}
			if test.diagnostic != "" && !strings.Contains(strings.Join(decision.Diagnostics, " "), test.diagnostic) {
				t.Fatalf("diagnostics = %v, want %q", decision.Diagnostics, test.diagnostic)
			}
			if test.errorText != "" && !strings.Contains(strings.Join(decision.Errors, " "), test.errorText) {
				t.Fatalf("errors = %v, want %q", decision.Errors, test.errorText)
			}
		})
	}
}

func TestClassifyPullRequestFlowUsesRepositoryIdentity(t *testing.T) {
	cfg := config.DefaultConfig()
	sameRepo := func(base, head string) PullRequest {
		return PullRequest{BaseRef: base, HeadRef: head, BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo"}
	}
	tests := []struct {
		name string
		pr   PullRequest
		want PRFlow
	}{
		{name: "managed work", pr: sameRepo("agent", "agent-work/7"), want: PRFlowWork},
		{name: "promotion", pr: sameRepo("main", "agent"), want: PRFlowPromotion},
		{name: "misrouted prefix", pr: sameRepo("release", "agent-work/7"), want: PRFlowMisroutedWork},
		{name: "forked managed shape", pr: PullRequest{BaseRef: "agent", HeadRef: "agent-work/7", BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "fork/repo"}, want: PRFlowUnmanaged},
		{name: "missing identity", pr: PullRequest{BaseRef: "agent", HeadRef: "agent-work/7", BaseRepositoryFullName: "example/repo"}, want: PRFlowIndeterminate},
		{name: "ordinary branch", pr: sameRepo("agent", "feature/7"), want: PRFlowUnmanaged},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyPullRequestFlow(test.pr, cfg); got != test.want {
				t.Fatalf("flow = %q, want %q", got, test.want)
			}
		})
	}
}
