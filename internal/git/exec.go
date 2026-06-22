package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func InspectAgentBranchRefs(input AgentBranchPlanInput) (AgentBranchPlanInput, error) {
	remote := firstNonEmpty(input.Remote, "origin")
	baseBranch := firstNonEmpty(input.BaseBranch, "main")
	targetBranch := firstNonEmpty(input.TargetBranch, "agent")
	out := input
	out.Remote = remote
	out.BaseBranch = baseBranch
	out.TargetBranch = targetBranch

	remoteHeads, err := lsRemoteHeads(remote, baseBranch, targetBranch)
	if err != nil {
		return AgentBranchPlanInput{}, err
	}
	out.RemoteBaseSHA = firstNonEmpty(out.RemoteBaseSHA, remoteHeads[baseBranch])
	out.RemoteTargetSHA = firstNonEmpty(out.RemoteTargetSHA, remoteHeads[targetBranch])

	localSHA, err := localBranchSHA(targetBranch)
	if err != nil {
		return AgentBranchPlanInput{}, err
	}
	out.LocalTargetSHA = firstNonEmpty(out.LocalTargetSHA, localSHA)
	out.LocalTargetUpstream = firstNonEmpty(out.LocalTargetUpstream, localBranchUpstream(targetBranch))
	return out, nil
}

func ApplyAgentBranchPlan(plan AgentBranchPlan) error {
	if len(plan.Errors) > 0 {
		return fmt.Errorf("cannot apply a plan with errors")
	}
	for _, command := range plan.ApplyCommands {
		if _, err := runGit(command.Args...); err != nil {
			return err
		}
	}
	return nil
}

func lsRemoteHeads(remote string, branches ...string) (map[string]string, error) {
	args := append([]string{"ls-remote", "--heads", remote}, branches...)
	stdout, err := runGit(args...)
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

func localBranchSHA(branch string) (string, error) {
	exists := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err := exists.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	out, err := runGit("show-ref", "--verify", "--hash", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func localBranchUpstream(branch string) string {
	out, err := runGit("rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return stdout.String(), fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, message)
		}
		return stdout.String(), fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
