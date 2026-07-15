package config

import (
	"encoding/json"
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
	if len(cfg.IssuePolicy.PriorityLabels) != 0 {
		t.Fatalf("legacy config unexpectedly enabled priority: %#v", cfg.IssuePolicy.PriorityLabels)
	}
	if _, ok := cfg.IssuePolicy.ControlledLabelGroups["priority"]; ok {
		t.Fatalf("legacy config unexpectedly added priority controlled group: %#v", cfg.IssuePolicy.ControlledLabelGroups["priority"])
	}
}

func TestDefaultConfigIncludesPriorityPolicy(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Setup.BaselineBatonVersion == "" {
		t.Fatalf("baseline Baton version = %q", cfg.Setup.BaselineBatonVersion)
	}
	if cfg.IssuePolicy.FormSections["priority"] != "Priority" {
		t.Fatalf("priority form section = %q", cfg.IssuePolicy.FormSections["priority"])
	}
	if cfg.IssuePolicy.PriorityLabels["P0"] != "priority:p0" || cfg.IssuePolicy.PriorityLabels["P3"] != "priority:p3" {
		t.Fatalf("priority labels = %#v", cfg.IssuePolicy.PriorityLabels)
	}
	wantGroup := []string{"priority:p0", "priority:p1", "priority:p2", "priority:p3"}
	gotGroup := cfg.IssuePolicy.ControlledLabelGroups["priority"]
	if len(gotGroup) != len(wantGroup) {
		t.Fatalf("priority group = %#v", gotGroup)
	}
	for i := range wantGroup {
		if gotGroup[i] != wantGroup[i] {
			t.Fatalf("priority group = %#v, want %#v", gotGroup, wantGroup)
		}
	}
}

