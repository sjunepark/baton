package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var ErrConfigNotFound = errors.New("baton config not found")

type Config struct {
	SchemaVersion int              `json:"schemaVersion" yaml:"-"`
	Version       int              `json:"version" yaml:"version"`
	Setup         SetupConfig      `json:"setup,omitempty" yaml:"setup,omitempty"`
	Repository    RepositoryConfig `json:"repository" yaml:"repository"`
	IssuePolicy   IssuePolicy      `json:"issuePolicy" yaml:"issue_policy"`
	PRPolicy      PRPolicy         `json:"prPolicy" yaml:"pr_policy"`
	Labels        LabelsConfig     `json:"labels" yaml:"labels"`
	Automation    AutomationConfig `json:"automation" yaml:"automation"`
}

type SetupConfig struct {
	BaselineBatonVersion string `json:"baselineBatonVersion,omitempty" yaml:"baseline_baton_version,omitempty"`
}

type RepositoryConfig struct {
	DefaultRemote    string `json:"defaultRemote" yaml:"default_remote"`
	BaseBranch       string `json:"baseBranch" yaml:"base_branch"`
	StagingBranch    string `json:"stagingBranch" yaml:"staging_branch"`
	WorkBranchPrefix string `json:"workBranchPrefix" yaml:"work_branch_prefix"`
}

type IssuePolicy struct {
	PolicyCommentMarker   string              `json:"policyCommentMarker" yaml:"policy_comment_marker"`
	FormSections          map[string]string   `json:"formSections" yaml:"form_sections"`
	WorkKindLabels        map[string]string   `json:"workKindLabels" yaml:"work_kind_labels"`
	AgentModeLabels       map[string]string   `json:"agentModeLabels" yaml:"agent_mode_labels"`
	PriorityLabels        map[string]string   `json:"priorityLabels,omitempty" yaml:"priority_labels,omitempty"`
	ControlledLabelGroups map[string][]string `json:"controlledLabelGroups" yaml:"controlled_label_groups"`
	ImplementationLabels  []string            `json:"implementationLabels" yaml:"implementation_labels"`
	CommentOnlyLabels     []string            `json:"commentOnlyLabels" yaml:"comment_only_labels"`
	SkipLabels            []string            `json:"skipLabels" yaml:"skip_labels"`
	RequiredSections      map[string][]string `json:"requiredSections" yaml:"required_sections"`
}

type PRPolicy struct {
	RequiredReferenceKeyword        string   `json:"requiredReferenceKeyword" yaml:"required_reference_keyword"`
	ForbiddenClosingKeywords        []string `json:"forbiddenClosingKeywords" yaml:"forbidden_closing_keywords"`
	AllowDirectBaseBranchPRs        bool     `json:"allowDirectBaseBranchPRs" yaml:"allow_direct_base_branch_prs"`
	RejectAllTrivialMultiIssuePRs   bool     `json:"rejectAllTrivialMultiIssuePRs" yaml:"reject_all_trivial_multi_issue_prs"`
	NoisyCommitSubjects             []string `json:"noisyCommitSubjects" yaml:"noisy_commit_subjects"`
	FailWhenCommitListingReachesCap bool     `json:"failWhenCommitListingReachesCap" yaml:"fail_when_commit_listing_reaches_cap"`

	allowDirectBaseBranchPRsSet        bool
	rejectAllTrivialMultiIssuePRsSet   bool
	failWhenCommitListingReachesCapSet bool
}

type LabelsConfig struct {
	Manifest string `json:"manifest" yaml:"manifest"`
}

type AutomationConfig struct {
	PreferPRFollowupBeforeIssueIntake bool `json:"preferPRFollowupBeforeIssueIntake" yaml:"prefer_pr_followup_before_issue_intake"`
	AllowMerge                        bool `json:"allowMerge" yaml:"allow_merge"`
}

