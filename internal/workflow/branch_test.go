package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	gitadapter "github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/operation"
)

func TestBranchWorkflowUsesSuppliedFactsAndAppliesComputedPlan(t *testing.T) {
	inspectCalled := false
	applyCalled := false
	workflow := BranchWorkflow{
		inspect: func(context.Context, string, gitadapter.AgentBranchPlanInput) (gitadapter.AgentBranchPlanInput, error) {
			inspectCalled = true
			return gitadapter.AgentBranchPlanInput{}, nil
		},
		apply: func(_ context.Context, root string, plan gitadapter.AgentBranchPlan) (operation.Report, error) {
			applyCalled = true
			if root != "/repo" || len(plan.ApplyCommands) != 3 {
				t.Fatalf("root=%q plan=%+v", root, plan)
			}
			return operation.NewReport(nil), nil
		},
	}
	plan, err := workflow.Run(BranchInput{
		WorkingDir: "/repo", Apply: true,
		Plan: gitadapter.AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent", RemoteBaseSHA: "abc123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspectCalled || !applyCalled || len(plan.ApplyCommands) != 3 {
		t.Fatalf("inspect=%v apply=%v plan=%+v", inspectCalled, applyCalled, plan)
	}
}

func TestBranchWorkflowFailuresIncludeRecoveryHints(t *testing.T) {
	inspectWorkflow := BranchWorkflow{inspect: func(context.Context, string, gitadapter.AgentBranchPlanInput) (gitadapter.AgentBranchPlanInput, error) {
		return gitadapter.AgentBranchPlanInput{}, errors.New("inspect failed")
	}}
	_, err := inspectWorkflow.Run(BranchInput{Plan: gitadapter.AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent"}})
	if applicationError := apperror.As(err); applicationError == nil || applicationError.Hint == "" {
		t.Fatalf("inspection error = %v", err)
	}

	applyWorkflow := BranchWorkflow{apply: func(context.Context, string, gitadapter.AgentBranchPlan) (operation.Report, error) {
		return operation.NewReport(nil), errors.New("apply failed")
	}}
	_, err = applyWorkflow.Run(BranchInput{Apply: true, Plan: gitadapter.AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent", RemoteBaseSHA: "base"}})
	if applicationError := apperror.As(err); applicationError == nil || applicationError.Hint == "" {
		t.Fatalf("apply error = %v", err)
	}
}

func TestBranchWorkflowUsesCompiledRepositoryPolicyDefaults(t *testing.T) {
	root := t.TempDir()
	policy := config.DefaultConfig()
	policy.Repository.DefaultRemote = "upstream"
	policy.Repository.BaseBranch = "stable"
	policy.Repository.StagingBranch = "integration"
	content, err := config.MarshalYAML(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "baton.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := BranchWorkflow{inspect: func(_ context.Context, _ string, input gitadapter.AgentBranchPlanInput) (gitadapter.AgentBranchPlanInput, error) {
		if input.Remote != "upstream" || input.BaseBranch != "stable" || input.TargetBranch != "integration" {
			t.Fatalf("branch input = %+v", input)
		}
		input.RemoteBaseSHA = "base-sha"
		return input, nil
	}}
	plan, err := workflow.Run(BranchInput{WorkingDir: root})
	if err != nil || plan.Preconditions.Remote != "upstream" || plan.Preconditions.BaseBranch != "stable" || plan.Preconditions.TargetBranch != "integration" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestBranchWorkflowDoesNotApplyPlanWithErrors(t *testing.T) {
	workflow := BranchWorkflow{
		inspect: func(context.Context, string, gitadapter.AgentBranchPlanInput) (gitadapter.AgentBranchPlanInput, error) {
			return gitadapter.AgentBranchPlanInput{}, nil
		},
		apply: func(context.Context, string, gitadapter.AgentBranchPlan) (operation.Report, error) {
			t.Fatal("invalid plan must not apply")
			return operation.Report{}, nil
		},
	}
	plan, err := workflow.Run(BranchInput{Apply: true, Plan: gitadapter.AgentBranchPlanInput{Remote: "origin", BaseBranch: "main", TargetBranch: "agent"}})
	if err != nil || len(plan.Errors) == 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}
