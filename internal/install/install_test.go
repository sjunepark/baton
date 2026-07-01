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
	if len(plan.Changes) != 6 {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	for _, change := range plan.Changes {
		if change.Action != "create" {
			t.Fatalf("change = %#v", change)
		}
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
	if err := os.WriteFile(filepath.Join(root, ".github", "baton.yml"), []byte("custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, false); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if _, err := Apply(root, true); err != nil {
		t.Fatal(err)
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
