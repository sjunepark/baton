package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
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
			if stderr.String() != "" {
				t.Fatalf("stderr = %q, want empty structured-mode stderr", stderr.String())
			}
			result := decodeErrorResult(t, stdout.String())
			if result.Category != "config" || result.ExitCode != exitConfig {
				t.Fatalf("error result = %+v, want config exit %d", result, exitConfig)
			}
			if !strings.Contains(result.Message, "does-not-exist.yml") {
				t.Fatalf("message = %q, want missing config path", result.Message)
			}
		})
	}
}

func TestSubcommandHelpExitsZeroOnStdout(t *testing.T) {
	commands := [][]string{
		{"queue", "--help"},
		{"next", "--help"},
		{"lease", "--help"},
		{"help", "queue"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, "test")
			if code != exitOK {
				t.Fatalf("Run(%v) exit = %d, want %d; stderr=%s", args, code, exitOK, stderr.String())
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %q, want empty help stderr", stderr.String())
			}
			output := stdout.String()
			for _, want := range []string{"Purpose:", "Usage:", "Examples:", "Related:"} {
				if !strings.Contains(output, want) {
					t.Fatalf("help output = %q, want %q", output, want)
				}
			}
		})
	}
}

func TestUnknownSubcommandHelpReturnsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"help", "missing"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d", code, exitUsage)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "missing"`) {
		t.Fatalf("stderr = %q, want unknown command", stderr.String())
	}
}

func TestReviewThreadBodyTruncation(t *testing.T) {
	result := gh.ReviewThreadResult{
		SchemaVersion: 1,
		Kind:          "reviewThreads",
		PRNumber:      12,
		Threads: []gh.ReviewThread{{
			Comments: []gh.ReviewComment{{Body: "abcdef", AuthorKind: "human"}},
		}},
	}

	truncated := truncateReviewThreadBodies(result, 3, false)
	comment := truncated.Threads[0].Comments[0]
	if comment.Body != "abc" || comment.BodyPreview != "abc" || !comment.BodyTruncated {
		t.Fatalf("truncated comment = %#v", comment)
	}
	if comment.BodyChars != 6 || comment.FullCommand != "baton review-threads 12 --full --json" {
		t.Fatalf("truncation metadata = %#v", comment)
	}

	full := truncateReviewThreadBodies(result, 3, true)
	comment = full.Threads[0].Comments[0]
	if comment.Body != "abcdef" || comment.BodyTruncated || comment.BodyPreview != "" || comment.BodyChars != 6 {
		t.Fatalf("full comment = %#v", comment)
	}
}

func TestReviewThreadsRejectsNegativeBodyLimit(t *testing.T) {
	configPath := writeDefaultConfig(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Run([]string{"review-threads", "12", "--config", configPath, "--body-limit", "-1", "--json"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Category != "usage" || !strings.Contains(result.Message, "body-limit") {
		t.Fatalf("error result = %#v", result)
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
    "baseRepositoryFullName": "example-org/example-repo",
    "headRepositoryFullName": "example-org/example-repo"
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
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty structured-mode stderr", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Category != "config" || result.ExitCode != exitConfig {
		t.Fatalf("error result = %+v, want config exit %d", result, exitConfig)
	}
	if !strings.Contains(result.Message, config.ErrConfigNotFound.Error()) {
		t.Fatalf("message = %q, want missing config error", result.Message)
	}
	if !strings.Contains(result.Hint, "<path>") {
		t.Fatalf("hint = %q, want readable placeholder", result.Hint)
	}
}

func TestIssuePolicyApplyJSONFailureReturnsSingleErrorObject(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "issue.md")
	if err := os.WriteFile(bodyPath, []byte("### Summary\n\nDo the thing."), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := writeDefaultConfig(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"issue-policy", "--body-file", bodyPath, "--config", configPath, "--apply", "--json"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty structured-mode stderr", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Kind != "error" || result.Category != "usage" {
		t.Fatalf("error result = %+v, want usage error", result)
	}
	if !strings.Contains(result.Message, "--event") {
		t.Fatalf("message = %q, want --event requirement", result.Message)
	}
}

func TestJSONUsageErrorReturnsStructuredError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "--dry-run", "--apply", "--json"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty structured-mode stderr", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Kind != "error" || result.Category != "usage" || result.ExitCode != exitUsage {
		t.Fatalf("error result = %+v, want usage error", result)
	}
	if result.Hint == "" {
		t.Fatalf("hint is empty in %+v", result)
	}
}

func decodeErrorResult(t *testing.T, content string) errorResult {
	t.Helper()
	var result errorResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("decode error result from %q: %v", content, err)
	}
	return result
}

func writeDefaultConfig(t *testing.T, dir string) string {
	t.Helper()
	content, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