type legacyIssuePolicy struct {
	TargetBranch          string              `yaml:"target_branch"`
	WorkBranchPrefix      string              `yaml:"work_branch_prefix"`
	PolicyCommentMarker   string              `yaml:"policy_comment_marker"`
	FormSections          map[string]string   `yaml:"form_sections"`
	WorkKindLabels        map[string]string   `yaml:"work_kind_labels"`
	AgentModeLabels       map[string]string   `yaml:"agent_mode_labels"`
	PriorityLabels        map[string]string   `yaml:"priority_labels"`
	ControlledLabelGroups map[string][]string `yaml:"controlled_label_groups"`
	ImplementationLabels  []string            `yaml:"implementation_labels"`
	CommentOnlyLabels     []string            `yaml:"comment_only_labels"`
	SkipLabels            []string            `yaml:"skip_labels"`
	RequiredSections      map[string][]string `yaml:"required_sections"`
}

func LoadForRepo(root string) (Config, error) {
	for _, path := range []string{
		filepath.Join(root, ".github", "baton.yml"),
		filepath.Join(root, ".github", "agent-issue-policy.yml"),
	} {
		cfg, err := Load(path)
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}
	return Config{}, ErrConfigNotFound
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var probe struct {
		Version      int    `yaml:"version"`
		TargetBranch string `yaml:"target_branch"`
	}
	if err := yaml.Unmarshal(content, &probe); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	var cfg Config
	if probe.Version > 0 {
		if err := yaml.Unmarshal(content, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
	} else {
		var legacy legacyIssuePolicy
		if err := yaml.Unmarshal(content, &legacy); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg = normalizeLegacy(legacy)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return cfg, nil
}

func (policy *PRPolicy) UnmarshalYAML(value *yaml.Node) error {
	type prPolicy PRPolicy
	var decoded prPolicy
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*policy = PRPolicy(decoded)
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case "allow_direct_base_branch_prs":
			policy.allowDirectBaseBranchPRsSet = true
		case "reject_all_trivial_multi_issue_prs":
			policy.rejectAllTrivialMultiIssuePRsSet = true
		case "fail_when_commit_listing_reaches_cap":
			policy.failWhenCommitListingReachesCapSet = true
		}
	}
	return nil
}

func MarshalYAML(cfg Config) ([]byte, error) {
	cfg.applyDefaults()
	cfg.SchemaVersion = 0
	return yaml.Marshal(cfg)
}

func normalizeLegacy(legacy legacyIssuePolicy) Config {
	cfg := DefaultConfig()
	cfg.Repository.StagingBranch = firstNonEmpty(legacy.TargetBranch, cfg.Repository.StagingBranch)
	cfg.Repository.WorkBranchPrefix = firstNonEmpty(legacy.WorkBranchPrefix, cfg.Repository.WorkBranchPrefix)
	cfg.IssuePolicy = IssuePolicy{
		PolicyCommentMarker:   legacy.PolicyCommentMarker,
		FormSections:          legacy.FormSections,
		WorkKindLabels:        legacy.WorkKindLabels,
		AgentModeLabels:       legacy.AgentModeLabels,
		PriorityLabels:        legacy.PriorityLabels,
		ControlledLabelGroups: legacy.ControlledLabelGroups,
		ImplementationLabels:  legacy.ImplementationLabels,
		CommentOnlyLabels:     legacy.CommentOnlyLabels,
		SkipLabels:            legacy.SkipLabels,
		RequiredSections:      legacy.RequiredSections,
	}
	return cfg
}

