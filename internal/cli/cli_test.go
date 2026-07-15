package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/task"
)

func TestNoArgumentsAndHelpPerformNoSetup(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--help"}, {"list", "--help"}} {
		calls := 0
		rt := runtime{
			getenv: func(string) string { calls++; return "" },
			resolve: func(context.Context, repository.TaskOptions) (string, error) {
				calls++
				return "", errors.New("unexpected")
			},
			newService: func(context.Context) (*task.Service, error) { calls++; return nil, errors.New("unexpected") },
		}
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), args, stdout, stderr, "v0.7.0", rt); code != exitOK || calls != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Usage:") {
			t.Fatalf("args %v: code %d calls %d stdout %q stderr %q", args, code, calls, stdout, stderr)
		}
	}
}

func TestVersionHasOneSpellingAndNoSetup(t *testing.T) {
	t.Parallel()
	rt := runtime{getenv: func(string) string { t.Fatal("unexpected env"); return "" }}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := runContext(context.Background(), []string{"--version"}, stdout, stderr, "v0.7.0", rt); code != exitOK || stdout.String() != "v0.7.0\n" {
		t.Fatalf("--version: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runContext(context.Background(), []string{"version"}, stdout, stderr, "v0.7.0", rt); code != exitUsage || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("version: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestInvalidInputFailsBeforeRepositoryOrAuth(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"--repo", "--json", "list"},
		{"--repo", "example/repo", "list", "--config", "x"},
		{"--repo", "example/repo", "list", "--fields", "number"},
		{"--repo", "example/repo", "list", "--format", "toon"},
		{"--repo", "example/repo", "update", "1"},
		{"--repo", "example/repo", "update", "1", "--add-blocker", "needs-info", "--remove-blocker", "NEEDS-INFO"},
		{"--repo", "example/repo", "enroll", "0"},
		{"--repo", "example/repo", "enroll", "1", "--mode", "automatic"},
	}
	for _, args := range tests {
		calls := 0
		rt := runtime{
			getenv: func(string) string { calls++; return "" },
			resolve: func(context.Context, repository.TaskOptions) (string, error) {
				calls++
				return "", errors.New("unexpected")
			},
			newService: func(context.Context) (*task.Service, error) { calls++; return nil, errors.New("unexpected") },
		}
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), args, stdout, stderr, "dev", rt); code != exitUsage || calls != 0 || stderr.Len() == 0 {
			t.Fatalf("args %v: code %d calls %d stdout %q stderr %q", args, code, calls, stdout, stderr)
		}
	}
}

func TestInvalidExplicitRepositoryIsUsageBeforeAuth(t *testing.T) {
	t.Parallel()
	serviceCalls := 0
	rt := runtime{
		getenv:  func(string) string { return "" },
		resolve: repository.ResolveTaskRepositoryContext,
		newService: func(context.Context) (*task.Service, error) {
			serviceCalls++
			return nil, errors.New("unexpected")
		},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runContext(context.Background(), []string{"--repo", "not-a-repository", "list"}, stdout, stderr, "dev", rt)
	if code != exitUsage || serviceCalls != 0 || !strings.Contains(stderr.String(), "owner/name") {
		t.Fatalf("code %d service calls %d stdout %q stderr %q", code, serviceCalls, stdout, stderr)
	}
}

func TestEveryRetainedCommandHasHelpAndRemovedCommandsAreUnknown(t *testing.T) {
	t.Parallel()
	for _, command := range commandOrder {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), []string{command, "--help"}, stdout, stderr, "dev", runtime{}); code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), commandHelp[command].usage) {
			t.Fatalf("%s help: code %d stdout %q stderr %q", command, code, stdout, stderr)
		}
	}
	for _, command := range []string{"home", "doctor", "queue", "snapshot", "pr", "checks", "version"} {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), []string{command}, stdout, stderr, "dev", runtime{}); code != exitUsage || !strings.Contains(stderr.String(), "unknown command") {
			t.Fatalf("%s: code %d stdout %q stderr %q", command, code, stdout, stderr)
		}
	}
}

