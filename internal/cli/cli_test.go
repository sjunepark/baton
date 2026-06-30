package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/doctor"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/queue"
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
	if strings.Contains(output, "lease") {
		t.Fatalf("home toon = %q, want no lease fields", output)
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
		Counts:        queue.SnapshotCounts{TotalIssues: 1, EligibleIssues: 1, OpenPullRequests: 0},
		Issues: []queue.IssueState{{
			Issue:    queue.Issue{Number: 7, Title: "Fix flaky test"},
			Eligible: true,
			Action:   "issue-implementation",
			Reasons:  []string{"eligible"},
		}},
		Help: []string{"Run `baton next --format toon`."},
	}
	writeQueueTOON(&stdout, snapshot)
	wantQueue := "kind: queueSnapshot\nschemaVersion: 1\nrepo: example/repo\ncounts.totalIssues: 1\ncounts.eligibleIssues: 1\ncounts.skippedIssues: 0\ncounts.openPullRequests: 0\nissues[1]:\n  - number=7 eligible=true action=issue-implementation title=Fix flaky test reasons=eligible\nhelp[1]:\n  - Run `baton next --format toon`.\n"
	if stdout.String() != wantQueue {
		t.Fatalf("queue toon = %q\nwant %q", stdout.String(), wantQueue)
	}

	stdout.Reset()
	writePRsTOON(&stdout, buildPullRequestsResult("example/repo", []queue.PullState{{PullRequest: queue.PullRequest{Number: 8, Title: "Update docs", HeadRef: "agent-work/8", BaseRef: "agent", CheckState: "success"}}}))
	if !strings.Contains(stdout.String(), "kind: pullRequests\n") || !strings.Contains(stdout.String(), "pullRequests[1]:\n  - number=8 headRef=agent-work/8 baseRef=agent checkState=success title=Update docs\n") {
		t.Fatalf("prs toon = %q", stdout.String())
	}

	stdout.Reset()
	writeQueueTOONFields(&stdout, snapshot, []string{"number", "title", "action", "reasons"})
	if !strings.Contains(stdout.String(), "issues[1]:\n  - number=7 title=Fix flaky test action=issue-implementation reasons=eligible\n") {
		t.Fatalf("queue fields toon = %q", stdout.String())
	}

	stdout.Reset()
	writePRsTOONFields(&stdout, buildPullRequestsResult("example/repo", []queue.PullState{{PullRequest: queue.PullRequest{Number: 8, Title: "Update docs", HeadRef: "agent-work/8", CheckState: "success"}}}), []string{"number", "title", "headRef", "checkState"})
	if !strings.Contains(stdout.String(), "pullRequests[1]:\n  - number=8 title=Update docs headRef=agent-work/8 checkState=success\n") {
		t.Fatalf("prs fields toon = %q", stdout.String())
	}

	stdout.Reset()
	writeNextTOON(&stdout, queue.NextCandidates{
		SchemaVersion:     2,
		Kind:              "nextCandidates",
		Repo:              "example/repo",
		Action:            "issue-implementation",
		Reason:            "eligible-issue",
		SelectionRequired: true,
		Candidates: []queue.NextCandidate{
			{Type: "issue", Number: 7, Title: "Fix flaky test"},
			{Type: "issue", Number: 9, Title: "Update docs"},
		},
		Instructions: []string{"Choose exactly one candidate."},
	})
	if !strings.Contains(stdout.String(), "kind: nextCandidates\n") ||
		!strings.Contains(stdout.String(), "selectionRequired: true\n") ||
		!strings.Contains(stdout.String(), "candidates[2]:\n  - type=issue number=7 title=Fix flaky test\n  - type=issue number=9 title=Update docs\n") ||
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

func TestPullRequestDashboardSummaries(t *testing.T) {
	pr := queue.PullRequest{Number: 42, Title: "Refs #7", Body: "Also refs #8 and refs #7", HeadRef: "agent/42", BaseRef: "agent", CheckState: "pending"}
	result := buildPullRequestDashboard("example/repo", pr)
	result.Checks = pullRequestCheckSummary{State: "failure", Count: 2, Summary: gh.CheckSummary{Failed: 1, Pending: 1}}
	result.ReviewThreads = pullRequestReviewSummary{Count: 3, Summary: gh.ThreadSummary{Total: 3, Unresolved: 2, HumanUnresolved: 1, BotUnresolved: 1}}
	result.LikelyNextCommand = pullRequestLikelyNextCommand(pr.Number, result.Checks.Summary, result.ReviewThreads.Summary, true, true)
	result.Help = pullRequestDashboardHelp(pr.Number, result.Checks.Summary, result.ReviewThreads.Summary)

	if result.Kind != "pullRequest" || result.Repo != "example/repo" {
		t.Fatalf("dashboard identity = %#v", result)
	}
	if got := intList(result.ReferencedIssues); got != "7|8" {
		t.Fatalf("referenced issues = %s, want 7|8", got)
	}
	if result.LikelyNextCommand != "baton review-threads 42 --format toon" {
		t.Fatalf("likely next = %q", result.LikelyNextCommand)
	}
	if !strings.Contains(strings.Join(result.Help, "\n"), "unresolved human comments") {
		t.Fatalf("help = %#v, want human-review guidance", result.Help)
	}
}

