package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	fixture := filepath.Join(t.TempDir(), "pr.json")
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

	var stdout, stderr bytes.Buffer
	code := Run([]string{"pr-policy", "--fixture", fixture, "--json"}, &stdout, &stderr, "test")
	if code != exitPolicy {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitPolicy, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "agent-work/") {
		t.Fatalf("stdout = %q, want branch-prefix policy error", stdout.String())
	}
}
