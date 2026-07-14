package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/doctor"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/snapshot"
	"github.com/sjunepark/baton/internal/workflow"
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

func TestWriteJSONIsCompact(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := writeJSON(&stdout, &stderr, map[string]any{"kind": "example", "items": []int{1, 2}})
	if code != exitOK {
		t.Fatalf("writeJSON exit = %d; stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != `{"items":[1,2],"kind":"example"}`+"\n" {
		t.Fatalf("json = %q, want compact object", got)
	}
}

func TestPRTransitionRequiresExplicitExecutionMode(t *testing.T) {
	for _, args := range [][]string{
		{"pr-transition", "--event", "event.json", "--json"},
		{"pr-transition", "--event", "event.json", "--dry-run", "--apply", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr, "test")
		if code != exitUsage {
			t.Fatalf("Run(%v) exit = %d, want %d", args, code, exitUsage)
		}
		if result := decodeErrorResult(t, stdout.String()); !strings.Contains(result.Message, "exactly one") {
			t.Fatalf("error = %+v", result)
		}
	}
}

func TestSubcommandHelpExitsZeroOnStdout(t *testing.T) {
	commands := [][]string{
		{"queue", "--help"},
		{"next", "--help"},
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

func TestNoArgsShowsHomeAndHelpStaysGlobal(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "description: Coordinate GitHub issue/PR agent workflows") {
		t.Fatalf("no-args output = %q, want home view", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--help"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run --help exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "Usage:") || strings.Contains(stdout.String(), "leases.active:") {
		t.Fatalf("--help output = %q, want global help", stdout.String())
	}
}

func TestVersionCommandAndFlag(t *testing.T) {
	for _, args := range [][]string{
		{"version"},
		{"--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, "v1.2.3")
			if code != exitOK {
				t.Fatalf("Run(%v) exit = %d, want %d; stderr=%s", args, code, exitOK, stderr.String())
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if got := stdout.String(); got != "v1.2.3\n" {
				t.Fatalf("stdout = %q, want version", got)
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

func TestHomeFormatTOON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"home", "--format", "toon"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"kind: home", "schemaVersion: 1", "next:", "help[3]:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("home toon = %q, want %q", output, want)
		}
	}
	if !strings.Contains(output, "Run `baton init --dry-run --json`.") {
		t.Fatalf("home toon = %q, want valid init dry-run suggestion", output)
	}
	if strings.Contains(output, "baton init --dry-run --format toon") {
		t.Fatalf("home toon = %q, want no invalid init --format suggestion", output)
	}
	if strings.Contains(output, "lease") {
		t.Fatalf("home toon = %q, want no lease fields", output)
	}
}

func TestHomeBinUsesCurrentExecutablePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	result := workflow.NewHomeWorkflow().Run(workflow.HomeInput{ExecutablePath: filepath.Join(home, "go", "bin", "baton"), HomeDir: home})
	if result.Bin != filepath.Join("~", "go", "bin", "baton") {
		t.Fatalf("home bin = %q, want home-relative executable path", result.Bin)
	}
}

func TestDoctorFormatTOON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--format", "toon"}, &stdout, &stderr, "test")
	if code != exitOK && code != exitLocalGit {
		t.Fatalf("Run exit = %d, want ok or local git; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"kind: doctor", "readyState:", "counts.ok:", "checks["} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor toon = %q, want %q", output, want)
		}
	}
}

func TestFormatConflictReturnsStructuredError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"doctor", "--json", "--format", "toon"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Category != "usage" || !strings.Contains(result.Message, "--json cannot be combined") {
		t.Fatalf("error result = %#v", result)
	}
}

func TestNumberedCommandMissingNumberHonorsTOONFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"checks", "--format", "toon"}, &stdout, &stderr, "test")
	if code != exitUsage {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "kind: error\n") || !strings.Contains(stdout.String(), "category: usage\n") {
		t.Fatalf("stdout = %q, want TOON error", stdout.String())
	}
}

