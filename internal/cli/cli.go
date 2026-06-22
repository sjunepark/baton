package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sejunpark/baton/internal/config"
	"github.com/sejunpark/baton/internal/git"
	"github.com/sejunpark/baton/internal/labels"
	"github.com/sejunpark/baton/internal/policy"
)

const (
	exitOK          = 0
	exitPolicy      = 1
	exitUsage       = 2
	exitConfig      = 3
	exitLocalGit    = 6
	schemaVersionV1 = 1
)

// Run executes the Baton command line. It is small by design: command packages
// own deterministic decisions, and this layer only parses flags and renders.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printHelp(stdout)
		return exitOK
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return exitOK
	case "issue-policy":
		return runIssuePolicy(args[1:], stdout, stderr)
	case "pr-policy":
		return runPRPolicy(args[1:], stdout, stderr)
	case "ensure-branch":
		return runEnsureBranch(args[1:], stdout, stderr)
	case "labels":
		return runLabels(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return exitUsage
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `baton coordinates reusable GitHub issue/PR agent workflows.

Usage:
  baton --help
  baton version
  baton issue-policy --body-file <path> [--labels a,b] [--config <path>] [--json]
  baton pr-policy --fixture <path> [--config <path>] [--json]
  baton ensure-branch --remote-base <sha> [--remote-target <sha>] [--local-target <sha>] [--json]
  baton labels --file <path> [--json]

The current implementation is the local deterministic policy/parsing core.
GitHub writes, queue inspection, and worktree leasing are implemented in later
phases.
`)
}

func runIssuePolicy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue-policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bodyFile := fs.String("body-file", "", "issue body markdown file")
	labelsCSV := fs.String("labels", "", "comma-separated current labels")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "issue-policy requires --body-file")
		return exitUsage
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}
	body, err := os.ReadFile(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "read issue body: %v\n", err)
		return exitUsage
	}

	decision := policy.ComputeIssuePolicy(policy.IssuePolicyInput{
		Body:          string(body),
		CurrentLabels: splitCSV(*labelsCSV),
		Policy:        cfg.IssuePolicy,
	})
	if *jsonOut {
		return writeJSON(stdout, stderr, decision)
	}
	if !decision.IsFormIssue {
		fmt.Fprintln(stdout, "Issue policy: body does not match the configured form.")
		return exitOK
	}
	fmt.Fprintf(stdout, "Issue policy: add %s; remove %s\n", strings.Join(decision.LabelsToAdd, ", "), strings.Join(decision.LabelsToRemove, ", "))
	if len(decision.MissingRequiredSections) > 0 {
		fmt.Fprintf(stdout, "Missing required sections: %s\n", strings.Join(decision.MissingRequiredSections, ", "))
	}
	return exitOK
}

func runPRPolicy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr-policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fixturePath := fs.String("fixture", "", "pure PR policy fixture JSON")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *fixturePath == "" {
		fmt.Fprintln(stderr, "pr-policy currently requires --fixture for deterministic local evaluation")
		return exitUsage
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}
	content, err := os.ReadFile(*fixturePath)
	if err != nil {
		fmt.Fprintf(stderr, "read PR policy fixture: %v\n", err)
		return exitUsage
	}
	var input policy.PRPolicyInput
	if err := json.Unmarshal(content, &input); err != nil {
		fmt.Fprintf(stderr, "parse PR policy fixture: %v\n", err)
		return exitUsage
	}
	input.Policy = cfg
	decision := policy.ComputePullRequestPolicy(input)
	if *jsonOut {
		return writeJSON(stdout, stderr, decision)
	}
	if len(decision.Errors) == 0 {
		fmt.Fprintln(stdout, "PR policy check passed.")
		return exitOK
	}
	fmt.Fprintln(stderr, "PR policy check failed:")
	for _, msg := range decision.Errors {
		fmt.Fprintf(stderr, "- %s\n", msg)
	}
	return exitPolicy
}

func runEnsureBranch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ensure-branch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remote := fs.String("remote", "origin", "remote name")
	base := fs.String("base", "main", "base branch")
	target := fs.String("target", "agent", "staging branch")
	remoteBase := fs.String("remote-base", "", "remote base SHA")
	remoteTarget := fs.String("remote-target", "", "remote target SHA")
	localTarget := fs.String("local-target", "", "local target SHA")
	localUpstream := fs.String("local-upstream", "", "local target upstream")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	plan := git.ComputeAgentBranchPlan(git.AgentBranchPlanInput{
		Remote:              *remote,
		BaseBranch:          *base,
		TargetBranch:        *target,
		RemoteBaseSHA:       *remoteBase,
		RemoteTargetSHA:     *remoteTarget,
		LocalTargetSHA:      *localTarget,
		LocalTargetUpstream: *localUpstream,
	})
	if *jsonOut {
		return writeJSON(stdout, stderr, plan)
	}
	fmt.Fprintln(stdout, "Agent branch plan:")
	for _, line := range plan.Status {
		fmt.Fprintf(stdout, "- %s\n", line)
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
	}
	for _, err := range plan.Errors {
		fmt.Fprintf(stderr, "error: %s\n", err)
	}
	if len(plan.Errors) > 0 {
		return exitLocalGit
	}
	if len(plan.ApplyCommands) == 0 {
		fmt.Fprintln(stdout, "No branch setup commands are needed.")
		return exitOK
	}
	fmt.Fprintln(stdout, "Dry run. Would run:")
	for _, command := range plan.ApplyCommands {
		fmt.Fprintf(stdout, "- %s\n", command.Description)
		fmt.Fprintf(stdout, "  git %s\n", strings.Join(command.Args, " "))
	}
	return exitOK
}

func runLabels(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("labels", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", ".github/labels.yml", "labels manifest path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	manifest, err := labels.LoadManifest(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}
	if *jsonOut {
		return writeJSON(stdout, stderr, manifest)
	}
	for _, label := range manifest.Labels {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", label.Name, label.Color, label.Description)
	}
	return exitOK
}

func loadConfig(path string) (config.Config, error) {
	if path != "" {
		return config.Load(path)
	}
	cfg, err := config.LoadForRepo(".")
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, config.ErrConfigNotFound) {
		return config.DefaultCreoCompat(), nil
	}
	return config.Config{}, err
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			labels = append(labels, part)
		}
	}
	return labels
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "encode JSON: %v\n", err)
		return exitUsage
	}
	return exitOK
}
