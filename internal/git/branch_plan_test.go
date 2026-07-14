package git

import "testing"

const (
	mainSHA  = "1111111111111111111111111111111111111111"
	agentSHA = "2222222222222222222222222222222222222222"
)

func TestComputeAgentBranchPlan(t *testing.T) {
	tests := []struct {
		name         string
		input        AgentBranchPlanInput
		wantErrors   []string
		wantWarnings []string
		wantArgs     [][]string
	}{
		{
			name:       "plans initial agent branch creation",
			input:      AgentBranchPlanInput{RemoteBaseSHA: mainSHA},
			wantErrors: []string{},
			wantArgs: [][]string{
				{"fetch", "origin", "main"},
				{"branch", "agent", "origin/main"},
				{"push", "-u", "origin", "agent"},
			},
		},
		{
			name:       "publishes matching local agent branch",
			input:      AgentBranchPlanInput{RemoteBaseSHA: mainSHA, LocalTargetSHA: mainSHA},
			wantErrors: []string{},
			wantArgs:   [][]string{{"push", "-u", "origin", "agent"}},
		},
		{
			name:       "refuses divergent local branch initial publish",
			input:      AgentBranchPlanInput{RemoteBaseSHA: mainSHA, LocalTargetSHA: agentSHA},
			wantErrors: []string{"Local agent exists at 222222222222, but origin/agent is missing and origin/main is at 111111111111. Refusing to publish a branch that is not exactly origin/main."},
			wantArgs:   [][]string{},
		},
		{
			name:         "plans tracking branch when remote staging history exists",
			input:        AgentBranchPlanInput{RemoteBaseSHA: mainSHA, RemoteTargetSHA: agentSHA},
			wantErrors:   []string{},
			wantWarnings: []string{},
			wantArgs: [][]string{
				{"fetch", "origin", "agent"},
				{"branch", "--track", "agent", "origin/agent"},
			},
		},
		{
			name:       "sets upstream when local matches remote without upstream",
			input:      AgentBranchPlanInput{RemoteBaseSHA: agentSHA, RemoteTargetSHA: agentSHA, LocalTargetSHA: agentSHA},
			wantErrors: []string{},
			wantArgs:   [][]string{{"branch", "--set-upstream-to", "origin/agent", "agent"}},
		},
		{
			name:       "sets upstream when local tracks another branch",
			input:      AgentBranchPlanInput{RemoteBaseSHA: agentSHA, RemoteTargetSHA: agentSHA, LocalTargetSHA: agentSHA, LocalTargetUpstream: "upstream/agent"},
			wantErrors: []string{},
			wantArgs:   [][]string{{"branch", "--set-upstream-to", "origin/agent", "agent"}},
		},
		{
			name:       "no commands when tracking is already correct",
			input:      AgentBranchPlanInput{RemoteBaseSHA: agentSHA, RemoteTargetSHA: agentSHA, LocalTargetSHA: agentSHA, LocalTargetUpstream: "origin/agent"},
			wantErrors: []string{},
			wantArgs:   [][]string{},
		},
		{
			name:         "local divergence only warns",
			input:        AgentBranchPlanInput{RemoteBaseSHA: mainSHA, RemoteTargetSHA: mainSHA, LocalTargetSHA: agentSHA},
			wantErrors:   []string{},
			wantWarnings: []string{"Local agent differs from origin/agent. This script will not reset local work."},
			wantArgs:     [][]string{},
		},
		{
			name:       "requires remote base",
			input:      AgentBranchPlanInput{},
			wantErrors: []string{"Remote base branch origin/main was not found."},
			wantArgs:   [][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := ComputeAgentBranchPlan(tt.input)
			assertStrings(t, plan.Errors, tt.wantErrors)
			assertStrings(t, plan.Warnings, tt.wantWarnings)
			if len(plan.ApplyCommands) != len(tt.wantArgs) {
				t.Fatalf("commands = %#v, want args %#v", plan.ApplyCommands, tt.wantArgs)
			}
			for i, command := range plan.ApplyCommands {
				assertStrings(t, command.Args, tt.wantArgs[i])
			}
		})
	}
}

func TestComputeAgentBranchPlanReportsExistingStagingHistoryAsStatus(t *testing.T) {
	plan := ComputeAgentBranchPlan(AgentBranchPlanInput{
		Remote: "origin", BaseBranch: "main", TargetBranch: "dev",
		RemoteBaseSHA: mainSHA, RemoteTargetSHA: agentSHA,
		LocalTargetSHA: agentSHA, LocalTargetUpstream: "origin/dev",
	})

	if len(plan.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", plan.Warnings)
	}
	assertStrings(t, plan.Status, []string{
		"origin/main: 111111111111",
		"origin/dev: 222222222222",
		"origin/dev has existing staging history; Baton will preserve it.",
		"dev: 222222222222",
	})
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