func TestJSONListShowAndNextContracts(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 2, Title: "Ready task", URL: "https://example.test/2", Body: "full body", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial", "bug"}})
	store.PutIssue("example/repo", task.Issue{Number: 3, Title: "Blocked task", URL: "https://example.test/3", State: task.IssueOpen, Labels: []string{task.LabelManaged}})
	rt := testRuntime(store)

	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "list"}, `{"repository":"example/repo","tasks":[{"number":2,"title":"Ready task","url":"https://example.test/2","issueState":"open","state":"ready","mode":"trivial","priority":"p2","inProgress":false,"blockers":[],"projectLabels":["bug"],"reasons":[]},{"number":3,"title":"Blocked task","url":"https://example.test/3","issueState":"open","state":"blocked","mode":null,"priority":"p2","inProgress":false,"blockers":[],"projectLabels":[],"reasons":["missing_mode"]}]}`)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "show", "2"}, `{"repository":"example/repo","task":{"number":2,"title":"Ready task","url":"https://example.test/2","issueState":"open","state":"ready","mode":"trivial","priority":"p2","inProgress":false,"blockers":[],"projectLabels":["bug"],"reasons":[],"body":"full body","bodyTruncated":false}}`)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "next"}, `{"repository":"example/repo","task":{"number":2,"title":"Ready task","url":"https://example.test/2","issueState":"open","state":"ready","mode":"trivial","priority":"p2","inProgress":false,"blockers":[],"projectLabels":["bug"],"reasons":[]}}`)
}

func TestJSONDefinitiveEmptyStates(t *testing.T) {
	t.Parallel()
	rt := testRuntime(task.NewMemoryStore())
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "list"}, `{"repository":"example/repo","tasks":[]}`)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "next"}, `{"repository":"example/repo","task":null}`)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := runContext(context.Background(), []string{"--repo", "example/repo", "list"}, stdout, stderr, "dev", rt); code != exitOK || stdout.String() != "No tasks.\n" || stderr.Len() != 0 {
		t.Fatalf("text empty: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestJSONMutationAndIdempotentNoOpContracts(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 7, Title: "Enroll me", URL: "https://example.test/7", State: task.IssueOpen})
	rt := testRuntime(store)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "enroll", "7", "--mode", "bounded", "--dry-run"}, `{"repository":"example/repo","changed":true,"dryRun":true,"changes":[{"action":"add_label","label":"agent:ready-bounded"},{"action":"add_label","label":"baton:managed"}],"task":{"number":7,"title":"Enroll me","url":"https://example.test/7","issueState":"open","state":"ready","mode":"bounded","priority":"p2","inProgress":false,"blockers":[],"projectLabels":[],"reasons":[]}}`)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "enroll", "7", "--mode", "bounded"}, `{"repository":"example/repo","changed":true,"dryRun":false,"changes":[{"action":"add_label","label":"agent:ready-bounded"},{"action":"add_label","label":"baton:managed"}],"task":{"number":7,"title":"Enroll me","url":"https://example.test/7","issueState":"open","state":"ready","mode":"bounded","priority":"p2","inProgress":false,"blockers":[],"projectLabels":[],"reasons":[]}}`)
	assertJSONGolden(t, rt, []string{"--repo", "example/repo", "--json", "enroll", "7", "--mode", "bounded"}, `{"repository":"example/repo","changed":false,"dryRun":false,"changes":[],"task":{"number":7,"title":"Enroll me","url":"https://example.test/7","issueState":"open","state":"ready","mode":"bounded","priority":"p2","inProgress":false,"blockers":[],"projectLabels":[],"reasons":[]}}`)
}