func (cfg *Config) applyDefaults() {
	cfg.SchemaVersion = 1
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Repository.DefaultRemote == "" {
		cfg.Repository.DefaultRemote = "origin"
	}
	if cfg.Repository.BaseBranch == "" {
		cfg.Repository.BaseBranch = "main"
	}
	if cfg.Repository.StagingBranch == "" {
		cfg.Repository.StagingBranch = "agent"
	}
	if cfg.Repository.WorkBranchPrefix == "" {
		cfg.Repository.WorkBranchPrefix = "agent-work/"
	}
	if cfg.PRPolicy.RequiredReferenceKeyword == "" {
		cfg.PRPolicy.RequiredReferenceKeyword = "Refs"
	}
	if len(cfg.PRPolicy.ForbiddenClosingKeywords) == 0 {
		cfg.PRPolicy.ForbiddenClosingKeywords = []string{"Closes", "Fixes", "Resolves"}
	}
	if len(cfg.PRPolicy.NoisyCommitSubjects) == 0 {
		cfg.PRPolicy.NoisyCommitSubjects = defaultNoisyCommitSubjects()
	}
	if !cfg.PRPolicy.AllowDirectBaseBranchPRs && !cfg.PRPolicy.allowDirectBaseBranchPRsSet {
		cfg.PRPolicy.AllowDirectBaseBranchPRs = true
	}
	if !cfg.PRPolicy.FailWhenCommitListingReachesCap && !cfg.PRPolicy.failWhenCommitListingReachesCapSet {
		cfg.PRPolicy.FailWhenCommitListingReachesCap = true
	}
	if !cfg.PRPolicy.RejectAllTrivialMultiIssuePRs && !cfg.PRPolicy.rejectAllTrivialMultiIssuePRsSet {
		cfg.PRPolicy.RejectAllTrivialMultiIssuePRs = true
	}
	if cfg.Labels.Manifest == "" {
		cfg.Labels.Manifest = ".github/labels.yml"
	}
}

func (cfg Config) Validate() error {
	if cfg.Repository.BaseBranch == "" {
		return errors.New("repository.base_branch is required")
	}
	if cfg.Repository.StagingBranch == "" {
		return errors.New("repository.staging_branch is required")
	}
	if cfg.Repository.WorkBranchPrefix == "" || cfg.Repository.WorkBranchPrefix[len(cfg.Repository.WorkBranchPrefix)-1] != '/' {
		return errors.New("repository.work_branch_prefix must end with /")
	}
	for _, branch := range []string{cfg.Repository.BaseBranch, cfg.Repository.StagingBranch, cfg.Repository.WorkBranchPrefix} {
		for _, ch := range branch {
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				return fmt.Errorf("branch value %q must not contain whitespace", branch)
			}
		}
	}
	requiredSections := []string{"work_kind", "agent_mode", "summary"}
	for _, section := range requiredSections {
		if cfg.IssuePolicy.FormSections[section] == "" {
			return fmt.Errorf("issue_policy.form_sections.%s is required", section)
		}
	}
	for mode, sections := range cfg.IssuePolicy.RequiredSections {
		for _, section := range sections {
			if cfg.IssuePolicy.FormSections[section] == "" {
				return fmt.Errorf("issue_policy.required_sections.%s references unknown section %q", mode, section)
			}
		}
	}
	if len(cfg.IssuePolicy.PriorityLabels) > 0 {
		if cfg.IssuePolicy.FormSections["priority"] == "" {
			return errors.New("issue_policy.form_sections.priority is required when priority_labels is set")
		}
		priorityGroup := cfg.IssuePolicy.ControlledLabelGroups["priority"]
		if len(priorityGroup) == 0 {
			return errors.New("issue_policy.controlled_label_groups.priority is required when priority_labels is set")
		}
		groupLabels := map[string]struct{}{}
		for _, label := range priorityGroup {
			if label == "" {
				return errors.New("issue_policy.controlled_label_groups.priority must not contain empty labels")
			}
			groupLabels[label] = struct{}{}
		}
		mappedLabels := map[string]struct{}{}
		for value, label := range cfg.IssuePolicy.PriorityLabels {
			if value == "" || label == "" {
				return errors.New("issue_policy.priority_labels must not contain empty keys or labels")
			}
			if _, ok := groupLabels[label]; !ok {
				return fmt.Errorf("issue_policy.priority_labels.%s references label %q outside controlled_label_groups.priority", value, label)
			}
			if _, duplicate := mappedLabels[label]; duplicate {
				return fmt.Errorf("issue_policy.priority_labels maps multiple values to %q", label)
			}
			mappedLabels[label] = struct{}{}
		}
		for _, label := range priorityGroup {
			if _, ok := mappedLabels[label]; !ok {
				return fmt.Errorf("issue_policy.controlled_label_groups.priority contains unmapped label %q", label)
			}
		}
	}
	return nil
}

