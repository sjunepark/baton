package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestApplyWithOptionsRendersGoInstallTarget(t *testing.T) {
	root := t.TempDir()
	if _, err := ApplyWithOptions(root, false, Options{GoInstall: "github.com/open-creo/baton/cmd/baton@v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "issue-policy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "github.com/open-creo/baton/cmd/baton@v1.2.3") {
		t.Fatalf("workflow did not use custom install target:\n%s", text)
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
