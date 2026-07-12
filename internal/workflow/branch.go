package workflow

import (
	"context"
	"errors"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	gitadapter "github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/operation"
)

type BranchInput struct {
	WorkingDir string
	ConfigPath string
	Plan       gitadapter.AgentBranchPlanInput
	Apply      bool
}

type BranchWorkflow struct {
	inspect func(context.Context, string, gitadapter.AgentBranchPlanInput) (gitadapter.AgentBranchPlanInput, error)
	apply   func(context.Context, string, gitadapter.AgentBranchPlan) (operation.Report, error)
}

func NewBranchWorkflow() BranchWorkflow {
	return BranchWorkflow{inspect: gitadapter.InspectAgentBranchRefsAtContext, apply: gitadapter.ApplyAgentBranchPlanWithReportAtContext}
}

func (workflow BranchWorkflow) Run(input BranchInput) (gitadapter.AgentBranchPlan, error) {
	return workflow.RunContext(context.Background(), input)
}

func (workflow BranchWorkflow) RunContext(ctx context.Context, input BranchInput) (gitadapter.AgentBranchPlan, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	facts := input.Plan
	if facts.Remote == "" || facts.BaseBranch == "" || facts.TargetBranch == "" {
		root := input.WorkingDir
		if root == "" {
			root = "."
		}
		if resolvedRoot, resolveErr := gitadapter.RepositoryRootContext(ctx, root); resolveErr == nil {
			root = resolvedRoot
		}
		var policy config.RepositoryPolicy
		var err error
		if input.ConfigPath != "" {
			policy, err = config.Load(input.ConfigPath)
		} else {
			policy, err = config.LoadForRepo(root)
		}
		if err != nil {
			hint := "Check repository policy before planning branch reconciliation."
			if errors.Is(err, config.ErrConfigNotFound) {
				hint = "Run `baton init --dry-run` and apply the repository policy first."
			}
			return gitadapter.AgentBranchPlan{}, apperror.Wrap(apperror.Config, "repository policy could not be loaded", err, hint)
		}
		if facts.Remote == "" {
			facts.Remote = policy.Repository.DefaultRemote
		}
		if facts.BaseBranch == "" {
			facts.BaseBranch = policy.Repository.BaseBranch
		}
		if facts.TargetBranch == "" {
			facts.TargetBranch = policy.Repository.StagingBranch
		}
	}
	if facts.RemoteBaseSHA == "" && facts.RemoteTargetSHA == "" && facts.LocalTargetSHA == "" && facts.LocalTargetUpstream == "" {
		inspected, err := workflow.inspect(ctx, input.WorkingDir, facts)
		if err != nil {
			return gitadapter.AgentBranchPlan{}, apperror.Wrap(apperror.LocalGit, "git branch facts could not be inspected", err, "")
		}
		facts = inspected
	}
	plan := gitadapter.ComputeAgentBranchPlan(facts)
	if !input.Apply || len(plan.Errors) > 0 || len(plan.ApplyCommands) == 0 {
		return plan, nil
	}
	report, err := workflow.apply(ctx, input.WorkingDir, plan)
	plan.Report = &report
	if err != nil {
		return plan, apperror.WithReport(apperror.Wrap(apperror.LocalGit, "git branch plan could not be applied", err, ""), report)
	}
	return plan, nil
}