func TestAwaitingReviewLabelIsExplicitAndBlocksIntake(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.IssuePolicy.AwaitingReviewLabel != "needs:review" || !containsFold(cfg.IssuePolicy.SkipLabels, cfg.IssuePolicy.AwaitingReviewLabel) {
		t.Fatalf("awaiting review policy = %+v", cfg.IssuePolicy)
	}
	cfg.IssuePolicy.SkipLabels = []string{"needs-info"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "skip_labels") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLoadOldBatonConfigWithoutPriorityDoesNotEnablePriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(path, []byte(oldBatonPolicyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IssuePolicy.PriorityLabels) != 0 {
		t.Fatalf("old Baton config unexpectedly enabled priority: %#v", cfg.IssuePolicy.PriorityLabels)
	}
	if _, ok := cfg.IssuePolicy.FormSections["priority"]; ok {
		t.Fatalf("old Baton config unexpectedly added priority form section")
	}
	if cfg.Setup.BaselineBatonVersion != "" {
		t.Fatalf("old Baton config unexpectedly defaulted setup baseline: %q", cfg.Setup.BaselineBatonVersion)
	}
}

func TestLoadPreservesSetupBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baton.yml")
	content := strings.Replace(oldBatonPolicyYAML, "version: 1\n", "version: 1\nsetup:\n  baseline_baton_version: v0.4.1\n", 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup.BaselineBatonVersion != "v0.4.1" {
		t.Fatalf("baseline Baton version = %q", cfg.Setup.BaselineBatonVersion)
	}
}

func TestValidateRejectsUnknownRequiredSection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IssuePolicy.RequiredSections["ready-trivial"] = []string{"missing"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsManagedBranchesInsideWorkNamespace(t *testing.T) {
	tests := []struct {
		name  string
		field string
		edit  func(*Config)
	}{
		{name: "base branch", field: "base_branch", edit: func(cfg *Config) { cfg.Repository.BaseBranch = "agent-work/main" }},
		{name: "staging branch", field: "staging_branch", edit: func(cfg *Config) { cfg.Repository.StagingBranch = "agent-work/staging" }},
		{name: "base reported first when both overlap", field: "base_branch", edit: func(cfg *Config) {
			cfg.Repository.BaseBranch = "agent-work/main"
			cfg.Repository.StagingBranch = "agent-work/staging"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			test.edit(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.field+" must not fall under repository.work_branch_prefix") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateAcceptsBranchesOutsideCaseSensitiveWorkNamespace(t *testing.T) {
	tests := []struct {
		name   string
		branch string
	}{
		{name: "prefix without separator", branch: "agent-work"},
		{name: "similar prefix", branch: "agent-worker/staging"},
		{name: "different case", branch: "Agent-Work/staging"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Repository.BaseBranch = test.branch
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsInvalidPriorityMappings(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{
			name: "missing form section",
			edit: func(cfg *Config) {
				delete(cfg.IssuePolicy.FormSections, "priority")
			},
		},
		{
			name: "mapped label outside controlled group",
			edit: func(cfg *Config) {
				cfg.IssuePolicy.PriorityLabels["P0"] = "priority:urgent"
			},
		},
		{
			name: "controlled label without mapping",
			edit: func(cfg *Config) {
				cfg.IssuePolicy.ControlledLabelGroups["priority"] = append(cfg.IssuePolicy.ControlledLabelGroups["priority"], "priority:p4")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
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
	if !contains(text, "setup:") || !contains(text, "baseline_baton_version: "+DefaultConfig().Setup.BaselineBatonVersion) {
		t.Fatalf("setup baseline missing from config yaml:\n%s", text)
	}
}

func TestDeliveryLocatorRoundTripsAndIsOptional(t *testing.T) {
	cfg := DefaultConfig()
	content, err := MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "delivery:") {
		t.Fatalf("default config unexpectedly enables delivery:\n%s", content)
	}
	cfg.Delivery = &DeliveryConfig{
		Authority:  DeliveryAuthorityShadow,
		Host:       "github.com",
		Repository: DeliveryRepository{FullName: "example/repo", NodeID: "R_1"},
		Issue:      DeliveryResource{Number: 900, NodeID: "I_900"},
		Checkpoint: DeliveryComment{DatabaseID: 100, NodeID: "IC_100"},
	}
	content, err = MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Delivery == nil || *got.Delivery != *cfg.Delivery {
		t.Fatalf("delivery = %+v, want %+v", got.Delivery, cfg.Delivery)
	}
}

func TestDeliveryLocatorMustBeComplete(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Delivery = &DeliveryConfig{Authority: DeliveryAuthorityShadow, Host: "github.com"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "delivery.repository") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDeliveryAuthorityMustBeExplicit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Delivery = &DeliveryConfig{
		Host: "github.com", Repository: DeliveryRepository{FullName: "example/repo", NodeID: "R_1"},
		Issue: DeliveryResource{Number: 900, NodeID: "I_900"}, Checkpoint: DeliveryComment{DatabaseID: 100, NodeID: "IC_100"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "delivery.authority") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLoadAcceptsRetiredPRPolicyOptionsAndPreservesCurrentBooleans(t *testing.T) {
	for _, retiredValue := range []string{"true", "false"} {
		t.Run(retiredValue, func(t *testing.T) {
			content, err := MarshalYAML(DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			retired := "    allow_direct_base_branch_prs: " + retiredValue + "\n    reject_all_trivial_multi_issue_prs: " + retiredValue + "\n"
			text := strings.Replace(string(content), "pr_policy:\n", "pr_policy:\n"+retired, 1)
			text = strings.ReplaceAll(text, "fail_when_commit_listing_reaches_cap: true", "fail_when_commit_listing_reaches_cap: false")
			path := filepath.Join(t.TempDir(), "baton.yml")
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PRPolicy.FailWhenCommitListingReachesCap {
				t.Fatal("fail_when_commit_listing_reaches_cap explicit false was defaulted to true")
			}
		})
	}
}

func TestMarshalOmitsRetiredPRPolicyOptions(t *testing.T) {
	content, err := MarshalYAML(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(content), "pr_policy:\n", "pr_policy:\n    allow_direct_base_branch_prs: true\n    reject_all_trivial_multi_issue_prs: true\n", 1)
	text = strings.ReplaceAll(text, "fail_when_commit_listing_reaches_cap: true", "fail_when_commit_listing_reaches_cap: false")
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "allow_direct_base_branch_prs") || strings.Contains(string(encoded), "reject_all_trivial_multi_issue_prs") {
		t.Fatalf("retired option was emitted:\n%s", encoded)
	}
}

func TestCompiledPolicyJSONOmitsRetiredPRPolicyOptions(t *testing.T) {
	content, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "allowDirectBaseBranchPRs") || strings.Contains(string(content), "rejectAllTrivialMultiIssuePRs") {
		t.Fatalf("compiled policy contains retired option: %s", content)
	}
}

func TestLoadRejectsUnknownFieldsAndUnsupportedVersions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "unknown current field", content: strings.Replace(oldBatonPolicyYAML, "version: 1\n", "version: 1\nunknown: true\n", 1), want: "field unknown not found"},
		{name: "unknown nested field", content: strings.Replace(oldBatonPolicyYAML, "  default_remote: origin\n", "  default_remote: origin\n  typo_remote: upstream\n", 1), want: "field typo_remote not found"},
		{name: "unknown legacy field", content: legacyPolicyYAML + "typo_marker: value\n", want: "field typo_marker not found"},
		{name: "unsupported version", content: strings.Replace(oldBatonPolicyYAML, "version: 1", "version: 2", 1), want: "unsupported config version 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "baton.yml")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsInvalidCompiledIssuePolicy(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{name: "empty marker", edit: func(cfg *Config) { cfg.IssuePolicy.PolicyCommentMarker = "" }, want: "stable versioned HTML comment"},
		{name: "unstable marker", edit: func(cfg *Config) { cfg.IssuePolicy.PolicyCommentMarker = "<!-- baton -->" }, want: "stable versioned HTML comment"},
		{name: "duplicate controlled label", edit: func(cfg *Config) { cfg.IssuePolicy.ControlledLabelGroups["quality_gate"] = []string{"bug"} }, want: "appears in both"},
		{name: "unmapped implementation label", edit: func(cfg *Config) {
			cfg.IssuePolicy.ImplementationLabels = append(cfg.IssuePolicy.ImplementationLabels, "agent:unknown")
		}, want: "unmapped agent-mode label"},
		{name: "unknown required mode", edit: func(cfg *Config) { cfg.IssuePolicy.RequiredSections["unknown-mode"] = []string{"summary"} }, want: "does not match an agent_mode_labels option"},
		{name: "duplicate heading", edit: func(cfg *Config) { cfg.IssuePolicy.FormSections["notes"] = cfg.IssuePolicy.FormSections["summary"] }, want: "duplicates heading"},
		{name: "invalid base branch", edit: func(cfg *Config) { cfg.Repository.BaseBranch = "release..next" }, want: "not a valid git branch"},
		{name: "invalid staging branch", edit: func(cfg *Config) { cfg.Repository.StagingBranch = "feature:next" }, want: "invalid git ref character"},
		{name: "invalid base reported first", edit: func(cfg *Config) {
			cfg.Repository.BaseBranch = "release..next"
			cfg.Repository.StagingBranch = "feature:next"
		}, want: "repository.base_branch"},
		{name: "same base and staging branch", edit: func(cfg *Config) { cfg.Repository.StagingBranch = cfg.Repository.BaseBranch }, want: "must differ"},
		{name: "symbolic head branch", edit: func(cfg *Config) { cfg.Repository.StagingBranch = "HEAD" }, want: "not a valid git branch"},
		{name: "option-like remote", edit: func(cfg *Config) { cfg.Repository.DefaultRemote = "--upload-pack" }, want: "non-option git remote"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			test.edit(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMarshalYAMLOmitsObsoleteAutomationPolicy(t *testing.T) {
	content, err := MarshalYAML(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "automation:") || strings.Contains(string(content), "allow_merge") || strings.Contains(string(content), "prefer_pr_followup") {
		t.Fatalf("obsolete automation policy was emitted:\n%s", content)
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

const oldBatonPolicyYAML = `version: 1
repository:
  default_remote: origin
  base_branch: main
  staging_branch: agent
  work_branch_prefix: agent-work/
issue_policy:
  policy_comment_marker: '<!-- baton-issue-policy:v1 -->'
  form_sections:
    work_kind: Work kind
    agent_mode: Agent mode
    summary: Summary
    context_evidence: Context / evidence
    acceptance_criteria: Acceptance criteria
  work_kind_labels:
    Bug: bug
  agent_mode_labels:
    Ready trivial: agent:ready-trivial
  controlled_label_groups:
    work_kind:
      - bug
    agent_mode:
      - agent:ready-trivial
    quality_gate:
      - needs-info
  implementation_labels:
    - agent:ready-trivial
  comment_only_labels: []
  skip_labels:
    - needs-info
  required_sections:
    ready-trivial:
      - summary
      - context_evidence
      - acceptance_criteria
pr_policy:
  required_reference_keyword: Refs
labels:
  manifest: .github/labels.yml
automation:
  prefer_pr_followup_before_issue_intake: true
`
