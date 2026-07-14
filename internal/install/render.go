package install

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"gopkg.in/yaml.v3"
)

type ManagedFile struct {
	Path      string
	Content   []byte
	Ownership string
}

func RenderManagedFiles(policy config.RepositoryPolicy, options Options) ([]ManagedFile, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("render managed files: %w", err)
	}
	options = options.withDefaults()
	configContent, err := config.MarshalYAML(policy)
	if err != nil {
		return nil, err
	}
	issueForm, err := renderIssueForm(policy)
	if err != nil {
		return nil, err
	}
	labelManifest, err := renderLabelManifest(policy)
	if err != nil {
		return nil, err
	}
	issueWorkflow, err := renderedEmbeddedTemplate(".github/workflows/issue-policy.yml", policy, options)
	if err != nil {
		return nil, err
	}
	prWorkflow, err := renderedEmbeddedTemplate(".github/workflows/pr-policy.yml", policy, options)
	if err != nil {
		return nil, err
	}
	transitionWorkflow, err := renderedTransitionWorkflow(policy, options)
	if err != nil {
		return nil, err
	}
	files := []ManagedFile{
		{Path: ".github/baton.yml", Content: append([]byte("# Managed by Baton. Edit in this repository when your policy changes.\n"), configContent...), Ownership: "repository-policy"},
		{Path: policy.Labels.Manifest, Content: labelManifest, Ownership: "repository-policy"},
		{Path: ".github/ISSUE_WORKFLOW.md", Content: renderWorkflowGuidance(policy), Ownership: "repository-policy"},
		{Path: ".github/ISSUE_TEMPLATE/agent-work.yml", Content: issueForm, Ownership: "repository-policy"},
		{Path: ".github/workflows/issue-policy.yml", Content: issueWorkflow, Ownership: "repository-policy"},
		{Path: ".github/workflows/pr-policy.yml", Content: prWorkflow, Ownership: "repository-policy"},
		{Path: ".github/workflows/work-item-transition.yml", Content: transitionWorkflow, Ownership: "repository-policy"},
	}
	if err := validateManagedFiles(files); err != nil {
		return nil, err
	}
	return files, nil
}

var exactGoInstallPattern = regexp.MustCompile(`^[A-Za-z0-9._~/-]+@v\d+\.\d+\.\d+$`)

func renderedTransitionWorkflow(policy config.RepositoryPolicy, options Options) ([]byte, error) {
	if !exactGoInstallPattern.MatchString(options.GoInstall) {
		return nil, fmt.Errorf("work-item transition workflow requires --go-install module@vX.Y.Z")
	}
	trusted := options
	trusted.InstallCommand = "mkdir -p \"$RUNNER_TEMP/baton-bin\"\nGOBIN=\"$RUNNER_TEMP/baton-bin\" go install " + options.GoInstall + "\necho \"$RUNNER_TEMP/baton-bin\" >> \"$GITHUB_PATH\""
	return renderedEmbeddedTemplate(".github/workflows/work-item-transition.yml", policy, trusted)
}

func renderedEmbeddedTemplate(path string, policy config.RepositoryPolicy, options Options) ([]byte, error) {
	name := "templates/" + path
	content, err := templatesFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	rendered := strings.ReplaceAll(string(content), defaultGoInstall, options.GoInstall)
	rendered = strings.ReplaceAll(rendered, installCommandPlaceholder, indentInstallCommand(options.InstallCommand))
	if path == ".github/workflows/pr-policy.yml" || path == ".github/workflows/work-item-transition.yml" {
		rendered, err = replaceWorkflowBranches(path, rendered, policy)
		if err != nil {
			return nil, err
		}
	}
	return []byte(rendered), nil
}

const workflowBranchPlaceholder = "      - agent\n      - main"

func replaceWorkflowBranches(path, rendered string, policy config.RepositoryPolicy) (string, error) {
	if !strings.Contains(rendered, workflowBranchPlaceholder) {
		return "", fmt.Errorf("render workflow %s: expected agent/main branch placeholder is missing", path)
	}
	replacement := "      - " + strconv.Quote(policy.Repository.StagingBranch) + "\n      - " + strconv.Quote(policy.Repository.BaseBranch)
	return strings.Replace(rendered, workflowBranchPlaceholder, replacement, 1), nil
}

