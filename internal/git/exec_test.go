package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAndApplyAgentBranchPlan(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	runTestGit(t, root, "init", "--bare", remote)
	runTestGit(t, root, "init", work)
	runTestGit(t, work, "config", "user.email", "baton@example.test")
	runTestGit(t, work, "config", "user.name", "Baton Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", "README.md")
	runTestGit(t, work, "commit", "-m", "Initial commit")
	runTestGit(t, work, "branch", "-M", "main")
	runTestGit(t, work, "remote", "add", "origin", remote)
	runTestGit(t, work, "push", "-u", "origin", "main")
	t.Chdir(work)

	inspected, err := InspectAgentBranchRefs(AgentBranchPlanInput{})
	if err != nil {
		t.Fatal(err)
	}
	plan := ComputeAgentBranchPlan(inspected)
	if len(plan.Errors) > 0 {
		t.Fatalf("errors = %#v", plan.Errors)
	}
	if len(plan.ApplyCommands) != 3 {
		t.Fatalf("commands = %#v", plan.ApplyCommands)
	}
	if err := ApplyAgentBranchPlan(plan); err != nil {
		t.Fatal(err)
	}
	out := runTestGit(t, work, "ls-remote", "--heads", "origin", "agent")
	if !strings.Contains(out, "refs/heads/agent") {
		t.Fatalf("agent branch was not published: %s", out)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
