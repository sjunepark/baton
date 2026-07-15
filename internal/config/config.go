package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrConfigNotFound = errors.New("baton config not found")

var policyMarkerPattern = regexp.MustCompile(`^<!-- [A-Za-z0-9][A-Za-z0-9:_-]*:v[1-9][0-9]* -->$`)

// RepositoryPolicy is the compiled, validated runtime policy. YAML wire
// compatibility is intentionally kept out of this model.
type RepositoryPolicy struct {
	SchemaVersion int              `json:"schemaVersion" yaml:"-"`
	Version       int              `json:"version" yaml:"version"`
	Setup         SetupConfig      `json:"setup,omitempty" yaml:"setup,omitempty"`
	Repository    RepositoryConfig `json:"repository" yaml:"repository"`
	IssuePolicy   IssuePolicy      `json:"issuePolicy" yaml:"issue_policy"`
	PRPolicy      PRPolicy         `json:"prPolicy" yaml:"pr_policy"`
	Labels        LabelsConfig     `json:"labels" yaml:"labels"`
	Delivery      *DeliveryConfig  `json:"delivery,omitempty" yaml:"delivery,omitempty"`
}

// Config remains an alias during the internal migration to RepositoryPolicy.
type Config = RepositoryPolicy

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
	AwaitingReviewLabel   string              `json:"awaitingReviewLabel" yaml:"awaiting_review_label"`
	RequiredSections      map[string][]string `json:"requiredSections" yaml:"required_sections"`
}

type PRPolicy struct {
	RequiredReferenceKeyword        string   `json:"requiredReferenceKeyword" yaml:"required_reference_keyword"`
	ForbiddenClosingKeywords        []string `json:"forbiddenClosingKeywords" yaml:"forbidden_closing_keywords"`
	NoisyCommitSubjects             []string `json:"noisyCommitSubjects" yaml:"noisy_commit_subjects"`
	FailWhenCommitListingReachesCap bool     `json:"failWhenCommitListingReachesCap" yaml:"fail_when_commit_listing_reaches_cap"`
}

type LabelsConfig struct {
	Manifest string `json:"manifest" yaml:"manifest"`
}

type DeliveryAuthority string

const (
	DeliveryAuthorityShadow DeliveryAuthority = "shadow"
	DeliveryAuthoritySealed DeliveryAuthority = "sealed"
)

// DeliveryConfig pins the repository-owned GitHub resources used by the
// delivery ledger. Shadow permits reviewed bootstrap/recording; sealed enables
// policy, transition, and recommendation authority.
type DeliveryConfig struct {
	Authority  DeliveryAuthority  `json:"authority" yaml:"authority"`
	Host       string             `json:"host" yaml:"host"`
	Repository DeliveryRepository `json:"repository" yaml:"repository"`
	Issue      DeliveryResource   `json:"issue" yaml:"issue"`
	Checkpoint DeliveryComment    `json:"checkpoint" yaml:"checkpoint"`
}

type DeliveryRepository struct {
	FullName string `json:"fullName" yaml:"full_name"`
	NodeID   string `json:"nodeId" yaml:"node_id"`
}

type DeliveryResource struct {
	Number int    `json:"number" yaml:"number"`
	NodeID string `json:"nodeId" yaml:"node_id"`
}

type DeliveryComment struct {
	DatabaseID int64  `json:"databaseId" yaml:"database_id"`
	NodeID     string `json:"nodeId" yaml:"node_id"`
}

type currentWireConfig struct {
	Version     int                    `yaml:"version"`
	Setup       currentWireSetup       `yaml:"setup,omitempty"`
	Repository  currentWireRepository  `yaml:"repository"`
	IssuePolicy currentWireIssuePolicy `yaml:"issue_policy"`
	PRPolicy    currentWirePR          `yaml:"pr_policy"`
	Labels      currentWireLabels      `yaml:"labels"`
	Delivery    *DeliveryConfig        `yaml:"delivery,omitempty"`
	Automation  *legacyAutomationWire  `yaml:"automation,omitempty"`
}

type currentWireSetup SetupConfig
type currentWireRepository RepositoryConfig
type currentWireIssuePolicy IssuePolicy
type currentWireLabels LabelsConfig

