package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCommandErrorDoesNotRenderArgumentsOrStderr(t *testing.T) {
	err := &CommandError{
		Args:   []string{"ls-remote", "https://token@github.com/example/repo"},
		Stderr: "authentication failed for https://token@github.com/example/repo",
		Cause:  errors.New("exit status 128"),
	}
	if got := err.Error(); got != "git command failed" || strings.Contains(got, "token") {
		t.Fatalf("Error() = %q", got)
	}
}

func TestOutputContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := OutputContext(ctx, "version")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestInspectAgentBranchRefsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := InspectAgentBranchRefsAtContext(ctx, t.TempDir(), AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %T %v", err, err)
	}
}
