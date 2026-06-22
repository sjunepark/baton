package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestNumberedReadCommandsValidateExplicitConfig(t *testing.T) {
	commands := [][]string{
		{"pr", "1", "--config", "does-not-exist.yml", "--json"},
		{"checks", "1", "--config", "does-not-exist.yml", "--json"},
		{"review-threads", "1", "--config", "does-not-exist.yml", "--json"},
	}

	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, "test")
			if code != exitConfig {
				t.Fatalf("Run(%v) exit = %d, want %d; stderr=%s", args, code, exitConfig, stderr.String())
			}
			if !strings.Contains(stderr.String(), "does-not-exist.yml") {
				t.Fatalf("stderr = %q, want missing config path", stderr.String())
			}
		})
	}
}

func TestPRPolicyJSONReturnsPolicyExitOnErrors(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "pr.json")
	content := `{
  "pullRequest": {
    "number": 10,
    "title": "Update issue policy",
    "body": "Refs #123",
    "baseRef": "agent",
    "headRef": "agent/123-issue-policy",
    "baseRepositoryFullName": "open-creo/creo",
    "headRepositoryFullName": "open-creo/creo"
  },
  "referencedIssues": [
    { "number": 123, "labels": ["agent:ready-trivial"] }
  ],
  "commitMessages": ["Document issue policy"]
}`
	if err := os.WriteFile(fixture, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := writeDefaultConfig(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"pr-policy", "--fixture", fixture, "--config", configPath, "--json"}, &stdout, &stderr, "test")
	if code != exitPolicy {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitPolicy, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "agent-work/") {
		t.Fatalf("stdout = %q, want branch-prefix policy error", stdout.String())
	}
}

func TestPolicyCommandFailsWhenRepoConfigMissing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	bodyPath := filepath.Join(dir, "issue.md")
	if err := os.WriteFile(bodyPath, []byte("### Summary\n\nDo the thing."), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"issue-policy", "--body-file", bodyPath, "--json"}, &stdout, &stderr, "test")
	if code != exitConfig {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitConfig, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), config.ErrConfigNotFound.Error()) {
		t.Fatalf("stderr = %q, want missing config error", stderr.String())
	}
}

func writeDefaultConfig(t *testing.T, dir string) string {
	t.Helper()
	content, err := config.MarshalYAML(config.DefaultCreoCompat())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
