package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestRepositoryFilesWorkflowMigrateConfigDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	from := writeWorkflowConfig(t, dir)
	to := filepath.Join(dir, "nested", "baton.yml")
	result, err := (RepositoryFilesWorkflow{}).MigrateConfig(ConfigMigrationInput{From: from, To: to, BodyLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "create" || !result.ContentTruncated || result.ContentChars <= len([]rune(result.Content)) || result.FullCommand == "" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote target: %v", err)
	}
}

func TestRepositoryFilesWorkflowLabelsUsesCompiledManifestPath(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	policy := config.DefaultConfig()
	policy.Labels.Manifest = ".config/custom-labels.yml"
	content, err := config.MarshalYAML(policy)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".github", "baton.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".config", "custom-labels.yml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("labels:\n  - name: custom\n    color: abcdef\n    description: Custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := (RepositoryFilesWorkflow{}).Labels(LabelsInput{WorkingDir: root})
	if err != nil || len(manifest.Labels) != 1 || manifest.Labels[0].Name != "custom" {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
}

func TestRepositoryFilesWorkflowRefusesOverwriteBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	from := writeWorkflowConfig(t, dir)
	to := filepath.Join(dir, "existing.yml")
	if err := os.WriteFile(to, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (RepositoryFilesWorkflow{}).MigrateConfig(ConfigMigrationInput{From: from, To: to, Apply: true, BodyLimit: 4096})
	if err == nil {
		t.Fatal("expected overwrite refusal")
	}
	content, readErr := os.ReadFile(to)
	if readErr != nil || string(content) != "keep me" {
		t.Fatalf("target changed: content=%q err=%v", content, readErr)
	}
}
