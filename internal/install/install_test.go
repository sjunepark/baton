package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"gopkg.in/yaml.v3"
)

func TestPreviewPlansTemplateCreation(t *testing.T) {
	plan, err := Preview(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 8 {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	for _, change := range plan.Changes {
		if change.Action != "create" {
			t.Fatalf("change = %#v", change)
		}
	}
}

func TestPreviewRejectsInvalidExistingConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "strict parse error",
			content: "version: 1\nunexpected: true\n",
			want:    "field unexpected not found",
		},
		{
			name: "validation error",
			content: `version: 1
repository:
  base_branch: main
  staging_branch: main
`,
			want: "repository.base_branch and repository.staging_branch must differ",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".github", "baton.yml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}

			plan, err := Preview(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Preview() plan=%+v error=%v, want error containing %q", plan, err, test.want)
			}
			if len(plan.Changes) != 0 {
				t.Fatalf("invalid config produced reconciliation changes: %+v", plan.Changes)
			}
		})
	}
}

func TestApplyWritesTemplatesAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	if _, err := Apply(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "baton.yml")); err != nil {
		t.Fatal(err)
	}
	guidancePath := filepath.Join(root, ".github", "ISSUE_WORKFLOW.md")
	if err := os.WriteFile(guidancePath, []byte("custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, false); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if _, err := Apply(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPreflightsAllConflictsBeforeFirstWrite(t *testing.T) {
	root := t.TempDir()
	conflict := filepath.Join(root, ".github", "workflows", "pr-policy.yml")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("user-owned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := Apply(root, false)
	if err == nil {
		t.Fatal("expected whole-plan conflict refusal")
	}
	if plan.Report == nil || plan.Report.Status != "refused" {
		t.Fatalf("report = %+v", plan.Report)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "baton.yml")); !os.IsNotExist(err) {
		t.Fatalf("earlier file was written before later conflict refusal: %v", err)
	}
}

func TestPreviewIncludesStableIdentityPreconditionsContentAndDiff(t *testing.T) {
	root := t.TempDir()
	first, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanID == "" || first.PlanID != second.PlanID || first.SchemaVersion != 2 || first.Kind != "repositoryReconciliationPlan" {
		t.Fatalf("plan identity = %+v / %+v", first, second)
	}
	for _, change := range first.Changes {
		if change.DesiredContent == "" || change.Diff == "" || change.Precondition.Exists || change.Ownership != "absent" {
			t.Fatalf("change = %+v", change)
		}
	}
	path := filepath.Join(root, ".github", "ISSUE_WORKFLOW.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("different\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := Preview(root)
	if err != nil {
		t.Fatal(err)
	}
	var guidanceChange *FileChange
	for index := range changed.Changes {
		if changed.Changes[index].Path == ".github/ISSUE_WORKFLOW.md" {
			guidanceChange = &changed.Changes[index]
			break
		}
	}
	if changed.PlanID == first.PlanID || guidanceChange == nil || guidanceChange.Precondition.SHA256 == "" || !guidanceChange.Conflict || guidanceChange.Ownership != "unmanaged" {
		t.Fatalf("changed plan = %+v", changed)
	}
}

func TestPreviewRejectsManagedSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, ".github", "baton.yml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Preview(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestBatonConfigTemplateIncludesSetupBaseline(t *testing.T) {
	content, err := templateContent(".github/baton.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	wantBaseline := "baseline_baton_version: " + config.DefaultConfig().Setup.BaselineBatonVersion
	if !strings.Contains(text, "setup:") || !strings.Contains(text, wantBaseline) {
		t.Fatalf("baton config template missing setup baseline:\n%s", text)
	}
}

func TestTemplatesSayManagedButEditable(t *testing.T) {
	for _, path := range templatePaths() {
		t.Run(path, func(t *testing.T) {
			content, err := templateContent(path, Options{}.withDefaults())
			if err != nil {
				t.Fatal(err)
			}
			text := string(content)
			if !strings.Contains(text, "Managed by Baton") || !strings.Contains(text, "Edit") {
				t.Fatalf("%s missing managed/editable marker:\n%s", path, text)
			}
		})
	}
}

func TestApplyWithOptionsRendersGoInstallTarget(t *testing.T) {
	root := t.TempDir()
	if _, err := ApplyWithOptions(root, false, Options{GoInstall: "github.com/example-org/baton/cmd/baton@v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "issue-policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "github.com/example-org/baton/cmd/baton@v1.2.3") {
		t.Fatalf("workflow did not use custom install target:\n%s", text)
	}
	if !strings.Contains(text, "actions/setup-go@v5") || !strings.Contains(text, "go-version: '1.26.x'") {
		t.Fatalf("workflow did not set up Go:\n%s", text)
	}
	if !strings.Contains(text, "GOBIN=\"$RUNNER_TEMP/baton-bin\" go install github.com/example-org/baton/cmd/baton@v1.2.3") {
		t.Fatalf("workflow did not install Baton into runner temp bin:\n%s", text)
	}
	if !strings.Contains(text, "echo \"$RUNNER_TEMP/baton-bin\" >> \"$GITHUB_PATH\"") {
		t.Fatalf("workflow did not add Baton install directory to PATH:\n%s", text)
	}
}

func TestApplyWithOptionsRendersInstallCommand(t *testing.T) {
	root := t.TempDir()
	command := "curl -fsSL https://example.invalid/baton.sh | sh\nbaton version"
	if _, err := ApplyWithOptions(root, false, Options{InstallCommand: command}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "pr-policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "curl -fsSL https://example.invalid/baton.sh | sh\n          baton version") {
		t.Fatalf("workflow did not render install command with indentation:\n%s", text)
	}
	if !strings.Contains(text, "group: baton-delivery-${{ github.repository_id }}") || !strings.Contains(text, "queue: max") {
		t.Fatalf("PR policy workflow does not share delivery concurrency:\n%s", text)
	}
	if !strings.Contains(text, "checks: write") || !strings.Contains(text, "issues: write") {
		t.Fatalf("PR policy workflow cannot seal plans or invalidate overlapping promotions:\n%s", text)
	}
	transition, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "work-item-transition.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(transition), "curl -fsSL") || !strings.Contains(string(transition), defaultGoInstall) {
		t.Fatalf("trusted transition workflow used arbitrary install command:\n%s", transition)
	}
	recorder, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "delivery-recorder.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorder), "curl -fsSL") || !strings.Contains(string(recorder), defaultGoInstall) {
		t.Fatalf("trusted delivery workflow used arbitrary install command:\n%s", recorder)
	}
	for _, fragment := range []string{
		"checks: write", "issues: write", "baton-delivery-${{ github.repository_id }}", "queue: max", "delivery-record --event",
		"inputs.mode == 'record'", "plan-bootstrap:", "apply-bootstrap:", "environment: baton-delivery-bootstrap", "--observed-at \"$OBSERVED_AT\"", "--plan-id \"$PLAN_ID\"",
	} {
		if !strings.Contains(string(recorder), fragment) {
			t.Fatalf("trusted delivery workflow missing %q:\n%s", fragment, recorder)
		}
	}
}

func TestRenderManagedFilesRequiresPinnedTransitionBinary(t *testing.T) {
	for _, target := range []string{"github.com/example/baton/cmd/baton@latest", "github.com/example/baton;curl@v1.2.3"} {
		_, err := RenderManagedFiles(config.DefaultConfig(), Options{GoInstall: target})
		if err == nil || !strings.Contains(err.Error(), "module@vX.Y.Z") {
			t.Fatalf("RenderManagedFiles(%q) error = %v, want exact version rejection", target, err)
		}
	}
}

func TestRenderedDeliveryRecorderRoutesSynchronizationAndBasePushWithoutOwningPolicy(t *testing.T) {
	cfg := config.DefaultConfig()
	content, err := templateContent(".github/workflows/delivery-recorder.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, fragment := range []string{
		"push:", "- " + cfg.Repository.BaseBranch,
		"github.event.pull_request.head.ref == '" + cfg.Repository.BaseBranch + "'",
		"github.event.pull_request.base.ref == '" + cfg.Repository.StagingBranch + "'",
		"github.event_name == 'push'", `github.event_name }}" == "pull_request_target"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("delivery recorder missing %q:\n%s", fragment, text)
		}
	}
	policyWorkflow, err := templateContent(".github/workflows/pr-policy.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(policyWorkflow), "head.ref == '"+cfg.Repository.BaseBranch+"' && github.event.pull_request.base.ref == '"+cfg.Repository.StagingBranch+"'") {
		t.Fatalf("PR policy prefilter must not own base-to-staging synchronization:\n%s", policyWorkflow)
	}
}

func TestRenderedIssuePolicyRepairsTrustedOwnershipCommentEvents(t *testing.T) {
	content, err := templateContent(".github/workflows/issue-policy.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, fragment := range []string{"issue_comment:", "- edited", "- deleted", "github-actions[bot]", "ref: ${{ github.event.repository.default_branch }}", "persist-credentials: false"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("issue workflow missing %q:\n%s", fragment, text)
		}
	}
}

func TestRenderedWorkflowsPinEveryCheckoutAndDisableCredentialPersistence(t *testing.T) {
	for path, test := range map[string]struct {
		ref       string
		checkouts int
	}{
		".github/workflows/issue-policy.yml":         {ref: "ref: ${{ github.event.repository.default_branch }}", checkouts: 1},
		".github/workflows/pr-policy.yml":            {ref: "ref: ${{ github.event.pull_request.base.sha }}", checkouts: 1},
		".github/workflows/work-item-transition.yml": {ref: "ref: ${{ github.event.pull_request.base.sha }}", checkouts: 1},
		".github/workflows/delivery-recorder.yml":    {ref: "ref: ${{ github.event.repository.default_branch }}", checkouts: 3},
	} {
		t.Run(path, func(t *testing.T) {
			content, err := templateContent(path, Options{}.withDefaults())
			if err != nil {
				t.Fatal(err)
			}
			text := string(content)
			if got := strings.Count(text, "uses: actions/checkout@v4"); got != test.checkouts {
				t.Fatalf("checkout count = %d, want %d:\n%s", got, test.checkouts, text)
			}
			if got := strings.Count(text, test.ref); got != test.checkouts {
				t.Fatalf("trusted ref count = %d, want %d:\n%s", got, test.checkouts, text)
			}
			if got := strings.Count(text, "persist-credentials: false"); got != test.checkouts {
				t.Fatalf("credential hardening count = %d, want %d:\n%s", got, test.checkouts, text)
			}
		})
	}
}

func TestIssueTemplateWorkKindsAreMappedInDefaultConfig(t *testing.T) {
	content, err := templateContent(".github/ISSUE_TEMPLATE/agent-work.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	var issueTemplate struct {
		Body []struct {
			ID         string `yaml:"id"`
			Attributes struct {
				Options []string `yaml:"options"`
			} `yaml:"attributes"`
		} `yaml:"body"`
	}
	if err := yaml.Unmarshal(content, &issueTemplate); err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultConfig()
	for _, field := range issueTemplate.Body {
		if field.ID != "work_kind" {
			continue
		}
		for _, option := range field.Attributes.Options {
			if defaults.IssuePolicy.WorkKindLabels[option] == "" {
				t.Fatalf("work kind option %q has no default label mapping", option)
			}
		}
		return
	}
	t.Fatal("work_kind field not found in issue template")
}

func TestIssueTemplatePrioritiesAreMappedInDefaultConfig(t *testing.T) {
	content, err := templateContent(".github/ISSUE_TEMPLATE/agent-work.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	var issueTemplate struct {
		Body []struct {
			ID         string `yaml:"id"`
			Attributes struct {
				Options []string `yaml:"options"`
				Default int      `yaml:"default"`
			} `yaml:"attributes"`
		} `yaml:"body"`
	}
	if err := yaml.Unmarshal(content, &issueTemplate); err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultConfig()
	for _, field := range issueTemplate.Body {
		if field.ID != "priority" {
			continue
		}
		if field.Attributes.Default != 2 {
			t.Fatalf("priority default = %d, want P2 index 2", field.Attributes.Default)
		}
		for _, option := range field.Attributes.Options {
			if defaults.IssuePolicy.PriorityLabels[option] == "" {
				t.Fatalf("priority option %q has no default label mapping", option)
			}
		}
		return
	}
	t.Fatal("priority field not found in issue template")
}

func TestRenderedDefaultConfigCompilesToDefaultPolicy(t *testing.T) {
	content, err := templateContent(".github/baton.yml", Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.MarshalYAML(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("installed policy differs from compiled defaults:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPinnedConfigTemplateSemanticallyMatchesCompiledDefaults(t *testing.T) {
	content, err := templatesFS.ReadFile("templates/.github/baton.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := config.MarshalYAML(config.DefaultConfig())
	got, _ := config.MarshalYAML(loaded)
	if string(got) != string(want) {
		t.Fatalf("pinned config template drifted from compiled defaults:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(string(content), "allow_direct_base_branch_prs") || strings.Contains(string(content), "reject_all_trivial_multi_issue_prs") {
		t.Fatalf("pinned config template contains retired PR policy option:\n%s", content)
	}
}

func TestRenderManagedFilesPropagatesCustomRepositoryPolicy(t *testing.T) {
	policy := config.DefaultConfig()
	policy.Repository.DefaultRemote = "upstream"
	policy.Repository.BaseBranch = "stable"
	policy.Repository.StagingBranch = "integration"
	policy.Repository.WorkBranchPrefix = "bot-work/"
	policy.Labels.Manifest = ".config/project-labels.yml"
	policy.IssuePolicy.FormSections["summary"] = "Requested outcome"
	policy.IssuePolicy.WorkKindLabels = map[string]string{"Defect": "kind:defect"}
	policy.IssuePolicy.ControlledLabelGroups["work_kind"] = []string{"kind:defect"}
	policy.IssuePolicy.AgentModeLabels = map[string]string{"Ship it": "workflow:ready", "Research": "workflow:research"}
	policy.IssuePolicy.ControlledLabelGroups["agent_mode"] = []string{"workflow:ready", "workflow:research"}
	policy.IssuePolicy.ImplementationLabels = []string{"workflow:ready"}
	policy.IssuePolicy.CommentOnlyLabels = []string{"workflow:research"}
	policy.IssuePolicy.RequiredSections = map[string][]string{"ship-it": {"summary"}}
	policy.IssuePolicy.ControlledLabelGroups["quality_gate"] = []string{"workflow:blocked"}
	policy.IssuePolicy.SkipLabels = []string{"workflow:blocked", "needs:review"}

	files, err := RenderManagedFiles(policy, Options{}.withDefaults())
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, file := range files {
		byPath[file.Path] = string(file.Content)
	}
	for path, fragments := range map[string][]string{
		".github/baton.yml":                          {"default_remote: upstream", "base_branch: stable", "staging_branch: integration", "work_branch_prefix: bot-work/", "manifest: .config/project-labels.yml"},
		".github/workflows/pr-policy.yml":            {"startsWith(github.event.pull_request.head.ref, 'bot-work/')", "head.ref == 'integration'", "base.ref == 'stable'"},
		".github/workflows/work-item-transition.yml": {"head.ref == 'integration'", "base.ref == 'stable'", "issues: write", "queue: max"},
		".github/ISSUE_TEMPLATE/agent-work.yml":      {"label: Requested outcome", "- Defect", "- Ship it", "- Research"},
		".github/ISSUE_WORKFLOW.md":                  {"`workflow:ready`", "`workflow:blocked`", "target integration", "prefixed with bot-work/", "target stable"},
		".config/project-labels.yml":                 {"name: baton:managed", "name: kind:defect", "name: workflow:ready", "name: workflow:research", "name: workflow:blocked"},
	} {
		content, exists := byPath[path]
		if !exists {
			t.Fatalf("missing rendered file %s; paths=%v", path, byPath)
		}
		for _, fragment := range fragments {
			if !strings.Contains(content, fragment) {
				t.Fatalf("%s missing %q:\n%s", path, fragment, content)
			}
		}
	}
	var form struct {
		Body []struct {
			ID          string `yaml:"id"`
			Validations struct {
				Required bool `yaml:"required"`
			} `yaml:"validations"`
		} `yaml:"body"`
	}
	if err := yaml.Unmarshal([]byte(byPath[".github/ISSUE_TEMPLATE/agent-work.yml"]), &form); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	for _, field := range form.Body {
		required[field.ID] = field.Validations.Required
	}
	if !required["work_kind"] || !required["agent_mode"] || !required["summary"] || !required["priority"] || required["context_evidence"] || required["acceptance_criteria"] {
		t.Fatalf("custom form required fields = %v", required)
	}
}

func TestRepositoryReconciliationDetectsOwnershipPrefilterDriftInBothPRWorkflows(t *testing.T) {
	root := t.TempDir()
	initial := config.DefaultConfig()
	initial.Repository.WorkBranchPrefix = "first-work/"
	files, err := RenderManagedFiles(initial, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyManagedFiles(root, files, true); err != nil {
		t.Fatal(err)
	}

	updated := initial
	updated.Repository.WorkBranchPrefix = "second-work/"
	updated.Repository.StagingBranch = "integration"
	updated.Repository.BaseBranch = "stable"
	desired, err := RenderManagedFiles(updated, Options{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PreviewManagedFiles(root, desired)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		".github/workflows/pr-policy.yml":            false,
		".github/workflows/work-item-transition.yml": false,
	}
	for _, change := range plan.Changes {
		if _, tracked := want[change.Path]; tracked && change.Conflict && strings.Contains(change.DesiredContent, "integration") && strings.Contains(change.DesiredContent, "stable") {
			if change.Path != ".github/workflows/pr-policy.yml" || strings.Contains(change.DesiredContent, "second-work/") {
				want[change.Path] = true
			}
		}
	}
	for path, detected := range want {
		if !detected {
			t.Fatalf("ownership prefilter drift was not detected for %s: %+v", path, plan.Changes)
		}
	}
}

func TestRenderManagedFilesQuotesYAMLSensitiveBranchNames(t *testing.T) {
	policy := config.DefaultConfig()
	policy.Repository.StagingBranch = "#release"
	files, err := RenderManagedFiles(policy, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path != ".github/workflows/pr-policy.yml" {
			continue
		}
		if !strings.Contains(string(file.Content), `head.ref == '#release'`) {
			t.Fatalf("workflow expression value was not quoted:\n%s", file.Content)
		}
		var document any
		if err := yaml.Unmarshal(file.Content, &document); err != nil {
			t.Fatalf("rendered workflow is invalid YAML: %v", err)
		}
		return
	}
	t.Fatal("PR policy workflow not rendered")
}

func TestReplaceWorkflowOwnershipPrefilterRequiresPinnedPlaceholder(t *testing.T) {
	_, err := replaceWorkflowOwnershipPrefilter(".github/workflows/pr-policy.yml", "branches: [custom]", config.DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), ".github/workflows/pr-policy.yml") || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenderManagedFilesRejectsManifestCollision(t *testing.T) {
	policy := config.DefaultConfig()
	policy.Labels.Manifest = ".github/./baton.yml"
	_, err := RenderManagedFiles(policy, Options{})
	if err == nil || !strings.Contains(err.Error(), "resolve to the same target") {
		t.Fatalf("RenderManagedFiles() error = %v, want duplicate managed target", err)
	}
}

func TestPreviewManagedFilesRejectsDuplicateNormalizedTargets(t *testing.T) {
	_, err := PreviewManagedFiles(t.TempDir(), []ManagedFile{
		{Path: ".github/baton.yml", Content: []byte("one")},
		{Path: ".github/./baton.yml", Content: []byte("two")},
	})
	if err == nil || !strings.Contains(err.Error(), "resolve to the same target") {
		t.Fatalf("PreviewManagedFiles() error = %v, want duplicate managed target", err)
	}
}
