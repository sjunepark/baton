package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLegacyIssuePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-issue-policy.yml")
	if err := os.WriteFile(path, []byte(legacyPolicyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.StagingBranch != "agent" {
		t.Fatalf("staging branch = %q", cfg.Repository.StagingBranch)
	}
	if cfg.IssuePolicy.FormSections["acceptance_criteria"] != "Acceptance criteria" {
		t.Fatalf("acceptance section = %q", cfg.IssuePolicy.FormSections["acceptance_criteria"])
	}
	if got := cfg.IssuePolicy.ImplementationLabels; len(got) != 2 || got[0] != "agent:ready-trivial" || got[1] != "agent:ready-bounded" {
		t.Fatalf("implementation labels = %#v", got)
	}
}

func TestValidateRejectsUnknownRequiredSection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IssuePolicy.RequiredSections["ready-trivial"] = []string{"missing"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestMarshalYAMLUsesBatonShape(t *testing.T) {
	content, err := MarshalYAML(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !contains(text, "issue_policy:") || !contains(text, "repository:") {
		t.Fatalf("unexpected yaml:\n%s", text)
	}
	if contains(text, "schemaVersion") || contains(text, "schemaversion") {
		t.Fatalf("schemaVersion should not be written to config yaml:\n%s", text)
	}
}

func TestLoadPreservesExplicitFalsePRPolicyBooleans(t *testing.T) {
	content, err := MarshalYAML(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(content), "allow_direct_base_branch_prs: true", "allow_direct_base_branch_prs: false")
	text = strings.ReplaceAll(text, "reject_all_trivial_multi_issue_prs: true", "reject_all_trivial_multi_issue_prs: false")
	text = strings.ReplaceAll(text, "fail_when_commit_listing_reaches_cap: true", "fail_when_commit_listing_reaches_cap: false")
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PRPolicy.AllowDirectBaseBranchPRs {
		t.Fatal("allow_direct_base_branch_prs explicit false was defaulted to true")
	}
	if cfg.PRPolicy.RejectAllTrivialMultiIssuePRs {
		t.Fatal("reject_all_trivial_multi_issue_prs explicit false was defaulted to true")
	}
	if cfg.PRPolicy.FailWhenCommitListingReachesCap {
		t.Fatal("fail_when_commit_listing_reaches_cap explicit false was defaulted to true")
	}
}

func TestLoadDefaultsDirectBaseBranchPRsToAllowed(t *testing.T) {
	content, err := MarshalYAML(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(content), "reject_all_trivial_multi_issue_prs: true", "reject_all_trivial_multi_issue_prs: false")
	text = strings.ReplaceAll(text, "fail_when_commit_listing_reaches_cap: true", "fail_when_commit_listing_reaches_cap: false")
	text = strings.ReplaceAll(text, "    allow_direct_base_branch_prs: true\n", "")
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PRPolicy.AllowDirectBaseBranchPRs {
		t.Fatal("allow_direct_base_branch_prs should default to true")
	}
}

func contains(text, needle string) bool {
	return strings.Contains(text, needle)
}

const legacyPolicyYAML = `target_branch: agent
policy_comment_marker: '<!-- legacy-agent-issue-policy:v1 -->'

form_sections:
  work_kind: Work kind
  agent_mode: Agent mode
  summary: Summary
  context_evidence: Context / evidence
  acceptance_criteria: Acceptance criteria

work_kind_labels:
  Bug: bug
  Documentation: documentation
  Enhancement: enhancement
  Question: question

agent_mode_labels:
  Ready trivial: agent:ready-trivial
  Ready bounded: agent:ready-bounded
  Investigate only: agent:investigate-only
  Needs discussion: needs:discussion

controlled_label_groups:
  work_kind:
    - bug
    - documentation
    - enhancement
    - question
  agent_mode:
    - agent:ready-trivial
    - agent:ready-bounded
    - agent:investigate-only
    - needs:discussion
  quality_gate:
    - needs-info

implementation_labels:
  - agent:ready-trivial
  - agent:ready-bounded

comment_only_labels:
  - agent:investigate-only

skip_labels:
  - needs-info
  - needs:discussion
  - needs:review

required_sections:
  ready-trivial:
    - summary
    - context_evidence
    - acceptance_criteria
  ready-bounded:
    - summary
    - context_evidence
    - acceptance_criteria
`