func TestPullRequestLikelyNextCommandForUnavailableSummaries(t *testing.T) {
	if got := pullRequestLikelyNextCommand(42, gh.CheckSummary{}, gh.ThreadSummary{}, false, true); got != "baton checks 42 --format toon" {
		t.Fatalf("missing checks next = %q", got)
	}
	if got := pullRequestLikelyNextCommand(42, gh.CheckSummary{}, gh.ThreadSummary{}, true, false); got != "baton review-threads 42 --format toon" {
		t.Fatalf("missing reviews next = %q", got)
	}
	if got := pullRequestLikelyNextCommand(42, gh.CheckSummary{}, gh.ThreadSummary{}, true, true); got != "baton next --format toon" {
		t.Fatalf("complete summaries next = %q", got)
	}
}

func TestBuildIssueReadinessUsesLabels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.IssuePolicy.ImplementationLabels = []string{"agent:ready-trivial"}
	cfg.IssuePolicy.CommentOnlyLabels = []string{"agent:needs-investigation"}
	cfg.IssuePolicy.SkipLabels = []string{"needs-info"}
	readiness := buildIssueReadiness([]int{7, 8, 9}, []queue.Issue{
		{Number: 7, Labels: []string{"agent:ready-trivial"}},
		{Number: 8, Labels: []string{"agent:ready-trivial", "needs-info"}},
	}, cfg)

	if len(readiness) != 3 {
		t.Fatalf("readiness len = %d", len(readiness))
	}
	if !readiness[0].Ready || readiness[0].Action != "issue-implementation" {
		t.Fatalf("ready issue = %#v", readiness[0])
	}
	if readiness[1].Ready || !strings.Contains(strings.Join(readiness[1].Reasons, ","), "skip label needs-info") {
		t.Fatalf("blocked issue = %#v", readiness[1])
	}
	if readiness[2].Found || readiness[2].Ready {
		t.Fatalf("missing issue = %#v", readiness[2])
	}
}

func TestRemovedLeaseCommandsReturnUsage(t *testing.T) {
	for _, command := range []string{"lease", "leases", "release", "prune"} {
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

func TestMigrateConfigDryRunTruncatesContent(t *testing.T) {
	dir := t.TempDir()
	configPath := writeDefaultConfig(t, dir)
	targetPath := filepath.Join(dir, "out.yml")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"migrate-config", "--from", configPath, "--to", targetPath, "--dry-run", "--body-limit", "8", "--json"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	var result configMigrationResult
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
	var result configMigrationResult
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

func TestCompleteJSONTruncatesOutputButPersistsFullRecord(t *testing.T) {
	dir := t.TempDir()
	summary := "abcdef"
	validation := "uvwxyz"

	var stdout, stderr bytes.Buffer
	code := Run([]string{"complete", "--summary", summary, "--validation", validation, "--state-root", dir, "--body-limit", "3", "--json"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	var result completionResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode completion: %v\n%s", err, stdout.String())
	}
	if result.Summary != "abc" || !result.SummaryTruncated || result.SummaryChars != 6 {
		t.Fatalf("summary result = %#v", result)
	}
	if result.Validation != "uvw" || !result.ValidationTruncated || result.ValidationChars != 6 {
		t.Fatalf("validation result = %#v", result)
	}

	content, err := os.ReadFile(filepath.Join(dir, "completions", result.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), summary) || !strings.Contains(string(content), validation) {
		t.Fatalf("persisted record = %s, want full summary and validation", content)
	}
}

func TestCompleteJSONFullOutput(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"complete", "--summary", "abcdef", "--state-root", dir, "--body-limit", "3", "--full", "--json"}, &stdout, &stderr, "test")
	if code != exitOK {
		t.Fatalf("Run exit = %d, want %d; stdout=%s stderr=%s", code, exitOK, stdout.String(), stderr.String())
	}
	var result completionResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode completion: %v\n%s", err, stdout.String())
	}
	if result.Summary != "abcdef" || result.SummaryTruncated || result.SummaryPreview != "" {
		t.Fatalf("result = %#v", result)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/issues":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/pulls":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example-org/example-repo/git/ref/heads/agent":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	t.Setenv("GH_TOKEN", "token")
	t.Setenv("GITHUB_API_URL", server.URL)
	dir := t.TempDir()
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