func TestTOONRenderersStable(t *testing.T) {
	var stdout bytes.Buffer
	snapshot := queue.Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          "example/repo",
		Counts:        queue.SnapshotCounts{TotalIssues: 1, EligibleIssues: 1, EligibleByAction: map[string]int{"issue-implementation": 1}, OpenPullRequests: 0},
		Issues: []queue.IssueState{{
			Issue:         queue.Issue{Number: 7, Title: "Fix flaky test"},
			Eligible:      true,
			Action:        "issue-implementation",
			PriorityLabel: "priority:p2",
			Reasons:       []string{"eligible"},
		}},
		Help: []string{"Run `baton next --format toon`."},
	}
	writeQueueTOON(&stdout, snapshot)
	wantQueue := "kind: queueSnapshot\nschemaVersion: 1\nrepo: example/repo\ncounts.totalIssues: 1\ncounts.eligibleIssues: 1\ncounts.eligibleByAction.issue-implementation: 1\ncounts.skippedIssues: 0\ncounts.openPullRequests: 0\nissues[1]:\n  - number=7 eligible=true action=issue-implementation priorityLabel=priority:p2 title=Fix flaky test reasons=eligible\nhelp[1]:\n  - Run `baton next --format toon`.\n"
	if stdout.String() != wantQueue {
		t.Fatalf("queue toon = %q\nwant %q", stdout.String(), wantQueue)
	}

	stdout.Reset()
	writePRsTOON(&stdout, workflow.BuildPullRequestsResult("example/repo", []queue.PullState{{PullRequest: queue.PullRequest{Number: 8, Title: "Update docs", HeadRef: "agent-work/8", BaseRef: "agent", CheckState: "success"}}}))
	if !strings.Contains(stdout.String(), "kind: pullRequests\n") || !strings.Contains(stdout.String(), "pullRequests[1]:\n  - number=8 headRef=agent-work/8 baseRef=agent checkState=success title=Update docs\n") {
		t.Fatalf("prs toon = %q", stdout.String())
	}

	stdout.Reset()
	writeQueueTOONFields(&stdout, snapshot, []string{"number", "title", "action", "priorityLabel", "reasons"})
	if !strings.Contains(stdout.String(), "issues[1]:\n  - number=7 title=Fix flaky test action=issue-implementation priorityLabel=priority:p2 reasons=eligible\n") {
		t.Fatalf("queue fields toon = %q", stdout.String())
	}

	stdout.Reset()
	writePRsTOONFields(&stdout, workflow.BuildPullRequestsResult("example/repo", []queue.PullState{{PullRequest: queue.PullRequest{Number: 8, Title: "Update docs", HeadRef: "agent-work/8", CheckState: "success"}}}), []string{"number", "title", "headRef", "checkState"})
	if !strings.Contains(stdout.String(), "pullRequests[1]:\n  - number=8 title=Update docs headRef=agent-work/8 checkState=success\n") {
		t.Fatalf("prs fields toon = %q", stdout.String())
	}

	stdout.Reset()
	writeNextTOON(&stdout, queue.NextCandidates{
		SchemaVersion:     2,
		Kind:              "nextCandidates",
		Repo:              "example/repo",
		SelectedAction:    "issue-implementation",
		Reason:            "eligible-issue",
		SelectionReason:   "implementation-work-precedes-investigation",
		SelectionRequired: true,
		Candidates: []queue.NextCandidate{
			{Type: "issue", Number: 7, Title: "Fix flaky test", PriorityLabel: "priority:p2"},
			{Type: "issue", Number: 9, Title: "Update docs"},
		},
		DeferredEligibleItems: []queue.NextCandidate{
			{Type: "issue", Number: 11, Title: "Investigate flaky env"},
		},
		Instructions: []string{"Choose exactly one candidate."},
	})
	if !strings.Contains(stdout.String(), "kind: nextCandidates\n") ||
		!strings.Contains(stdout.String(), "selectedAction: issue-implementation\n") ||
		!strings.Contains(stdout.String(), "selectionReason: implementation-work-precedes-investigation\n") ||
		!strings.Contains(stdout.String(), "selectionRequired: true\n") ||
		!strings.Contains(stdout.String(), "candidates[2]:\n  - type=issue number=7 title=Fix flaky test priorityLabel=priority:p2\n  - type=issue number=9 title=Update docs\n") ||
		!strings.Contains(stdout.String(), "deferredEligibleItems[1]:\n  - type=issue number=11 title=Investigate flaky env\n") ||
		!strings.Contains(stdout.String(), "instructions[1]:\n  - Choose exactly one candidate.\n") {
		t.Fatalf("next toon = %q", stdout.String())
	}

	stdout.Reset()
	writeChecksTOON(&stdout, gh.CheckRollup{SchemaVersion: 1, Kind: "checkRollup", Repo: "example/repo", PRNumber: 4, State: "failure", Count: 1, Summary: gh.CheckSummary{Failed: 1}, Checks: []gh.CheckState{{Name: "unit", Status: "completed", Conclusion: "failure"}}})
	if !strings.Contains(stdout.String(), "summary.failed: 1\n") || !strings.Contains(stdout.String(), "checks[1]:\n  - name=unit status=completed conclusion=failure\n") {
		t.Fatalf("checks toon = %q", stdout.String())
	}

	stdout.Reset()
	writeChecksTOONFields(&stdout, gh.CheckRollup{SchemaVersion: 1, Kind: "checkRollup", Repo: "example/repo", PRNumber: 4, State: "failure", Count: 1, Checks: []gh.CheckState{{Name: "unit", Status: "completed", Conclusion: "failure", URL: "https://example/check"}}}, []string{"name", "state", "url"})
	if !strings.Contains(stdout.String(), "checks[1]:\n  - name=unit state=failure url=https://example/check\n") {
		t.Fatalf("checks fields toon = %q", stdout.String())
	}

	stdout.Reset()
	writeReviewThreadsTOON(&stdout, gh.ReviewThreadResult{SchemaVersion: 1, Kind: "reviewThreads", Repo: "example/repo", PRNumber: 4, Count: 1, Summary: gh.ThreadSummary{Total: 1, Unresolved: 1}, Threads: []gh.ReviewThread{{Path: "main.go", Line: 12, Comments: []gh.ReviewComment{{Body: "fix"}}}}})
	if !strings.Contains(stdout.String(), "summary.unresolved: 1\n") || !strings.Contains(stdout.String(), "threads[1]:\n  - path=main.go line=12 resolved=false outdated=false comments=1\n") {
		t.Fatalf("review threads toon = %q", stdout.String())
	}

	stdout.Reset()
	writeDoctorTOON(&stdout, doctor.Result{SchemaVersion: 1, Kind: "doctor", ReadyState: "ready", Counts: doctor.Counts{OK: 1}, Checks: []doctor.Check{{Name: "git", Status: "ok"}}})
	if !strings.Contains(stdout.String(), "kind: doctor\n") || !strings.Contains(stdout.String(), "checks[1]:\n  - name=git status=ok\n") {
		t.Fatalf("doctor toon = %q", stdout.String())
	}
}

