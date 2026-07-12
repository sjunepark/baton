package git

import (
	"context"
	"errors"
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

func TestApplyAgentBranchPlanRefusesStaleReviewedSHAs(t *testing.T) {
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
	inspected, err := InspectAgentBranchRefsAt(work, AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	plan := ComputeAgentBranchPlan(inspected)
	plan.Preconditions.RemoteBaseSHA = "reviewed-but-stale"
	report, err := ApplyAgentBranchPlanWithReportAtContext(context.Background(), work, plan)
	if err == nil || report.Status != "refused" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	out := runTestGit(t, work, "ls-remote", "--heads", "origin", "agent")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stale plan mutated remote: %s", out)
	}
}

func TestApplyAgentBranchPlanReportsInspectionFailure(t *testing.T) {
	plan := ComputeAgentBranchPlan(AgentBranchPlanInput{
		Remote: "origin", BaseBranch: "main", TargetBranch: "agent", RemoteBaseSHA: "reviewed",
	})
	report, err := ApplyAgentBranchPlanWithReportAtContext(context.Background(), filepath.Join(t.TempDir(), "missing"), plan)
	if err == nil || report.Status != "failed" || len(report.Operations) != 1 || report.Operations[0].Status != "failed" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestApplyAgentBranchPlanReportsInvalidPlanRefusal(t *testing.T) {
	plan := ComputeAgentBranchPlan(AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent"})
	report, err := ApplyAgentBranchPlanWithReportAtContext(context.Background(), t.TempDir(), plan)
	if err == nil || report.Status != "refused" || len(report.Operations) != 1 || report.Operations[0].Status != "refused" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestLocalBranchUpstreamContextDistinguishesMissingFromCancellation(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init", ".")
	runTestGit(t, root, "config", "user.email", "baton@example.test")
	runTestGit(t, root, "config", "user.name", "Baton Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "README.md")
	runTestGit(t, root, "commit", "-m", "Initial commit")
	branch := strings.TrimSpace(runTestGit(t, root, "branch", "--show-current"))
	upstream, err := localBranchUpstreamAtContext(context.Background(), root, branch)
	if err != nil || upstream != "" {
		t.Fatalf("upstream=%q err=%v", upstream, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = localBranchUpstreamAtContext(ctx, root, branch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%T %v", err, err)
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