type issueFormDocument struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Title       string           `yaml:"title"`
	Body        []issueFormField `yaml:"body"`
}

type issueFormField struct {
	Type        string               `yaml:"type"`
	ID          string               `yaml:"id"`
	Attributes  issueFormAttributes  `yaml:"attributes"`
	Validations issueFormValidations `yaml:"validations"`
}

type issueFormAttributes struct {
	Label       string   `yaml:"label"`
	Description string   `yaml:"description,omitempty"`
	Options     []string `yaml:"options,omitempty"`
	Default     *int     `yaml:"default,omitempty"`
	Placeholder string   `yaml:"placeholder,omitempty"`
}

type issueFormValidations struct {
	Required bool `yaml:"required"`
}

func renderIssueForm(policy config.RepositoryPolicy) ([]byte, error) {
	fields := []issueFormField{}
	requiredSections := requiredIssueFormSections(policy)
	for _, id := range orderedSectionIDs(policy.IssuePolicy.FormSections) {
		field := issueFormField{
			Type: "textarea", ID: id,
			Attributes:  issueFormAttributes{Label: policy.IssuePolicy.FormSections[id], Description: sectionDescription(id)},
			Validations: issueFormValidations{Required: requiredSections[id]},
		}
		switch id {
		case "work_kind":
			field.Type = "dropdown"
			field.Attributes.Options = orderedOptions(policy.IssuePolicy.WorkKindLabels, []string{"Bug", "Documentation", "Enhancement", "Question"})
		case "agent_mode":
			field.Type = "dropdown"
			field.Attributes.Options = orderedOptions(policy.IssuePolicy.AgentModeLabels, []string{"Ready trivial", "Ready bounded", "Investigate only", "Needs discussion"})
		case "priority":
			if len(policy.IssuePolicy.PriorityLabels) == 0 {
				continue
			}
			field.Type = "dropdown"
			field.Attributes.Options = orderedOptions(policy.IssuePolicy.PriorityLabels, []string{"P0", "P1", "P2", "P3"})
			defaultIndex := 0
			for index, option := range field.Attributes.Options {
				if option == "P2" {
					defaultIndex = index
				}
			}
			field.Attributes.Default = &defaultIndex
		case "acceptance_criteria":
			field.Attributes.Placeholder = "- [ ] ..."
		}
		fields = append(fields, field)
	}
	document := issueFormDocument{Name: "Agent-readable work item", Description: "Structured work item for human maintainers and LLM agents.", Title: "[Work]: ", Body: fields}
	content, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Managed by Baton. Edit in this repository when your issue workflow changes.\n"), content...), nil
}

func requiredIssueFormSections(policy config.RepositoryPolicy) map[string]bool {
	required := map[string]bool{"work_kind": true, "agent_mode": true}
	for _, sections := range policy.IssuePolicy.RequiredSections {
		for _, section := range sections {
			required[section] = true
		}
	}
	if len(policy.IssuePolicy.PriorityLabels) > 0 {
		required["priority"] = true
	}
	return required
}