func TestParseFieldsRejectsUnknownField(t *testing.T) {
	_, err := parseFields("number,nope", queueFieldSet())
	if err == nil || !strings.Contains(err.Error(), `unknown field "nope"`) {
		t.Fatalf("err = %v, want unknown field", err)
	}
}

func TestRemovedCommandsReturnUsage(t *testing.T) {
	for _, command := range []string{"lease", "leases", "release", "prune", "complete"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{command}, &stdout, &stderr, "test")
			if code != exitUsage {
				t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitUsage, stdout.String(), stderr.String())
			}
			if stdout.String() != "" {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), `unknown command "`+command+`"`) {
				t.Fatalf("stderr = %q, want unknown command", stderr.String())
			}
		})
	}
}

func TestGlobalHelpUsesCommandHelpCatalog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr, "test"); code != exitOK {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, name := range commandOrder {
		help, ok := commandHelps[name]
		if !ok || !strings.Contains(output, help.Usage) {
			t.Fatalf("command %q missing from help catalog/output", name)
		}
	}
	if strings.Contains(output, "baton complete") {
		t.Fatalf("removed command remains in help:\n%s", output)
	}
	if len(commandOrder) != len(commandHelps) {
		t.Fatalf("command order has %d entries, help catalog has %d", len(commandOrder), len(commandHelps))
	}
}

func TestCommandHelpCatalogCoversSafetyRelevantFlags(t *testing.T) {
	required := map[string][]string{
		"init":          {"profile", "go-install", "install-command"},
		"issue-policy":  {"labels", "repo", "config"},
		"pr-policy":     {"repo", "config"},
		"pr-transition": {"dry-run", "apply", "repo", "config"},
		"sync-labels":   {"dry-run", "apply", "repo", "config", "labels-file"},
		"snapshot":      {"repo", "config", "format", "json"},
		"ensure-branch": {"apply", "config", "remote-base", "remote-target", "local-target", "local-upstream"},
	}
	for command, flags := range required {
		documented := strings.Join(commandHelps[command].Flags, "\n")
		for _, flag := range flags {
			if !strings.Contains(documented, "--"+flag+":") {
				t.Errorf("%s help omits --%s", command, flag)
			}
		}
	}
}

