package lease

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireRefusesBranchCollisionAndReleaseDirty(t *testing.T) {
	repo := initRepo(t)
	manager := NewManager(filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	record, err := manager.Acquire(AcquireRequest{
		SourceRepoPath: repo,
		Purpose:        "issue-1",
		BaseRef:        "main",
		NewBranch:      "agent-work/one",
		Repo:           "demo",
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.WorktreePath == "" || record.HeadRef != "agent-work/one" || record.Status != "active" {
		t.Fatalf("record = %#v", record)
	}
	if _, err := manager.Acquire(AcquireRequest{
		SourceRepoPath: repo,
		Purpose:        "issue-1-again",
		BaseRef:        "main",
		NewBranch:      "agent-work/one",
		Repo:           "demo",
		Now:            now.Add(time.Minute),
	}); err == nil {
		t.Fatal("expected branch collision error")
	}
	if err := os.WriteFile(filepath.Join(record.WorktreePath, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseByID(record.ID, false); err == nil {
		t.Fatal("expected dirty release refusal")
	}
	result, err := manager.ReleaseByID(record.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Dirty || result.Lease.Status != "released" {
		t.Fatalf("release = %#v", result)
	}
}

func TestPruneDryRunIncludesReleasedAndExpired(t *testing.T) {
	repo := initRepo(t)
	manager := NewManager(filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	released, err := manager.Acquire(AcquireRequest{
		SourceRepoPath: repo,
		Purpose:        "released",
		BaseRef:        "main",
		NewBranch:      "agent-work/released",
		Repo:           "demo",
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseByID(released.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Acquire(AcquireRequest{
		SourceRepoPath: repo,
		Purpose:        "expired",
		BaseRef:        "main",
		NewBranch:      "agent-work/expired",
		Repo:           "demo",
		Now:            now.Add(time.Minute),
		TTL:            time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.PruneDryRun(now.Add(10 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "baton@example.test")
	runGit(t, dir, "config", "user.name", "Baton Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "Initial commit")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