type currentWirePR struct {
	RequiredReferenceKeyword string   `yaml:"required_reference_keyword"`
	ForbiddenClosingKeywords []string `yaml:"forbidden_closing_keywords"`
	// Retired options remain decode-only for the v0.6 adopter window and are
	// removed with the next config-schema major.
	AllowDirectBaseBranchPRs        *bool    `yaml:"allow_direct_base_branch_prs,omitempty"`
	RejectAllTrivialMultiIssuePRs   *bool    `yaml:"reject_all_trivial_multi_issue_prs,omitempty"`
	NoisyCommitSubjects             []string `yaml:"noisy_commit_subjects"`
	FailWhenCommitListingReachesCap *bool    `yaml:"fail_when_commit_listing_reaches_cap"`
}

// legacyAutomationWire is accepted only so released v1 files can migrate.
// It compiles to no runtime behavior and is never emitted again.
type legacyAutomationWire struct {
	PreferPRFollowupBeforeIssueIntake bool `yaml:"prefer_pr_followup_before_issue_intake"`
	AllowMerge                        bool `yaml:"allow_merge"`
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
	AwaitingReviewLabel   string              `yaml:"awaiting_review_label"`
	RequiredSections      map[string][]string `yaml:"required_sections"`
}

func LoadForRepo(root string) (Config, error) {
	cfg, _, err := LoadForRepoWithPath(root)
	return cfg, err
}

// LoadForRepoWithPath loads repository policy and reports the file that
// supplied it. Callers that bind policy to a repository context must retain
// this path rather than rediscovering configuration independently.
func LoadForRepoWithPath(root string) (Config, string, error) {
	for _, path := range []string{
		filepath.Join(root, ".github", "baton.yml"),
		filepath.Join(root, ".github", "agent-issue-policy.yml"),
	} {
		cfg, err := Load(path)
		if err == nil {
			return cfg, path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, "", err
		}
	}
	return Config{}, "", ErrConfigNotFound
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	var cfg RepositoryPolicy
	if hasTopLevelKey(&document, "version") {
		var wire currentWireConfig
		if err := decodeStrict(content, &wire); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
		if wire.Version != 1 {
			return Config{}, fmt.Errorf("validate %s: unsupported config version %d", path, wire.Version)
		}
		cfg = compileCurrent(wire)
	} else {
		var legacy legacyIssuePolicy
		if err := decodeStrict(content, &legacy); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg = compileLegacy(legacy)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return cfg, nil
}

func MarshalYAML(cfg Config) ([]byte, error) {
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(wireFromPolicy(cfg))
}

func compileLegacy(legacy legacyIssuePolicy) RepositoryPolicy {
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
		AwaitingReviewLabel:   legacy.AwaitingReviewLabel,
		RequiredSections:      legacy.RequiredSections,
	}
	return cfg
}

func compileCurrent(wire currentWireConfig) RepositoryPolicy {
	return RepositoryPolicy{
		SchemaVersion: 1,
		Version:       wire.Version,
		Setup:         SetupConfig(wire.Setup),
		Repository:    RepositoryConfig(wire.Repository),
		IssuePolicy:   IssuePolicy(wire.IssuePolicy),
		PRPolicy: PRPolicy{
			RequiredReferenceKeyword:        wire.PRPolicy.RequiredReferenceKeyword,
			ForbiddenClosingKeywords:        wire.PRPolicy.ForbiddenClosingKeywords,
			NoisyCommitSubjects:             wire.PRPolicy.NoisyCommitSubjects,
			FailWhenCommitListingReachesCap: boolValue(wire.PRPolicy.FailWhenCommitListingReachesCap, true),
		},
		Labels:   LabelsConfig(wire.Labels),
		Delivery: wire.Delivery,
	}
}

