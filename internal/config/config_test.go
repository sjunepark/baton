package config

import (
	"os"
	"path/filepath"
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
	cfg := DefaultCreoCompat()
	cfg.IssuePolicy.RequiredSections["ready-trivial"] = []string{"missing"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

const legacyPolicyYAML = `target_branch: agent
policy_comment_marker: '<!-- creo-agent-issue-policy:v1 -->'

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
    - agent:blocked

implementation_labels:
  - agent:ready-trivial
  - agent:ready-bounded

comment_only_labels:
  - agent:investigate-only

skip_labels:
  - agent:blocked
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
