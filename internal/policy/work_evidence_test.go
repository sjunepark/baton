package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/delivery"
)

func TestManagedWorkPolicyEvidenceRoundTrip(t *testing.T) {
	evidence := managedWorkEvidenceFixture(t, true)
	body, err := RenderManagedWorkPolicyEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	match, err := FindTrustedManagedWorkPolicyEvidence([]ManagedWorkEvidenceComment{{
		ID: 10, NodeID: "IC_10", Body: body, AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot", CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Evidence.Digest != evidence.Digest || len(match.Evidence.Issues) != 1 || match.Comment.ID != 10 {
		t.Fatalf("match = %+v", match)
	}
}

func TestManagedWorkPolicyEvidenceSelectsNewestAppendOnlyRecord(t *testing.T) {
	evidence := managedWorkEvidenceFixture(t, true)
	body, err := RenderManagedWorkPolicyEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := NewManagedWorkPolicyEvidence(ManagedWorkPolicyEvidence{
		Repository: evidence.Repository, PullRequest: evidence.PullRequest, BaseBranch: evidence.BaseBranch, WorkBranch: evidence.WorkBranch,
		BaseSHA: evidence.BaseSHA, HeadSHA: evidence.HeadSHA, ProseDigest: ManagedWorkProseDigest("Changed", "No refs"),
		PolicySchemaVersion: evidence.PolicySchemaVersion, Allowed: false, Writer: delivery.WriterProvenance{Workflow: "PR Policy", RunID: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	newerBody, err := RenderManagedWorkPolicyEvidence(newer)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	comments := []ManagedWorkEvidenceComment{
		{ID: 10, Body: body, AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot", CreatedAt: before},
		{ID: 11, Body: newerBody, AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot", CreatedAt: before.Add(time.Minute)},
	}
	match, err := FindTrustedManagedWorkPolicyEvidence(comments)
	if err != nil || match == nil || match.Comment.ID != 11 || match.Evidence.Allowed {
		t.Fatalf("match=%+v error=%v", match, err)
	}
}

func TestRejectedManagedWorkPolicyEvidenceAuthorizesNoIssues(t *testing.T) {
	evidence := managedWorkEvidenceFixture(t, false)
	if evidence.Allowed || len(evidence.Issues) != 0 {
		t.Fatalf("evidence = %+v", evidence)
	}
	evidence.Issues = []delivery.ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: NewManagedIssueRecord("I_7", 7).Digest}}
	if _, err := NewManagedWorkPolicyEvidence(evidence); err == nil || !strings.Contains(err.Error(), "must not authorize") {
		t.Fatalf("error = %v", err)
	}
}

func TestManagedWorkPolicyEvidenceRejectsTrailingJSON(t *testing.T) {
	body, err := RenderManagedWorkPolicyEvidence(managedWorkEvidenceFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	body = strings.TrimSuffix(body, " -->") + " {} -->"
	_, err = FindTrustedManagedWorkPolicyEvidence([]ManagedWorkEvidenceComment{{ID: 10, Body: body, AuthorLogin: ManagedIssueTrustedLogin, AuthorType: "Bot"}})
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("error = %v", err)
	}
}

func managedWorkEvidenceFixture(t *testing.T, allowed bool) ManagedWorkPolicyEvidence {
	t.Helper()
	issues := []delivery.ManagedIssueReference{}
	if allowed {
		issues = append(issues, delivery.ManagedIssueReference{Number: 7, NodeID: "I_7", OwnershipDigest: NewManagedIssueRecord("I_7", 7).Digest})
	}
	evidence, err := NewManagedWorkPolicyEvidence(ManagedWorkPolicyEvidence{
		Repository:  delivery.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		PullRequest: delivery.ResourceIdentity{Number: 20, NodeID: "PR_20"}, BaseBranch: "agent", WorkBranch: "agent-work/7",
		BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), ProseDigest: ManagedWorkProseDigest("Work", "Refs #7"),
		PolicySchemaVersion: 4, Allowed: allowed, Issues: issues, Writer: delivery.WriterProvenance{Workflow: "PR Policy", RunID: 99},
	})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
