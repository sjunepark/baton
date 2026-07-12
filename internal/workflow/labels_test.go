package workflow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
)

type labelSyncGitHub struct {
	existing   []gh.Label
	created    []gh.Label
	updated    []gh.Label
	failCreate bool
	latest     []gh.Label
	listCalls  int
}

func TestLabelSyncWorkflowUsesCompiledManifestPath(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init", root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	policy := config.DefaultConfig()
	policy.Labels.Manifest = ".config/custom-labels.yml"
	configContent, err := config.MarshalYAML(policy)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".github", "baton.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".config", "custom-labels.yml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("labels:\n  - name: custom\n    color: abcdef\n    description: Custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &labelSyncGitHub{}
	workflow := LabelSyncWorkflow{newClient: func(context.Context, LabelSyncInput) (LabelSyncGitHub, error) { return client, nil }}
	plan, err := workflow.Run(LabelSyncInput{Repository: "example/repo", WorkingDir: root})
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Name != "custom" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func (f *labelSyncGitHub) ListLabelsContext(context.Context, string) ([]gh.Label, error) {
	f.listCalls++
	if f.listCalls > 1 && f.latest != nil {
		return f.latest, nil
	}
	return f.existing, nil
}

func TestLabelSyncWorkflowRefusesStalePlanBeforeMutation(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "labels.yml")
	if err := os.WriteFile(manifest, []byte("labels:\n  - name: bug\n    color: ff0000\n    description: Bug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &labelSyncGitHub{existing: []gh.Label{{Name: "bug", Color: "000000", Description: "Bug"}}, latest: []gh.Label{{Name: "bug", Color: "ff0000", Description: "Updated elsewhere"}}}
	workflow := LabelSyncWorkflow{newClient: func(context.Context, LabelSyncInput) (LabelSyncGitHub, error) { return client, nil }}
	plan, err := workflow.Run(LabelSyncInput{Repository: "example/repo", ManifestPath: manifest, Apply: true})
	if err == nil || plan.Report == nil || plan.Report.Status != "refused" || len(client.updated) != 0 || len(client.created) != 0 {
		t.Fatalf("plan=%+v err=%v created=%v updated=%v", plan, err, client.created, client.updated)
	}
}
func (f *labelSyncGitHub) CreateLabelContext(_ context.Context, _ string, label gh.Label) error {
	if f.failCreate {
		return errors.New("create failed")
	}
	f.created = append(f.created, label)
	return nil
}

func TestLabelSyncWorkflowPreservesPartialOperationReport(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "labels.yml")
	content := []byte("labels:\n  - name: bug\n    color: ff0000\n    description: Bug\n  - name: docs\n    color: 00ff00\n    description: Documentation\n")
	if err := os.WriteFile(manifest, content, 0o600); err != nil {
		t.Fatal(err)
	}
	client := &labelSyncGitHub{existing: []gh.Label{{Name: "bug", Color: "000000", Description: "Bug"}}, failCreate: true}
	workflow := LabelSyncWorkflow{newClient: func(context.Context, LabelSyncInput) (LabelSyncGitHub, error) { return client, nil }}
	plan, err := workflow.Run(LabelSyncInput{Repository: "example/repo", ManifestPath: manifest, Apply: true})
	if err == nil {
		t.Fatal("expected second operation failure")
	}
	if plan.Report == nil || plan.Report.Status != "partial" || len(plan.Report.Operations) != 2 || plan.Report.Operations[0].Status != "applied" || plan.Report.Operations[1].Status != "failed" {
		t.Fatalf("plan report = %+v", plan.Report)
	}
}
func (f *labelSyncGitHub) UpdateLabelContext(_ context.Context, _ string, label gh.Label) error {
	f.updated = append(f.updated, label)
	return nil
}

func TestLabelSyncWorkflowPlansAndAppliesTransportMutations(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "labels.yml")
	content := []byte("labels:\n  - name: bug\n    color: ff0000\n    description: Bug\n  - name: docs\n    color: 00ff00\n    description: Documentation\n")
	if err := os.WriteFile(manifest, content, 0o600); err != nil {
		t.Fatal(err)
	}
	client := &labelSyncGitHub{existing: []gh.Label{{Name: "bug", Color: "000000", Description: "Bug"}}}
	workflow := LabelSyncWorkflow{newClient: func(context.Context, LabelSyncInput) (LabelSyncGitHub, error) { return client, nil }}
	plan, err := workflow.Run(LabelSyncInput{Repository: "example/repo", ManifestPath: manifest, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Counts.Update != 1 || plan.Counts.Create != 1 || len(client.updated) != 1 || len(client.created) != 1 {
		t.Fatalf("plan=%+v created=%v updated=%v", plan, client.created, client.updated)
	}
}

func TestLabelSyncWorkflowRejectsRepositoryMismatchBeforeClient(t *testing.T) {
	workflow := LabelSyncWorkflow{newClient: func(context.Context, LabelSyncInput) (LabelSyncGitHub, error) {
		t.Fatal("repository mismatch must not construct a GitHub client")
		return nil, nil
	}}
	_, err := workflow.Run(LabelSyncInput{Repository: "flag/repo", EnvironmentRepo: "environment/repo"})
	if err == nil {
		t.Fatal("expected repository mismatch")
	}
}
