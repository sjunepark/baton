package install

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	policyengine "github.com/sjunepark/baton/internal/policy"
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
	deliveryWorkflow, err := renderedDeliveryWorkflow(policy, options)
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
		{Path: ".github/workflows/delivery-recorder.yml", Content: deliveryWorkflow, Ownership: "repository-policy"},
	}
	if err := validateManagedFiles(files); err != nil {
		return nil, err
	}
	return files, nil
}

var exactGoInstallPattern = regexp.MustCompile(`^[A-Za-z0-9._~/-]+@v\d+\.\d+\.\d+$`)

func renderedTransitionWorkflow(policy config.RepositoryPolicy, options Options) ([]byte, error) {
	trusted, err := trustedWorkflowOptions(options, "work-item transition")
	if err != nil {
		return nil, err
	}
	return renderedEmbeddedTemplate(".github/workflows/work-item-transition.yml", policy, trusted)
}

func renderedDeliveryWorkflow(policy config.RepositoryPolicy, options Options) ([]byte, error) {
	trusted, err := trustedWorkflowOptions(options, "delivery recorder")
	if err != nil {
		return nil, err
	}
	return renderedEmbeddedTemplate(".github/workflows/delivery-recorder.yml", policy, trusted)
}

func trustedWorkflowOptions(options Options, workflow string) (Options, error) {
	if !exactGoInstallPattern.MatchString(options.GoInstall) {
		return Options{}, fmt.Errorf("%s workflow requires --go-install module@vX.Y.Z", workflow)
	}
	trusted := options
	trusted.InstallCommand = "mkdir -p \"$RUNNER_TEMP/baton-bin\"\nGOBIN=\"$RUNNER_TEMP/baton-bin\" go install " + options.GoInstall + "\necho \"$RUNNER_TEMP/baton-bin\" >> \"$GITHUB_PATH\""
	return trusted, nil
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
		rendered, err = replaceWorkflowOwnershipPrefilter(path, rendered, policy)
		if err != nil {
			return nil, err
		}
	}
	if path == ".github/workflows/delivery-recorder.yml" {
		rendered, err = replaceDeliveryRecorderPrefilter(path, rendered, policy)
		if err != nil {
			return nil, err
		}
	}
	return []byte(rendered), nil
}

const workflowOwnershipPrefilterPlaceholder = "__BATON_PR_OWNERSHIP_PREFILTER__"
const deliveryRecorderPrefilterPlaceholder = "__BATON_WORK_PR_OWNERSHIP_PREFILTER__"
const deliveryRecorderBaseBranchPlaceholder = "__BATON_BASE_BRANCH__"

