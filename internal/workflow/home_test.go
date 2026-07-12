package workflow

import (
	"path/filepath"
	"testing"
)

func TestHomeWorkflowUsesExplicitInputsWithoutEnvironmentGlobals(t *testing.T) {
	home := t.TempDir()
	result := NewHomeWorkflow().Run(HomeInput{
		WorkingDir: t.TempDir(), EnvironmentRepo: "example/repo", GitHubToken: "token",
		ExecutablePath: filepath.Join(home, "go", "bin", "baton"), HomeDir: home,
	})
	if result.Repo != "example/repo" || result.Auth != "ok (token env)" || result.Bin != filepath.Join("~", "go", "bin", "baton") {
		t.Fatalf("result = %+v", result)
	}
	if result.Config != "missing (.github/baton.yml)" || result.Next == "run `baton next --format toon`" {
		t.Fatalf("config=%q next=%q", result.Config, result.Next)
	}
}