func wireFromPolicy(cfg RepositoryPolicy) currentWireConfig {
	return currentWireConfig{
		Version: cfg.Version, Setup: currentWireSetup(cfg.Setup), Repository: currentWireRepository(cfg.Repository),
		IssuePolicy: currentWireIssuePolicy(cfg.IssuePolicy), Labels: currentWireLabels(cfg.Labels), Delivery: cfg.Delivery,
		PRPolicy: currentWirePR{
			RequiredReferenceKeyword:        cfg.PRPolicy.RequiredReferenceKeyword,
			ForbiddenClosingKeywords:        cfg.PRPolicy.ForbiddenClosingKeywords,
			NoisyCommitSubjects:             cfg.PRPolicy.NoisyCommitSubjects,
			FailWhenCommitListingReachesCap: boolPointer(cfg.PRPolicy.FailWhenCommitListingReachesCap),
		},
	}
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
	if cfg.Labels.Manifest == "" {
		cfg.Labels.Manifest = ".github/labels.yml"
	}
	if cfg.IssuePolicy.AwaitingReviewLabel == "" {
		cfg.IssuePolicy.AwaitingReviewLabel = "needs:review"
		if !containsFold(cfg.IssuePolicy.SkipLabels, cfg.IssuePolicy.AwaitingReviewLabel) {
			cfg.IssuePolicy.SkipLabels = append(cfg.IssuePolicy.SkipLabels, cfg.IssuePolicy.AwaitingReviewLabel)
		}
	}
}