func TestEnsureBranchUsesStagingTerminologyForExistingHistory(t *testing.T) {
	args := []string{
		"ensure-branch", "--remote", "origin", "--base", "main", "--target", "dev",
		"--remote-base", "1111111111111111111111111111111111111111",
		"--remote-target", "2222222222222222222222222222222222222222",
		"--local-target", "2222222222222222222222222222222222222222",
		"--local-upstream", "origin/dev",
	}
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr, "test"); code != exitOK {
		t.Fatalf("exit = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Staging branch plan:") || strings.Contains(stdout.String(), "warning:") {
		t.Fatalf("stdout = %q, want neutral staging-branch status", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
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

func TestMigrateConfigDryRunTruncatesContent(t *testing.T) {
	dir := t.TempDir()
	configPath := writeDefaultConfig(t, dir)
	targetPath := filepath.Join(dir, "out.yml")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"migrate-config", "--from", configPath, "--to", targetPath, "--dry-run", "--body-limit", "8", "--json"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	var result workflow.ConfigMigrationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode config migration: %v\n%s", err, stdout.String())
	}
	if !result.ContentTruncated || result.ContentChars <= len(result.Content) || result.Content != result.ContentPreview {
		t.Fatalf("result = %#v", result)
	}
	if result.FullCommand != "baton migrate-config --dry-run --full --json" {
		t.Fatalf("fullCommand = %q", result.FullCommand)
	}
}

func TestMigrateConfigDryRunFullContent(t *testing.T) {
	dir := t.TempDir()
	configPath := writeDefaultConfig(t, dir)
	targetPath := filepath.Join(dir, "out.yml")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"migrate-config", "--from", configPath, "--to", targetPath, "--dry-run", "--body-limit", "8", "--full", "--json"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	var result workflow.ConfigMigrationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode config migration: %v\n%s", err, stdout.String())
	}
	if result.ContentTruncated || result.ContentPreview != "" || result.FullCommand != "" {
		t.Fatalf("result = %#v", result)
	}
	if result.ContentChars != len([]rune(result.Content)) {
		t.Fatalf("contentChars = %d, content length = %d", result.ContentChars, len([]rune(result.Content)))
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

func TestNextMissingStagingBranchReturnsSetupError(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/issues":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/pulls":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/branches/agent":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GH_TOKEN", "token")
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := writeDefaultConfig(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"next", "--repo", "example-org/example-repo", "--config", configPath, "--json"}, &stdout, &stderr, "test")
	if code != exitConfig {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitConfig, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty structured-mode stderr", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Category != "config" || result.ExitCode != exitConfig {
		t.Fatalf("error result = %+v, want config error", result)
	}
	if !strings.Contains(result.Message, `staging branch "agent" was not found`) {
		t.Fatalf("message = %q, want missing staging branch", result.Message)
	}
	if !strings.Contains(result.Hint, "baton ensure-branch --json") {
		t.Fatalf("hint = %q, want ensure-branch guidance", result.Hint)
	}
}

func TestSnapshotCommandReturnsOneUnifiedObservation(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	server := newSnapshotTestServer(t, false)
	defer server.Close()
	t.Setenv("GH_TOKEN", "token")
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := writeDefaultConfig(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"snapshot", "--repo", "example-org/example-repo", "--config", configPath, "--json"}, &stdout, &stderr, "test")
	if code != exitOK || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		Repository    string `json:"repository"`
		Completeness  string `json:"completeness"`
		Queue         struct {
			Kind string `json:"kind"`
		} `json:"queue"`
		Recommendation struct {
			Outcome string `json:"outcome"`
		} `json:"recommendation"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Kind != "repositorySnapshot" || result.Repository != "example-org/example-repo" || result.Completeness != "complete" || result.Queue.Kind != "queueSnapshot" || result.Recommendation.Outcome != "idle" {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestSnapshotCommandReturnsDegradedFactsAsData(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	server := newSnapshotTestServer(t, true)
	defer server.Close()
	t.Setenv("GH_TOKEN", "token")
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := writeDefaultConfig(t, dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"snapshot", "--repo", "example-org/example-repo", "--config", configPath, "--json"}, &stdout, &stderr, "test")
	if code != exitOK || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var result struct {
		Completeness   string `json:"completeness"`
		Warnings       []any  `json:"warnings"`
		Recommendation struct {
			Outcome string  `json:"outcome"`
			Action  *string `json:"action"`
		} `json:"recommendation"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Completeness != "degraded" || len(result.Warnings) == 0 || result.Recommendation.Outcome != "degraded" || result.Recommendation.Action != nil {
		t.Fatalf("snapshot = %+v", result)
	}
}

