package install

import (
	"os"
	"path/filepath"
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
