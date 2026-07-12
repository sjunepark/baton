package git

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/sjunepark/baton/internal/operation"
)

func InspectAgentBranchRefs(input AgentBranchPlanInput) (AgentBranchPlanInput, error) {
	return InspectAgentBranchRefsContext(context.Background(), input)
}

func InspectAgentBranchRefsAt(root string, input AgentBranchPlanInput) (AgentBranchPlanInput, error) {
	return InspectAgentBranchRefsAtContext(context.Background(), root, input)
}

func InspectAgentBranchRefsContext(ctx context.Context, input AgentBranchPlanInput) (AgentBranchPlanInput, error) {
	return InspectAgentBranchRefsAtContext(ctx, "", input)
}

func InspectAgentBranchRefsAtContext(ctx context.Context, root string, input AgentBranchPlanInput) (AgentBranchPlanInput, error) {
	remote := firstNonEmpty(input.Remote, "origin")
	baseBranch := firstNonEmpty(input.BaseBranch, "main")
	targetBranch := firstNonEmpty(input.TargetBranch, "agent")
	out := input
	out.Remote = remote
	out.BaseBranch = baseBranch
	out.TargetBranch = targetBranch

	remoteHeads, err := lsRemoteHeadsAtContext(ctx, root, remote, baseBranch, targetBranch)
	if err != nil {
		return AgentBranchPlanInput{}, err
	}
	out.RemoteBaseSHA = firstNonEmpty(out.RemoteBaseSHA, remoteHeads[baseBranch])
	out.RemoteTargetSHA = firstNonEmpty(out.RemoteTargetSHA, remoteHeads[targetBranch])

	localSHA, err := localBranchSHAAtContext(ctx, root, targetBranch)
	if err != nil {
		return AgentBranchPlanInput{}, err
	}
	out.LocalTargetSHA = firstNonEmpty(out.LocalTargetSHA, localSHA)
	if out.LocalTargetUpstream == "" && out.LocalTargetSHA != "" {
		upstream, err := localBranchUpstreamAtContext(ctx, root, targetBranch)
		if err != nil {
			return AgentBranchPlanInput{}, err
		}
		out.LocalTargetUpstream = upstream
	}
	return out, nil
}

func ApplyAgentBranchPlan(plan AgentBranchPlan) error {
	return ApplyAgentBranchPlanContext(context.Background(), plan)
}

func ApplyAgentBranchPlanAt(root string, plan AgentBranchPlan) error {
	return ApplyAgentBranchPlanAtContext(context.Background(), root, plan)
}

func ApplyAgentBranchPlanContext(ctx context.Context, plan AgentBranchPlan) error {
	return ApplyAgentBranchPlanAtContext(ctx, "", plan)
}

func ApplyAgentBranchPlanAtContext(ctx context.Context, root string, plan AgentBranchPlan) error {
	_, err := ApplyAgentBranchPlanWithReportAtContext(ctx, root, plan)
	return err
}

func ApplyAgentBranchPlanWithReportAtContext(ctx context.Context, root string, plan AgentBranchPlan) (operation.Report, error) {
	if len(plan.Errors) > 0 {
		return operation.NewReport([]operation.Result{{
			ID: "branch-plan-validation", Resource: plan.Preconditions.Remote + "/" + plan.Preconditions.TargetBranch, Action: "validate_plan", Status: operation.StatusRefused,
			Error: &operation.Failure{Category: "invalidPlan", Message: "branch plan contains errors"},
		}}), fmt.Errorf("cannot apply a plan with errors")
	}
	expected := plan.Preconditions
	actual, err := InspectAgentBranchRefsAtContext(ctx, root, AgentBranchPlanInput{Remote: expected.Remote, BaseBranch: expected.BaseBranch, TargetBranch: expected.TargetBranch})
	if err != nil {
		return operation.NewReport([]operation.Result{{
			ID: "branch-preflight", Resource: expected.Remote + "/" + expected.TargetBranch, Action: "inspect_refs", Status: operation.StatusFailed,
			Error: &operation.Failure{Category: "localGit", Message: "branch refs could not be inspected"},
		}}), err
	}
	if !reflect.DeepEqual(actual, expected) {
		return operation.NewReport([]operation.Result{{ID: "branch-precondition", Resource: expected.Remote + "/" + expected.TargetBranch, Action: "verify_refs", Status: operation.StatusRefused, Error: &operation.Failure{Category: "stale", Message: "branch refs changed after review"}}}), fmt.Errorf("branch plan is stale; inspect and review a new plan before applying")
	}
	results := make([]operation.Result, len(plan.ApplyCommands))
	for index, command := range plan.ApplyCommands {
		results[index] = operation.Result{ID: fmt.Sprintf("git-%03d", index+1), Resource: expected.Remote + "/" + expected.TargetBranch, Action: command.Description, Status: operation.StatusNotAttempted}
	}
	for index, command := range plan.ApplyCommands {
		if _, err := OutputAtContext(ctx, root, command.Args...); err != nil {
			results[index].Status = operation.StatusFailed
			results[index].Error = &operation.Failure{Category: "localGit", Message: "git command failed"}
			return operation.NewReport(results), err
		}
		results[index].Status = operation.StatusApplied
	}
	return operation.NewReport(results), nil
}

func lsRemoteHeadsAtContext(ctx context.Context, root, remote string, branches ...string) (map[string]string, error) {
	args := append([]string{"ls-remote", "--heads", remote}, branches...)
	stdout, err := OutputAtContext(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		branch := strings.TrimPrefix(fields[1], "refs/heads/")
		heads[branch] = fields[0]
	}
	return heads, nil
}

func localBranchSHAAtContext(ctx context.Context, root, branch string) (string, error) {
	_, err := OutputAtContext(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		if IsExitCode(err, 1) {
			return "", nil
		}
		return "", err
	}
	out, err := OutputAtContext(ctx, root, "show-ref", "--verify", "--hash", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func localBranchUpstreamAtContext(ctx context.Context, root, branch string) (string, error) {
	out, err := OutputAtContext(ctx, root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil {
		if IsExitCode(err, 128) && (StderrContains(err, "no upstream configured") || StderrContains(err, "no upstream branch")) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}
