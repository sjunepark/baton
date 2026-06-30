package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
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

func TestDoctorWarnsWhenStagingBranchSetupIsNeeded(t *testing.T) {
	work := setupDoctorGitRepo(t, true)
	t.Chdir(work)

	result := Run("")
	check := findCheck(t, result.Checks, "staging-branch")
	if check.Status != "warn" {
		t.Fatalf("staging-branch check = %#v, want warn", check)
	}
	if !strings.Contains(check.Message, "ensure-branch") {
		t.Fatalf("staging-branch message = %q, want ensure-branch guidance", check.Message)
	}
	if len(result.Help) != 2 || !strings.Contains(strings.Join(result.Help, "\n"), "baton ensure-branch --json") {
		t.Fatalf("help = %#v, want ensure-branch guidance", result.Help)
	}
}

func TestDoctorFailsWhenBaseBranchIsMissing(t *testing.T) {
	work := setupDoctorGitRepo(t, false)
	t.Chdir(work)

	result := Run("")
	check := findCheck(t, result.Checks, "staging-branch")
	if check.Status != "fail" {
		t.Fatalf("staging-branch check = %#v, want fail", check)
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