func (cfg Config) Validate() error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if strings.TrimSpace(cfg.Repository.DefaultRemote) == "" {
		return errors.New("repository.default_remote is required")
	}
	if cfg.Repository.BaseBranch == "" {
		return errors.New("repository.base_branch is required")
	}
	if cfg.Repository.StagingBranch == "" {
		return errors.New("repository.staging_branch is required")
	}
	if cfg.Repository.BaseBranch == cfg.Repository.StagingBranch {
		return errors.New("repository.base_branch and repository.staging_branch must differ")
	}
	if cfg.Repository.WorkBranchPrefix == "" || cfg.Repository.WorkBranchPrefix[len(cfg.Repository.WorkBranchPrefix)-1] != '/' {
		return errors.New("repository.work_branch_prefix must end with /")
	}
	if err := validateRemoteName(cfg.Repository.DefaultRemote); err != nil {
		return fmt.Errorf("repository.default_remote: %w", err)
	}
	branches := []struct {
		field  string
		branch string
	}{{"base_branch", cfg.Repository.BaseBranch}, {"staging_branch", cfg.Repository.StagingBranch}}
	for _, candidate := range branches {
		if err := validateBranchName(candidate.branch); err != nil {
			return fmt.Errorf("repository.%s: %w", candidate.field, err)
		}
	}
	if err := validateBranchName(cfg.Repository.WorkBranchPrefix + "work"); err != nil {
		return fmt.Errorf("repository.work_branch_prefix: %w", err)
	}
	for _, candidate := range branches {
		if strings.HasPrefix(candidate.branch, cfg.Repository.WorkBranchPrefix) {
			return fmt.Errorf("repository.%s must not fall under repository.work_branch_prefix %q", candidate.field, cfg.Repository.WorkBranchPrefix)
		}
	}
	if !policyMarkerPattern.MatchString(cfg.IssuePolicy.PolicyCommentMarker) {
		return errors.New("issue_policy.policy_comment_marker must be a stable versioned HTML comment such as <!-- baton-issue-policy:v1 -->")
	}
	requiredSections := []string{"work_kind", "agent_mode", "summary"}
	for _, section := range requiredSections {
		if cfg.IssuePolicy.FormSections[section] == "" {
			return fmt.Errorf("issue_policy.form_sections.%s is required", section)
		}
	}
	if err := validateFormHeadings(cfg.IssuePolicy.FormSections); err != nil {
		return err
	}
	modeSlugs := map[string]struct{}{}
	for mode := range cfg.IssuePolicy.AgentModeLabels {
		modeSlugs[normalizeSlug(mode)] = struct{}{}
	}
	for mode, sections := range cfg.IssuePolicy.RequiredSections {
		if _, exists := modeSlugs[mode]; !exists {
			return fmt.Errorf("issue_policy.required_sections.%s does not match an agent_mode_labels option", mode)
		}
		for _, section := range sections {
			if cfg.IssuePolicy.FormSections[section] == "" {
				return fmt.Errorf("issue_policy.required_sections.%s references unknown section %q", mode, section)
			}
		}
	}
	if err := validateControlledLabels(cfg.IssuePolicy); err != nil {
		return err
	}
	if err := validateMappedGroup("work_kind_labels", cfg.IssuePolicy.WorkKindLabels, cfg.IssuePolicy.ControlledLabelGroups["work_kind"]); err != nil {
		return err
	}
	if err := validateMappedGroup("agent_mode_labels", cfg.IssuePolicy.AgentModeLabels, cfg.IssuePolicy.ControlledLabelGroups["agent_mode"]); err != nil {
		return err
	}
	if len(cfg.IssuePolicy.ControlledLabelGroups["quality_gate"]) != 1 {
		return errors.New("issue_policy.controlled_label_groups.quality_gate must contain exactly one label")
	}
	agentLabels := stringSet(cfg.IssuePolicy.AgentModeLabels)
	if err := validatePolicyLabelSubset("implementation_labels", cfg.IssuePolicy.ImplementationLabels, agentLabels); err != nil {
		return err
	}
	if err := validatePolicyLabelSubset("comment_only_labels", cfg.IssuePolicy.CommentOnlyLabels, agentLabels); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.IssuePolicy.AwaitingReviewLabel) == "" || !containsFold(cfg.IssuePolicy.SkipLabels, cfg.IssuePolicy.AwaitingReviewLabel) {
		return errors.New("issue_policy.awaiting_review_label must be non-empty and included in skip_labels")
	}
	for group, controlled := range cfg.IssuePolicy.ControlledLabelGroups {
		if containsFold(controlled, cfg.IssuePolicy.AwaitingReviewLabel) {
			return fmt.Errorf("issue_policy.awaiting_review_label must remain workflow state, not a controlled %s label", group)
		}
	}
	implementation := normalizedSet(cfg.IssuePolicy.ImplementationLabels)
	for _, label := range cfg.IssuePolicy.CommentOnlyLabels {
		if _, duplicate := implementation[strings.ToLower(label)]; duplicate {
			return fmt.Errorf("issue_policy label %q cannot be both implementation and comment-only", label)
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
	if strings.TrimSpace(cfg.Labels.Manifest) == "" {
		return errors.New("labels.manifest is required")
	}
	cleanManifest := filepath.Clean(cfg.Labels.Manifest)
	if filepath.IsAbs(cleanManifest) || cleanManifest == ".." || strings.HasPrefix(cleanManifest, ".."+string(filepath.Separator)) {
		return errors.New("labels.manifest must be a repository-relative path")
	}
	if cfg.Delivery != nil {
		if err := validateDeliveryConfig(*cfg.Delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateDeliveryConfig(delivery DeliveryConfig) error {
	if delivery.Authority != DeliveryAuthorityShadow && delivery.Authority != DeliveryAuthoritySealed {
		return errors.New("delivery.authority must be shadow or sealed")
	}
	if strings.TrimSpace(delivery.Host) == "" {
		return errors.New("delivery.host is required")
	}
	if strings.TrimSpace(delivery.Repository.FullName) == "" || strings.TrimSpace(delivery.Repository.NodeID) == "" {
		return errors.New("delivery.repository full_name and node_id are required")
	}
	parts := strings.Split(delivery.Repository.FullName, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return errors.New("delivery.repository.full_name must be owner/name")
	}
	if delivery.Issue.Number <= 0 || strings.TrimSpace(delivery.Issue.NodeID) == "" {
		return errors.New("delivery.issue number and node_id are required")
	}
	if delivery.Checkpoint.DatabaseID <= 0 || strings.TrimSpace(delivery.Checkpoint.NodeID) == "" {
		return errors.New("delivery.checkpoint database_id and node_id are required")
	}
	return nil
}

func validateRemoteName(value string) error {
	if value == "" || strings.HasPrefix(value, "-") {
		return errors.New("must be a non-option git remote name")
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-/", character)) {
			return fmt.Errorf("contains invalid character %q", character)
		}
	}
	if strings.Contains(value, "..") || strings.Contains(value, "//") || strings.HasSuffix(value, "/") {
		return errors.New("must be a normalized git remote name")
	}
	return nil
}

func validateBranchName(value string) error {
	if value == "" || value == "@" || value == "HEAD" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") {
		return errors.New("is not a valid git branch name")
	}
	if strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.Contains(value, "//") {
		return errors.New("is not a valid git branch name")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f || strings.ContainsRune(" ~^:?*[\\", character) {
			return fmt.Errorf("contains invalid git ref character %q", character)
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return errors.New("is not a valid git branch name")
		}
	}
	return nil
}

func DefaultConfig() Config {
	cfg := Config{
		SchemaVersion: 1,
		Version:       1,
		Setup: SetupConfig{
			BaselineBatonVersion: "v0.5.1", // x-release-please-version
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
			AwaitingReviewLabel:  "needs:review",
			RequiredSections: map[string][]string{
				"ready-trivial": {"summary", "context_evidence", "acceptance_criteria"},
				"ready-bounded": {"summary", "context_evidence", "acceptance_criteria"},
			},
		},
		PRPolicy: PRPolicy{
			RequiredReferenceKeyword:        "Refs",
			ForbiddenClosingKeywords:        []string{"Closes", "Fixes", "Resolves"},
			NoisyCommitSubjects:             defaultNoisyCommitSubjects(),
			FailWhenCommitListingReachesCap: true,
		},
		Labels: LabelsConfig{Manifest: ".github/labels.yml"},
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

func decodeStrict(content []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("config must contain exactly one YAML document")
		}
		return err
	}
	return nil
}

func hasTopLevelKey(document *yaml.Node, key string) bool {
	if document == nil || len(document.Content) == 0 {
		return false
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value == key {
			return true
		}
	}
	return false
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func boolPointer(value bool) *bool {
	return &value
}

func validateFormHeadings(sections map[string]string) error {
	seen := map[string]string{}
	for id, heading := range sections {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(heading) == "" {
			return errors.New("issue_policy.form_sections must not contain empty IDs or headings")
		}
		key := strings.ToLower(strings.TrimSpace(heading))
		if prior, duplicate := seen[key]; duplicate {
			return fmt.Errorf("issue_policy.form_sections.%s duplicates heading used by %s", id, prior)
		}
		seen[key] = id
	}
	return nil
}

func validateControlledLabels(policy IssuePolicy) error {
	seen := map[string]string{}
	for group, labels := range policy.ControlledLabelGroups {
		if strings.TrimSpace(group) == "" || len(labels) == 0 {
			return errors.New("issue_policy.controlled_label_groups must not contain empty groups")
		}
		for _, label := range labels {
			key := strings.ToLower(strings.TrimSpace(label))
			if key == "" {
				return fmt.Errorf("issue_policy.controlled_label_groups.%s must not contain empty labels", group)
			}
			if prior, duplicate := seen[key]; duplicate {
				return fmt.Errorf("controlled label %q appears in both %s and %s", label, prior, group)
			}
			seen[key] = group
		}
	}
	return nil
}

func validateMappedGroup(name string, mappings map[string]string, controlled []string) error {
	if len(mappings) == 0 || len(controlled) == 0 {
		return fmt.Errorf("issue_policy.%s and its controlled label group are required", name)
	}
	group := normalizedSet(controlled)
	mapped := map[string]string{}
	for option, label := range mappings {
		if strings.TrimSpace(option) == "" || strings.TrimSpace(label) == "" {
			return fmt.Errorf("issue_policy.%s must not contain empty options or labels", name)
		}
		key := strings.ToLower(label)
		if prior, duplicate := mapped[key]; duplicate {
			return fmt.Errorf("issue_policy.%s maps both %q and %q to %q", name, prior, option, label)
		}
		if _, exists := group[key]; !exists {
			return fmt.Errorf("issue_policy.%s.%s references label %q outside its controlled group", name, option, label)
		}
		mapped[key] = option
	}
	for _, label := range controlled {
		if _, exists := mapped[strings.ToLower(label)]; !exists {
			return fmt.Errorf("controlled label %q has no mapping in issue_policy.%s", label, name)
		}
	}
	return nil
}

func validatePolicyLabelSubset(name string, labels []string, allowed map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, label := range labels {
		key := strings.ToLower(strings.TrimSpace(label))
		if key == "" {
			return fmt.Errorf("issue_policy.%s must not contain empty labels", name)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("issue_policy.%s contains duplicate label %q", name, label)
		}
		if _, mapped := allowed[key]; !mapped {
			return fmt.Errorf("issue_policy.%s contains unmapped agent-mode label %q", name, label)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func stringSet(mappings map[string]string) map[string]struct{} {
	result := make(map[string]struct{}, len(mappings))
	for _, label := range mappings {
		result[strings.ToLower(label)] = struct{}{}
	}
	return result
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = struct{}{}
	}
	return result
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastDash := false
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			result.WriteRune(character)
			lastDash = false
		} else if !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