func newSnapshotTestServer(t *testing.T, failRules bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example-org/example-repo/issues":
			w.Write([]byte(`[]`))
		case r.URL.Path == "/repos/example-org/example-repo/pulls":
			w.Write([]byte(`[]`))
		case r.URL.Path == "/repos/example-org/example-repo/branches/agent":
			w.Write([]byte(`{"name":"agent","commit":{"sha":"agent-sha"}}`))
		case r.URL.Path == "/repos/example-org/example-repo/branches/main":
			w.Write([]byte(`{"name":"main","commit":{"sha":"main-sha"}}`))
		case strings.Contains(r.URL.Path, "/check-runs"):
			w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
		case strings.Contains(r.URL.Path, "/statuses"):
			w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/rules/branches/") && failRules:
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"message":"temporarily unavailable"}`))
		case strings.Contains(r.URL.Path, "/rules/branches/"):
			w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}

func TestNextRepositoryMismatchReturnsStructuredConfigErrorBeforeGitHubAccess(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	dir := t.TempDir()
	runTestGit(t, dir, "init")
	runTestGit(t, dir, "remote", "add", "origin", "https://github.com/example/local.git")
	configPath := filepath.Join(dir, ".github", "baton.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig(t, filepath.Dir(configPath))
	t.Chdir(dir)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"next", "--repo", "example/other", "--json"}, &stdout, &stderr, "test")
	if code != exitConfig {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitConfig, stdout.String(), stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.SchemaVersion != 1 || result.Category != "config" || result.ExitCode != exitConfig {
		t.Fatalf("error result = %+v", result)
	}
	if !strings.Contains(result.Message, "repository mismatch") || !strings.Contains(result.Message, "example/local") {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestNextRepositoryInspectionFailureReturnsStructuredLocalGitError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("invalid gitfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"next", "--repo", "example/project", "--json"}, &stdout, &stderr, "test")
	if code != exitLocalGit {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitLocalGit, stdout.String(), stderr.String())
	}
	result := decodeErrorResult(t, stdout.String())
	if result.Category != "localGit" || result.ExitCode != exitLocalGit {
		t.Fatalf("error result = %+v", result)
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

func TestApplicationErrorRendererPreservesAllStableCategories(t *testing.T) {
	tests := []struct {
		category apperror.Category
		code     int
	}{
		{apperror.Policy, exitPolicy}, {apperror.Usage, exitUsage}, {apperror.Config, exitConfig},
		{apperror.Auth, exitAuth}, {apperror.GitHub, exitGitHub}, {apperror.LocalGit, exitLocalGit},
	}
	for _, test := range tests {
		t.Run(string(test.category), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := newRenderer(&stdout, &stderr, true).ApplicationError(apperror.New(test.category, "safe message", "safe hint"))
			result := decodeErrorResult(t, stdout.String())
			if code != test.code || result.Category != string(test.category) || result.ExitCode != test.code || result.Message != "safe message" {
				t.Fatalf("code=%d result=%+v", code, result)
			}
			if result.Retryable {
				t.Fatalf("unqualified error must not be retryable: %+v", result)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestApplicationErrorRendererProjectsOnlyTransientFailuresAsRetryable(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*apperror.Error)
	}{
		{name: "typed retryable", mutate: func(err *apperror.Error) { err.Retryable = true }},
		{name: "rate limited", mutate: func(err *apperror.Error) { err.HTTPStatus = http.StatusTooManyRequests }},
		{name: "server failure", mutate: func(err *apperror.Error) { err.HTTPStatus = http.StatusBadGateway }},
		{name: "retry after", mutate: func(err *apperror.Error) { err.RetryAfter = time.Second }},
	} {
		t.Run(test.name, func(t *testing.T) {
			applicationError := apperror.New(apperror.GitHub, "request failed", "")
			test.mutate(applicationError)
			var stdout, stderr bytes.Buffer
			newRenderer(&stdout, &stderr, true).ApplicationError(applicationError)
			if result := decodeErrorResult(t, stdout.String()); !result.Retryable {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestRepositorySnapshotTOONPreservesDecisionEvidence(t *testing.T) {
	var stdout bytes.Buffer
	action := snapshot.ActionIssueImplementation
	writeRepositorySnapshotTOON(&stdout, snapshot.RepositorySnapshot{
		SchemaVersion: 1, Kind: "repositorySnapshot", Repository: "example/repo", Completeness: snapshot.Degraded,
		Acquisition: snapshot.AcquisitionWindow{StartedAt: time.Unix(1, 0).UTC(), CompletedAt: time.Unix(2, 0).UTC()},
		Warnings:    []snapshot.Warning{{Code: "rate_limited", Scope: "issues", Message: "retry later", Retryable: true, HTTPStatus: 429, RequestID: "request-1"}},
		Recommendation: snapshot.Recommendation{
			Outcome: snapshot.OutcomeActionable, Action: &action, Reasons: []string{"eligible_issue"},
			Candidates:         []snapshot.Candidate{{Identity: snapshot.CandidateIdentity{Repository: "example/repo", Kind: snapshot.CandidateIssue, Number: 7}, State: "eligible", Reasons: []string{"ready"}}},
			DeferredCandidates: []snapshot.Candidate{{Identity: snapshot.CandidateIdentity{Repository: "example/repo", Kind: snapshot.CandidateIssue, Number: 8}, State: "deferred"}},
			Instructions:       []string{"Choose exactly one candidate."},
		},
	})
	output := stdout.String()
	for _, expected := range []string{
		"warnings[1]:", "code=rate_limited scope=issues retryable=true httpStatus=429 requestId=request-1 message=retry later",
		"recommendation.reasons[1]:\n  - eligible_issue", "recommendation.candidates[1]:", "reasons=ready",
		"recommendation.deferredCandidates[1]:", "number=8 state=deferred", "recommendation.instructions[1]:\n  - Choose exactly one candidate.",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("snapshot TOON missing %q:\n%s", expected, output)
		}
	}
}

func TestApplicationErrorRendererIncludesOperationReportInSingleErrorObject(t *testing.T) {
	report := operation.NewReport([]operation.Result{
		{ID: "local", Resource: "record", Action: "write", Status: operation.StatusApplied},
		{ID: "remote", Resource: "issue", Action: "comment", Status: operation.StatusFailed},
	})
	applicationError := apperror.New(apperror.GitHub, "comment failed", "retry")
	applicationError.Report = &report

	var stdout, stderr bytes.Buffer
	code := newRenderer(&stdout, &stderr, true).ApplicationError(applicationError)
	result := decodeErrorResult(t, stdout.String())
	if code != exitGitHub || result.Report == nil || result.Report.Status != operation.ReportPartial || len(result.Report.Operations) != 2 {
		t.Fatalf("code=%d result=%+v", code, result)
	}
	decoder := json.NewDecoder(strings.NewReader(stdout.String()))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	if decoder.Decode(&object) != io.EOF {
		t.Fatalf("structured failure emitted more than one JSON object: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestIssuePolicyOutputPreservesDecisionShapeAndAddsReport(t *testing.T) {
	report := operation.NewReport([]operation.Result{{ID: "one", Resource: "issue", Action: "label", Status: operation.StatusApplied}})
	output := issuePolicyOutput{
		IssuePolicyDecision: policy.IssuePolicyDecision{SchemaVersion: 1, Kind: "issuePolicyDecision", IsFormIssue: true, LabelsToAdd: []string{}, LabelsToRemove: []string{}, MissingRequiredSections: []string{}},
		Report:              &report,
	}
	content, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["kind"] != "issuePolicyDecision" || decoded["report"] == nil || decoded["Decision"] != nil {
		t.Fatalf("output = %s", content)
	}
}

func TestTOONApplicationErrorIncludesOperationReport(t *testing.T) {
	report := operation.NewReport([]operation.Result{{ID: "remote", Resource: "issue", Action: "comment", Status: operation.StatusFailed}})
	applicationError := apperror.New(apperror.GitHub, "comment failed", "retry")
	applicationError.Report = &report

	var stdout, stderr bytes.Buffer
	code := newFormatRenderer(&stdout, &stderr, formatTOON).ApplicationError(applicationError)
	if code != exitGitHub || !strings.Contains(stdout.String(), "report:\n") || !strings.Contains(stdout.String(), "status: failed") || !strings.Contains(stdout.String(), "- id: remote") {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
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

func runTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
}