func TestEveryMutationCommandExecutesItsFixedFlags(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 8, Title: "Lifecycle", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	rt := testRuntime(store)
	commands := [][]string{
		{"--repo", "example/repo", "--json", "update", "8", "--mode", "investigate", "--priority", "p0", "--add-blocker", "needs-info"},
		{"--repo", "example/repo", "--json", "update", "8", "--remove-blocker", "needs-info", "--priority", "none"},
		{"--repo", "example/repo", "--json", "start", "8"},
		{"--repo", "example/repo", "--json", "stop", "8"},
		{"--repo", "example/repo", "--json", "close", "8", "--dry-run"},
		{"--repo", "example/repo", "--json", "close", "8"},
		{"--repo", "example/repo", "--json", "unenroll", "8"},
	}
	for _, args := range commands {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), args, stdout, stderr, "dev", rt); code != exitOK || stderr.Len() != 0 {
			t.Fatalf("args %v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
	issue, err := store.GetIssue(context.Background(), "example/repo", 8)
	if err != nil {
		t.Fatal(err)
	}
	if issue.State != task.IssueClosed || reflect.DeepEqual(issue.Labels, []string{}) {
		t.Fatalf("final issue = %#v", issue)
	}
	for _, label := range issue.Labels {
		if label == task.LabelManaged || label == task.LabelInProgress {
			t.Fatalf("unenroll left Baton ownership/activity label: %v", issue.Labels)
		}
	}
}

func TestJSONUsageAndOperationalErrorsUseThreeExitContract(t *testing.T) {
	t.Parallel()
	rt := testRuntime(task.NewMemoryStore())
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runContext(context.Background(), []string{"--json", "--repo", "example/repo", "list", "--fields", "number"}, stdout, stderr, "dev", rt)
	if code != exitUsage || stdout.Len() != 0 {
		t.Fatalf("usage: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	assertJSONValue(t, stderr.String(), map[string]any{"error": map[string]any{
		"code": "invalid_usage", "message": "unknown flag --fields", "hint": "Run `baton list --help` for valid syntax.",
	}})

	stdout.Reset()
	stderr.Reset()
	code = runContext(context.Background(), []string{"--json", "--repo", "example/repo", "show", "99"}, stdout, stderr, "dev", rt)
	if code != exitOperational || stdout.Len() != 0 {
		t.Fatalf("operational: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	assertJSONValue(t, stderr.String(), map[string]any{"error": map[string]any{
		"code": "operation_failed", "message": "command failed", "hint": "Retry the command.",
	}})
}

func TestJSONTaskAndPartialMutationErrorContracts(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 4, Title: "Plain issue", URL: "https://example.test/4", State: task.IssueOpen})
	rt := testRuntime(store)
	assertErrorJSONGolden(t, rt, []string{"--json", "--repo", "example/repo", "show", "4"}, exitOperational, `{"error":{"code":"not_managed","message":"issue #4 is not managed by Baton","hint":"Run baton enroll 4 to enroll it."}}`)

	store.FailAction = "add_label"
	store.FailLabel = "agent:ready-bounded"
	assertErrorJSONGolden(t, rt, []string{"--json", "--repo", "example/repo", "enroll", "4", "--mode", "bounded"}, exitOperational, `{"error":{"code":"mutation_failed","message":"Task mutation for issue #4 failed","hint":"Inspect the confirmed changes and current task, then retry the command.","changes":[{"action":"create_label","label":"agent:ready-bounded"}]}}`)

	closedStore := task.NewMemoryStore()
	closedStore.PutIssue("example/repo", task.Issue{Number: 5, Title: "Closed", State: task.IssueClosed, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	assertErrorJSONGolden(t, testRuntime(closedStore), []string{"--json", "--repo", "example/repo", "start", "5"}, exitOperational, `{"error":{"code":"invalid_transition","message":"closed task #5 cannot be started","hint":"Choose an open task."}}`)
}

func TestGlobalRepositoryAndJSONFlagsMustPrecedeCommand(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"list", "--repo", "example/repo"}, {"list", "--json"}} {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		if code := runContext(context.Background(), args, stdout, stderr, "dev", testRuntime(task.NewMemoryStore())); code != exitUsage {
			t.Fatalf("args %v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestCLIRepositorySelectionUsesExplicitAmbientThenLocal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		args          []string
		ambient       string
		remote        string
		wantRepo      string
		wantRemoteUse int
	}{
		{name: "explicit", args: []string{"--repo", "explicit/repo", "list"}, ambient: "ambient/repo", remote: "git@github.com:local/repo.git", wantRepo: "explicit/repo"},
		{name: "ambient", args: []string{"list"}, ambient: "ambient/repo", remote: "git@github.com:local/repo.git", wantRepo: "ambient/repo"},
		{name: "local", args: []string{"list"}, remote: "git@github.com:local/repo.git", wantRepo: "local/repo", wantRemoteUse: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			remoteReads := 0
			resolved := ""
			rt := runtime{
				getenv: func(name string) string {
					if name == "GITHUB_REPOSITORY" {
						return test.ambient
					}
					return ""
				},
				resolve: func(ctx context.Context, options repository.TaskOptions) (string, error) {
					options.ReadRemote = func(context.Context, string) (string, error) {
						remoteReads++
						return test.remote, nil
					}
					value, err := repository.ResolveTaskRepositoryContext(ctx, options)
					resolved = value
					return value, err
				},
				newService: func(context.Context) (*task.Service, error) { return task.NewService(task.NewMemoryStore()), nil },
			}
			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			if code := runContext(context.Background(), test.args, stdout, stderr, "dev", rt); code != exitOK || resolved != test.wantRepo || remoteReads != test.wantRemoteUse {
				t.Fatalf("code %d resolved %q remote reads %d stdout %q stderr %q", code, resolved, remoteReads, stdout, stderr)
			}
		})
	}
}

