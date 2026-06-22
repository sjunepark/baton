package git

type AgentBranchPlanInput struct {
	Remote              string `json:"remote"`
	BaseBranch          string `json:"baseBranch"`
	TargetBranch        string `json:"targetBranch"`
	RemoteBaseSHA       string `json:"remoteBaseSha"`
	RemoteTargetSHA     string `json:"remoteTargetSha"`
	LocalTargetSHA      string `json:"localTargetSha"`
	LocalTargetUpstream string `json:"localTargetUpstream"`
}

type AgentBranchPlan struct {
	SchemaVersion int          `json:"schemaVersion"`
	Kind          string       `json:"kind"`
	Errors        []string     `json:"errors"`
	Warnings      []string     `json:"warnings"`
	Status        []string     `json:"status"`
	ApplyCommands []GitCommand `json:"applyCommands"`
}

type GitCommand struct {
	Description string   `json:"description"`
	Args        []string `json:"args"`
}

func ComputeAgentBranchPlan(input AgentBranchPlanInput) AgentBranchPlan {
	remote := firstNonEmpty(input.Remote, "origin")
	baseBranch := firstNonEmpty(input.BaseBranch, "main")
	targetBranch := firstNonEmpty(input.TargetBranch, "agent")
	remoteBase := remote + "/" + baseBranch
	remoteTarget := remote + "/" + targetBranch

	plan := AgentBranchPlan{SchemaVersion: 1, Kind: "agentBranchPlan"}
	if input.RemoteBaseSHA == "" {
		plan.Errors = append(plan.Errors, "Remote base branch "+remoteBase+" was not found.")
		return plan
	}

	plan.Status = append(plan.Status, remoteBase+": "+shortSHA(input.RemoteBaseSHA))

	if input.RemoteTargetSHA == "" {
		plan.Status = append(plan.Status, remoteTarget+": missing")
		if input.LocalTargetSHA != "" && input.LocalTargetSHA != input.RemoteBaseSHA {
			plan.Errors = append(plan.Errors, "Local "+targetBranch+" exists at "+shortSHA(input.LocalTargetSHA)+", but "+remoteTarget+" is missing and "+remoteBase+" is at "+shortSHA(input.RemoteBaseSHA)+". Refusing to publish a branch that is not exactly "+remoteBase+".")
			return plan
		}
		if input.LocalTargetSHA != "" {
			plan.Status = append(plan.Status, targetBranch+": "+shortSHA(input.LocalTargetSHA))
			plan.ApplyCommands = append(plan.ApplyCommands, GitCommand{
				Description: "Publish local " + targetBranch + " and set upstream to " + remoteTarget + ".",
				Args:        []string{"push", "-u", remote, targetBranch},
			})
			return plan
		}
		plan.Status = append(plan.Status, targetBranch+": missing")
		plan.ApplyCommands = append(plan.ApplyCommands,
			GitCommand{Description: "Fetch " + remoteBase + ".", Args: []string{"fetch", remote, baseBranch}},
			GitCommand{Description: "Create local " + targetBranch + " from " + remoteBase + ".", Args: []string{"branch", targetBranch, remoteBase}},
			GitCommand{Description: "Publish " + targetBranch + " and set upstream to " + remoteTarget + ".", Args: []string{"push", "-u", remote, targetBranch}},
		)
		return plan
	}

	plan.Status = append(plan.Status, remoteTarget+": "+shortSHA(input.RemoteTargetSHA))
	if input.RemoteTargetSHA != input.RemoteBaseSHA {
		plan.Warnings = append(plan.Warnings, remoteTarget+" differs from "+remoteBase+". This script will not force-sync it; use the PR workflow or an explicit human reset decision.")
	}

	if input.LocalTargetSHA == "" {
		plan.Status = append(plan.Status, targetBranch+": missing")
		plan.ApplyCommands = append(plan.ApplyCommands,
			GitCommand{Description: "Fetch " + remoteTarget + ".", Args: []string{"fetch", remote, targetBranch}},
			GitCommand{Description: "Create local tracking branch " + targetBranch + " from " + remoteTarget + ".", Args: []string{"branch", "--track", targetBranch, remoteTarget}},
		)
		return plan
	}

	plan.Status = append(plan.Status, targetBranch+": "+shortSHA(input.LocalTargetSHA))
	if input.LocalTargetSHA != input.RemoteTargetSHA {
		plan.Warnings = append(plan.Warnings, "Local "+targetBranch+" differs from "+remoteTarget+". This script will not reset local work.")
		return plan
	}
	if input.LocalTargetUpstream != remoteTarget {
		plan.ApplyCommands = append(plan.ApplyCommands, GitCommand{
			Description: "Set " + targetBranch + " upstream to " + remoteTarget + ".",
			Args:        []string{"branch", "--set-upstream-to", remoteTarget, targetBranch},
		})
	}
	return plan
}

func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
