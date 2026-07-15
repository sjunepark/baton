package doctor

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/gh"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/labels"
)

func TestDoctorCountsAndReadyState(t *testing.T) {
	tests := []struct {
		name       string
		checks     []Check
		wantCounts Counts
		wantState  string
	}{
		{
			name: "failures_block_ready_state",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "warn"},
				{Name: "repo-root", Status: "fail"},
			},
			wantCounts: Counts{OK: 1, Warn: 1, Fail: 1},
			wantState:  "blocked",
		},
		{
			name: "warnings_degrade_ready_state",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "warn"},
			},
			wantCounts: Counts{OK: 1, Warn: 1},
			wantState:  "degraded",
		},
		{
			name: "all_ok_is_ready",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "ok"},
			},
			wantCounts: Counts{OK: 2},
			wantState:  "ready",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := countChecks(tt.checks)
			if counts != tt.wantCounts {
				t.Fatalf("counts = %#v, want %#v", counts, tt.wantCounts)
			}
			if state := readyState(counts); state != tt.wantState {
				t.Fatalf("readyState = %q, want %q", state, tt.wantState)
			}
		})
	}
}

func TestDoctorDoesNotTreatTokenDiscoveryAsAuthenticatedRepositoryAccess(t *testing.T) {
	work := setupDoctorGitRepo(t, true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	defer server.Close()

	result := RunWithOptions(Options{WorkingDir: work, Repository: "example/repo", GitHubAPIURL: server.URL, GitHubToken: "discovered-but-insufficient"})
	check := findCheck(t, result.Checks, "github-auth")
	if check.Status != "fail" || !strings.Contains(check.Message, "authenticated repository read failed") {
		t.Fatalf("github-auth = %+v", check)
	}
	if result.ReadyState != "blocked" {
		t.Fatalf("readyState = %q, want blocked", result.ReadyState)
	}
	for _, candidate := range result.Checks {
		if candidate.Name == "workflows-enabled" || candidate.Name == "delivery-readiness" {
			t.Fatalf("repository auth failure produced cascading compatibility check %+v", candidate)
		}
	}
}

func TestDoctorAcquiresCompleteLiveCompatibilityFacts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Delivery = &config.DeliveryConfig{
		Authority: config.DeliveryAuthorityShadow, Host: "github.com",
		Repository: config.DeliveryRepository{FullName: "example/repo", NodeID: "R_1"},
		Issue:      config.DeliveryResource{Number: 900, NodeID: "I_900"},
		Checkpoint: config.DeliveryComment{DatabaseID: 901, NodeID: "IC_901"},
	}
	desired, err := install.RenderManagedFiles(cfg, install.Options{})
	if err != nil {
		t.Fatal(err)
	}
	contentByPath := map[string][]byte{}
	var expectedLabels []labels.Label
	for _, file := range desired {
		contentByPath[file.Path] = file.Content
		if file.Path == cfg.Labels.Manifest {
			manifest, parseErr := labels.ParseManifest(file.Content)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			expectedLabels = manifest.Labels
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo":
			writeDoctorJSON(t, w, map[string]any{
				"node_id": "R_1", "full_name": "example/repo", "html_url": "https://github.com/example/repo", "default_branch": "main", "visibility": "public",
				"allow_merge_commit": true, "allow_squash_merge": true, "allow_rebase_merge": true, "owner": map[string]any{"type": "User"},
			})
		case r.URL.Path == "/apps/github-actions":
			writeDoctorJSON(t, w, map[string]any{"id": 15368, "slug": "github-actions"})
		case r.URL.Path == "/repos/example/repo/branches/main":
			writeDoctorJSON(t, w, map[string]any{"name": "main", "commit": map[string]any{"sha": "base-sha"}})
		case r.URL.Path == "/repos/example/repo/branches/agent":
			writeDoctorJSON(t, w, map[string]any{"name": "agent", "commit": map[string]any{"sha": "staging-sha"}})
		case strings.HasPrefix(r.URL.Path, "/repos/example/repo/contents/"):
			path := strings.TrimPrefix(r.URL.Path, "/repos/example/repo/contents/")
			content, found := contentByPath[path]
			if !found || r.URL.Query().Get("ref") != "base-sha" {
				t.Fatalf("unexpected content request %s", r.URL.String())
			}
			writeDoctorJSON(t, w, map[string]any{"path": path, "sha": "file-sha", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)})
		case strings.HasPrefix(r.URL.Path, "/repos/example/repo/actions/workflows/"):
			filename := strings.TrimPrefix(r.URL.Path, "/repos/example/repo/actions/workflows/")
			path := ".github/workflows/" + filename
			if _, found := contentByPath[path]; !found {
				t.Fatalf("unexpected workflow request %s", r.URL.Path)
			}
			writeDoctorJSON(t, w, map[string]any{"name": filename, "path": path, "state": "active"})
		case r.URL.Path == "/repos/example/repo/labels":
			writeDoctorJSON(t, w, expectedLabels)
		case strings.HasPrefix(r.URL.Path, "/repos/example/repo/rules/branches/"):
			writeDoctorJSON(t, w, []map[string]any{{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": []map[string]any{{"context": requiredPolicyCheck, "integration_id": 15368}}}}})
		case strings.HasSuffix(r.URL.Path, "/protection"):
			w.WriteHeader(http.StatusNotFound)
			writeDoctorJSON(t, w, map[string]any{"message": "Branch not protected"})
		case r.URL.Path == "/repos/example/repo/actions/permissions":
			writeDoctorJSON(t, w, map[string]any{"enabled": true, "allowed_actions": "all", "sha_pinning_required": false})
		case r.URL.Path == "/repos/example/repo/issues":
			writeDoctorJSON(t, w, []any{})
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()

	work := setupDoctorManagedGitRepo(t, desired)
	result := RunWithOptions(Options{WorkingDir: work, Repository: "example/repo", GitHubAPIURL: server.URL, GitHubToken: "token"})
	if result.SchemaVersion != 2 || result.ReadyState != "degraded" || result.Counts.Fail != 0 {
		t.Fatalf("doctor result = %+v", result)
	}
	for _, name := range []string{"github-auth", "github-repository", "managed-files", "workflows-enabled", "actions-policy", "base-required-check", "staging-required-check", "merge-queue"} {
		if check := findCheck(t, result.Checks, name); check.Status != "ok" {
			t.Fatalf("check %q = %+v", name, check)
		}
	}
	if check := findCheck(t, result.Checks, "delivery-readiness"); check.Status != "warn" {
		t.Fatalf("delivery-readiness = %+v", check)
	}
}

func TestSynchronizationCompatibilityRejectsLinearAndSquashOnlyRepositories(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings gh.RepositorySettings
		rules    gh.BranchRules
	}{
		{name: "linear history", settings: gh.RepositorySettings{AllowMergeCommit: true}, rules: gh.BranchRules{RequiredLinearHistory: true}},
		{name: "squash rebase only", settings: gh.RepositorySettings{AllowSquashMerge: true, AllowRebaseMerge: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if check := SynchronizationCompatibilityCheck(test.settings, test.rules); check.Status != "fail" {
				t.Fatalf("check = %+v", check)
			}
		})
	}
	if check := SynchronizationCompatibilityCheck(gh.RepositorySettings{AllowMergeCommit: true}, gh.BranchRules{}); check.Status != "ok" {
		t.Fatalf("compatible check = %+v", check)
	}
}

func TestDoctorWarnsWhenStagingBranchSetupIsNeeded(t *testing.T) {
	work := setupDoctorGitRepo(t, true)

	result := RunWithOptions(Options{WorkingDir: work, GitHubToken: "token"})
	check := findCheck(t, result.Checks, "staging-branch")
	if check.Status != "warn" {
		t.Fatalf("staging-branch check = %#v, want warn", check)
	}
	if !strings.Contains(check.Message, "ensure-branch") {
		t.Fatalf("staging-branch message = %q, want ensure-branch guidance", check.Message)
	}
	if !strings.Contains(strings.Join(result.Help, "\n"), "baton ensure-branch --json") {
		t.Fatalf("help = %#v, want ensure-branch guidance", result.Help)
	}
}

func TestDoctorAcceptsStagingBranchWithUnpromotedWork(t *testing.T) {
	work := setupDoctorGitRepo(t, true)
	runDoctorTestGit(t, work, "switch", "-c", "agent")
	if err := os.WriteFile(filepath.Join(work, "STAGED.md"), []byte("unpromoted work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDoctorTestGit(t, work, "add", "STAGED.md")
	runDoctorTestGit(t, work, "commit", "-m", "docs: add staged work")
	runDoctorTestGit(t, work, "push", "-u", "origin", "agent")

	result := RunWithOptions(Options{WorkingDir: work, GitHubToken: "token"})
	check := findCheck(t, result.Checks, "staging-branch")
	if check.Status != "ok" {
		t.Fatalf("staging-branch check = %#v, want ok", check)
	}
	if result.ReadyState != "blocked" {
		t.Fatalf("readyState = %q, want blocked until live GitHub compatibility is proved", result.ReadyState)
	}
}

func TestDoctorWarnsWhenLocalBaseTrackingRefIsMissing(t *testing.T) {
	work := setupDoctorGitRepo(t, false)

	result := RunWithOptions(Options{WorkingDir: work, GitHubToken: "token"})
	check := findCheck(t, result.Checks, "staging-branch")
	if check.Status != "warn" {
		t.Fatalf("staging-branch check = %#v, want warn", check)
	}
	if !strings.Contains(check.Message, "origin/main was not found") {
		t.Fatalf("staging-branch message = %q, want missing base branch", check.Message)
	}
	if result.ReadyState != "blocked" {
		t.Fatalf("readyState = %q, want blocked", result.ReadyState)
	}
}

func setupDoctorGitRepo(t *testing.T, pushMain bool) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	runDoctorTestGit(t, root, "init", "--bare", remote)
	runDoctorTestGit(t, root, "init", work)
	runDoctorTestGit(t, work, "config", "user.email", "baton@example.test")
	runDoctorTestGit(t, work, "config", "user.name", "Baton Test")
	if err := os.MkdirAll(filepath.Join(work, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".github", "baton.yml"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDoctorTestGit(t, work, "add", ".")
	runDoctorTestGit(t, work, "commit", "-m", "Initial commit")
	runDoctorTestGit(t, work, "branch", "-M", "main")
	runDoctorTestGit(t, work, "remote", "add", "origin", remote)
	if pushMain {
		runDoctorTestGit(t, work, "push", "-u", "origin", "main")
	}
	return work
}

func setupDoctorManagedGitRepo(t *testing.T, desired []install.ManagedFile) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	runDoctorTestGit(t, root, "init", "--bare", remote)
	runDoctorTestGit(t, root, "init", work)
	runDoctorTestGit(t, work, "config", "user.email", "baton@example.test")
	runDoctorTestGit(t, work, "config", "user.name", "Baton Test")
	if _, err := install.ApplyManagedFiles(work, desired, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDoctorTestGit(t, work, "add", ".")
	runDoctorTestGit(t, work, "commit", "-m", "Initial Baton setup")
	runDoctorTestGit(t, work, "branch", "-M", "main")
	runDoctorTestGit(t, work, "remote", "add", "origin", remote)
	runDoctorTestGit(t, work, "push", "-u", "origin", "main")
	runDoctorTestGit(t, work, "branch", "agent")
	runDoctorTestGit(t, work, "push", "-u", "origin", "agent")
	return work
}

func writeDoctorJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("check %q not found in %#v", name, checks)
	return Check{}
}

func runDoctorTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