func TestTextPartialMutationErrorIncludesConfirmedState(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 10, Title: "Partial", State: task.IssueOpen})
	store.FailAction = "add_label"
	store.FailLabel = "agent:ready-bounded"
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runContext(context.Background(), []string{"--repo", "example/repo", "enroll", "10", "--mode", "bounded"}, stdout, stderr, "dev", testRuntime(store))
	if code != exitOperational || stdout.Len() != 0 || !strings.Contains(stderr.String(), "confirmed changes:") || strings.Contains(stderr.String(), "current task:") {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestProductionHTTPClientHasFiniteDeadlines(t *testing.T) {
	t.Parallel()
	client := newProductionHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || client.Timeout != githubRequestTimeout || transport.ResponseHeaderTimeout != githubResponseHeaderTimeout {
		t.Fatalf("production HTTP client = %#v transport %#v", client, client.Transport)
	}
}

func TestTextOutputFailuresReturnOperationalExit(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue("example/repo", task.Issue{Number: 1, Title: "Task", URL: "https://example.test/1", Body: "body", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	rt := testRuntime(store)
	tests := [][]string{
		nil,
		{"--version"},
		{"list", "--help"},
		{"--repo", "example/repo", "list"},
		{"--repo", "example/repo", "show", "1"},
		{"--repo", "example/repo", "next"},
		{"--repo", "example/repo", "start", "1", "--dry-run"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stderr := &bytes.Buffer{}
			code := runContext(context.Background(), args, errorWriter{}, stderr, "dev", rt)
			if code != exitOperational || !strings.Contains(stderr.String(), "error: write output failed") {
				t.Fatalf("args %v: code %d stderr %q", args, code, stderr)
			}
		})
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }

func testRuntime(store *task.MemoryStore) runtime {
	service := task.NewService(store)
	return runtime{
		getenv: func(string) string { return "" },
		resolve: func(_ context.Context, options repository.TaskOptions) (string, error) {
			if options.Repository == "" {
				return "", &repository.TaskResolveError{Code: "missing_repository", Message: "missing", Hint: "pass --repo"}
			}
			return options.Repository, nil
		},
		newService: func(context.Context) (*task.Service, error) { return service, nil },
	}
}

func assertJSONGolden(t *testing.T, rt runtime, args []string, want string) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := runContext(context.Background(), args, stdout, stderr, "dev", rt); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("args %v: code %d stdout %q stderr %q", args, code, stdout, stderr)
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("args %v JSON mismatch\n got: %s\nwant: %s", args, got, want)
	}
}

func assertErrorJSONGolden(t *testing.T, rt runtime, args []string, wantCode int, want string) {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := runContext(context.Background(), args, stdout, stderr, "dev", rt); code != wantCode || stdout.Len() != 0 {
		t.Fatalf("args %v: code %d stdout %q stderr %q", args, code, stdout, stderr)
	}
	if got := strings.TrimSpace(stderr.String()); got != want {
		t.Fatalf("args %v JSON mismatch\n got: %s\nwant: %s", args, got, want)
	}
}

func assertJSONValue(t *testing.T, got string, want map[string]any) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(got), &value); err != nil {
		t.Fatalf("decode JSON %q: %v", got, err)
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("JSON = %#v, want %#v", value, want)
	}
}