func quoteWorkflowExpression(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func sameRepositoryExpression() string {
	// Missing repository identities must reach the authoritative CLI classifier,
	// which reports an indeterminate flow instead of silently skipping policy.
	return "(!github.event.pull_request.head.repo.full_name || !github.event.pull_request.base.repo.full_name || github.event.pull_request.head.repo.full_name == github.event.pull_request.base.repo.full_name)"
}

func replaceWorkflowOwnershipPrefilter(path, rendered string, policy config.RepositoryPolicy) (string, error) {
	if !strings.Contains(rendered, workflowOwnershipPrefilterPlaceholder) {
		return "", fmt.Errorf("render workflow %s: expected ownership prefilter placeholder is missing", path)
	}
	prefix := quoteWorkflowExpression(policy.Repository.WorkBranchPrefix)
	staging := quoteWorkflowExpression(policy.Repository.StagingBranch)
	base := quoteWorkflowExpression(policy.Repository.BaseBranch)
	prefilter := "${{ " + sameRepositoryExpression() + " && (startsWith(github.event.pull_request.head.ref, " + prefix + ") || (github.event.pull_request.head.ref == " + staging + " && github.event.pull_request.base.ref == " + base + ")) }}"
	if path == ".github/workflows/work-item-transition.yml" {
		prefilter = "${{ " + sameRepositoryExpression() + " && github.event.pull_request.head.ref == " + staging + " && github.event.pull_request.base.ref == " + base + " }}"
	}
	return strings.Replace(rendered, workflowOwnershipPrefilterPlaceholder, prefilter, 1), nil
}

func replaceDeliveryRecorderPrefilter(path, rendered string, policy config.RepositoryPolicy) (string, error) {
	if !strings.Contains(rendered, deliveryRecorderPrefilterPlaceholder) {
		return "", fmt.Errorf("render workflow %s: expected work ownership prefilter placeholder is missing", path)
	}
	if !strings.Contains(rendered, deliveryRecorderBaseBranchPlaceholder) {
		return "", fmt.Errorf("render workflow %s: expected base branch placeholder is missing", path)
	}
	work := "(startsWith(github.event.pull_request.head.ref, " + quoteWorkflowExpression(policy.Repository.WorkBranchPrefix) + ") && github.event.pull_request.base.ref == " + quoteWorkflowExpression(policy.Repository.StagingBranch) + ")"
	synchronization := "(github.event.pull_request.head.ref == " + quoteWorkflowExpression(policy.Repository.BaseBranch) + " && github.event.pull_request.base.ref == " + quoteWorkflowExpression(policy.Repository.StagingBranch) + ")"
	prefilter := "${{ (github.event_name == 'workflow_dispatch' && inputs.mode == 'record') || github.event_name == 'push' || (" + sameRepositoryExpression() + " && (" + work + " || " + synchronization + ")) }}"
	rendered = strings.Replace(rendered, deliveryRecorderPrefilterPlaceholder, prefilter, 1)
	return strings.Replace(rendered, deliveryRecorderBaseBranchPlaceholder, quoteWorkflowExpression(policy.Repository.BaseBranch), 1), nil
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
	names[policyengine.ManagedIssueIndexLabel] = struct{}{}
	names[delivery.DeliveryStateLabel] = struct{}{}
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
		"baton:managed":          {"0052CC", "Index for issues with a trusted Baton ownership record."},
		"baton:delivery-state":   {"1F6FEB", "Reserved bootstrap index for the pinned Baton delivery ledger."},
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

GitHub Issues are the repository work queue. A trusted versioned comment records
Baton ownership; the %s label is an index, not authority.
Implementation labels are %s. Comment-only labels are %s. Baton skips issues carrying %s.
Merged work PRs add %s while the issue awaits promotion review.

Ready issues use the configured form headings and required sections. Issue policy
writes ownership before controlled labels. Incomplete ready issues receive %s and
one updatable policy comment; later body/label edits do not revoke ownership.
Implementation and skip labels guide intake and recommendations, not work-PR
merge policy after durable ownership has been established.

Work PRs target %s from branches prefixed with %s and reference issues with
%s #123. Promotion PRs target %s from %s. Baton derives the included work PRs
from the promotion revisions and requires closing keywords for every referenced
work issue. Manual-only promotions need no artificial issue reference; incomplete
promotion evidence fails policy instead of guessing. Ordinary and fork PRs are
unmanaged. Same-repository use of the reserved prefix on another target fails.
`, "`baton:managed`", markdownLabels(policy.IssuePolicy.ImplementationLabels), markdownLabels(policy.IssuePolicy.CommentOnlyLabels), markdownLabels(policy.IssuePolicy.SkipLabels), markdownLabels([]string{policy.IssuePolicy.AwaitingReviewLabel}), qualityGate, policy.Repository.StagingBranch, policy.Repository.WorkBranchPrefix, policy.PRPolicy.RequiredReferenceKeyword, policy.Repository.BaseBranch, policy.Repository.StagingBranch))
}

func markdownLabels(labels []string) string {
	quoted := make([]string, len(labels))
	for index, label := range labels {
		quoted[index] = "`" + label + "`"
	}
	return strings.Join(quoted, ", ")
}