func orderedSectionIDs(sections map[string]string) []string {
	preferred := []string{"work_kind", "agent_mode", "priority", "summary", "context_evidence", "acceptance_criteria", "non_goals", "validation_hints", "notes"}
	result, seen := []string{}, map[string]struct{}{}
	for _, id := range preferred {
		if _, exists := sections[id]; exists {
			result = append(result, id)
			seen[id] = struct{}{}
		}
	}
	extra := []string{}
	for id := range sections {
		if _, exists := seen[id]; !exists {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	return append(result, extra...)
}

func orderedOptions(values map[string]string, preferred []string) []string {
	result, seen := []string{}, map[string]struct{}{}
	for _, option := range preferred {
		if _, exists := values[option]; exists {
			result = append(result, option)
			seen[option] = struct{}{}
		}
	}
	extra := []string{}
	for option := range values {
		if _, exists := seen[option]; !exists {
			extra = append(extra, option)
		}
	}
	sort.Strings(extra)
	return append(result, extra...)
}

func sectionDescription(id string) string {
	return map[string]string{
		"work_kind": "Pick the closest routing kind.", "agent_mode": "Controls what an agent may do with this issue.",
		"priority": "Queue priority within the same Baton safety tier.", "summary": "State the requested outcome.",
		"context_evidence": "Include evidence and exact observations.", "acceptance_criteria": "List concrete completion criteria.",
		"non_goals": "Optional constraints and compatibility limits.", "validation_hints": "Optional relevant checks.", "notes": "Optional additional context.",
	}[id]
}

type labelManifestDocument struct {
	Labels []labelManifestEntry `yaml:"labels"`
}

type labelManifestEntry struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description"`
}

func renderLabelManifest(policy config.RepositoryPolicy) ([]byte, error) {
	names := map[string]struct{}{}
	for _, labels := range policy.IssuePolicy.ControlledLabelGroups {
		for _, label := range labels {
			names[label] = struct{}{}
		}
	}
	for _, label := range policy.IssuePolicy.SkipLabels {
		names[label] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	entries := make([]labelManifestEntry, 0, len(ordered))
	for _, name := range ordered {
		color, description := labelPresentation(name)
		entries = append(entries, labelManifestEntry{Name: name, Color: color, Description: description})
	}
	content, err := yaml.Marshal(labelManifestDocument{Labels: entries})
	if err != nil {
		return nil, err
	}
	prefix := "# Managed by Baton. Edit repository policy, then run baton sync-labels --dry-run.\n"
	return append([]byte(prefix), content...), nil
}

func labelPresentation(name string) (string, string) {
	known := map[string][2]string{
		"agent:ready-trivial":    {"0E8A16", "Agent may make a narrow, obvious fix."},
		"agent:ready-bounded":    {"1D76DB", "Agent may implement within explicit scope and criteria."},
		"agent:investigate-only": {"5319E7", "Agent may inspect and comment, but not change behavior."},
		"needs-info":             {"B60205", "Issue is missing information required by repository policy."},
		"needs:discussion":       {"FBCA04", "Human decision needed before implementation."},
		"needs:review":           {"C5DEF5", "Agent work is ready for human review."},
		"priority:p0":            {"B60205", "Highest urgency."}, "priority:p1": {"D93F0B", "High priority."},
		"priority:p2": {"FBCA04", "Normal priority."}, "priority:p3": {"C2E0C6", "Lower priority."},
		"bug": {"D73A4A", "Confirmed defect."}, "documentation": {"0075CA", "Documentation change."},
		"enhancement": {"A2EEEF", "Feature or product improvement."}, "question": {"D876E3", "Question or clarification."},
	}
	if presentation, exists := known[name]; exists {
		return presentation[0], presentation[1]
	}
	return "BFDADC", "Repository policy label."
}

func renderWorkflowGuidance(policy config.RepositoryPolicy) []byte {
	qualityGate := policy.IssuePolicy.ControlledLabelGroups["quality_gate"][0]
	return []byte(fmt.Sprintf(`# GitHub Issue Workflow

Managed by Baton. Edit repository policy when this workflow changes.

GitHub Issues are the repository work queue. Implementation labels are %s.
Comment-only labels are %s. Baton skips issues carrying %s.
Merged work PRs add %s while the issue awaits promotion review.

Ready issues use the configured form headings and required sections. Incomplete
ready issues receive %s and one updatable policy comment.

Work PRs target %s from branches prefixed with %s and reference issues with
%s #123. Promotion PRs target %s from %s. Baton derives the included work PRs
from the promotion revisions and requires closing keywords for every referenced
work issue. Manual-only promotions need no artificial issue reference; incomplete
promotion evidence fails policy instead of guessing.
`, markdownLabels(policy.IssuePolicy.ImplementationLabels), markdownLabels(policy.IssuePolicy.CommentOnlyLabels), markdownLabels(policy.IssuePolicy.SkipLabels), markdownLabels([]string{policy.IssuePolicy.AwaitingReviewLabel}), qualityGate, policy.Repository.StagingBranch, policy.Repository.WorkBranchPrefix, policy.PRPolicy.RequiredReferenceKeyword, policy.Repository.BaseBranch, policy.Repository.StagingBranch))
}

func markdownLabels(labels []string) string {
	quoted := make([]string, len(labels))
	for index, label := range labels {
		quoted[index] = "`" + label + "`"
	}
	return strings.Join(quoted, ", ")
}