func DefaultConfig() Config {
	cfg := Config{
		SchemaVersion: 1,
		Version:       1,
		Setup: SetupConfig{
			BaselineBatonVersion: "v0.4.2", // x-release-please-version
		},
		Repository: RepositoryConfig{
			DefaultRemote:    "origin",
			BaseBranch:       "main",
			StagingBranch:    "agent",
			WorkBranchPrefix: "agent-work/",
		},
		IssuePolicy: IssuePolicy{
			PolicyCommentMarker: "<!-- baton-issue-policy:v1 -->",
			FormSections: map[string]string{
				"work_kind":           "Work kind",
				"agent_mode":          "Agent mode",
				"priority":            "Priority",
				"summary":             "Summary",
				"context_evidence":    "Context / evidence",
				"acceptance_criteria": "Acceptance criteria",
				"non_goals":           "Non-goals / constraints",
				"validation_hints":    "Validation hints",
				"notes":               "Notes",
			},
			WorkKindLabels: map[string]string{
				"Bug":           "bug",
				"Documentation": "documentation",
				"Enhancement":   "enhancement",
				"Question":      "question",
			},
			AgentModeLabels: map[string]string{
				"Ready trivial":    "agent:ready-trivial",
				"Ready bounded":    "agent:ready-bounded",
				"Investigate only": "agent:investigate-only",
				"Needs discussion": "needs:discussion",
			},
			PriorityLabels: map[string]string{
				"P0": "priority:p0",
				"P1": "priority:p1",
				"P2": "priority:p2",
				"P3": "priority:p3",
			},
			ControlledLabelGroups: map[string][]string{
				"work_kind":    {"bug", "documentation", "enhancement", "question"},
				"agent_mode":   {"agent:ready-trivial", "agent:ready-bounded", "agent:investigate-only", "needs:discussion"},
				"priority":     {"priority:p0", "priority:p1", "priority:p2", "priority:p3"},
				"quality_gate": {"needs-info"},
			},
			ImplementationLabels: []string{"agent:ready-trivial", "agent:ready-bounded"},
			CommentOnlyLabels:    []string{"agent:investigate-only"},
			SkipLabels:           []string{"needs-info", "needs:discussion", "needs:review"},
			RequiredSections: map[string][]string{
				"ready-trivial": {"summary", "context_evidence", "acceptance_criteria"},
				"ready-bounded": {"summary", "context_evidence", "acceptance_criteria"},
			},
		},
		PRPolicy: PRPolicy{
			RequiredReferenceKeyword:        "Refs",
			ForbiddenClosingKeywords:        []string{"Closes", "Fixes", "Resolves"},
			AllowDirectBaseBranchPRs:        true,
			RejectAllTrivialMultiIssuePRs:   true,
			NoisyCommitSubjects:             defaultNoisyCommitSubjects(),
			FailWhenCommitListingReachesCap: true,
		},
		Labels: LabelsConfig{Manifest: ".github/labels.yml"},
		Automation: AutomationConfig{
			PreferPRFollowupBeforeIssueIntake: true,
			AllowMerge:                        false,
		},
	}
	return cfg
}

func defaultNoisyCommitSubjects() []string {
	return []string{
		"address comments",
		"address review",
		"changes",
		"fix",
		"fix lint",
		"lint",
		"misc",
		"oops",
		"try again",
		"update",
		"wip",
		"work in progress",
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
