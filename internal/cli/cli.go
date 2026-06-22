package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/complete"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/doctor"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/labels"
	"github.com/sjunepark/baton/internal/lease"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
)

const (
	exitOK          = 0
	exitPolicy      = 1
	exitUsage       = 2
	exitConfig      = 3
	exitAuth        = 4
	exitGitHub      = 5
	exitLocalGit    = 6
	schemaVersionV1 = 1
)

type renderer struct {
	stdout     io.Writer
	stderr     io.Writer
	structured bool
}

type errorResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Category      string `json:"category"`
	ExitCode      int    `json:"exitCode"`
	Message       string `json:"message"`
	Hint          string `json:"hint,omitempty"`
	Retryable     bool   `json:"retryable"`
}

type pullRequestsResult struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Repo          string            `json:"repo"`
	Count         int               `json:"count"`
	Counts        pullRequestCounts `json:"counts"`
	PullRequests  []queue.PullState `json:"pullRequests"`
	Help          []string          `json:"help,omitempty"`
}

type pullRequestCounts struct {
	Success int `json:"success"`
	Failure int `json:"failure"`
	Pending int `json:"pending"`
	Unknown int `json:"unknown"`
}

type leasesResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Count         int            `json:"count"`
	Counts        leaseCounts    `json:"counts"`
	Leases        []lease.Record `json:"leases"`
	Help          []string       `json:"help,omitempty"`
}

type leaseCounts struct {
	Active   int `json:"active"`
	Released int `json:"released"`
	Pruned   int `json:"pruned"`
}

func newRenderer(stdout, stderr io.Writer, structured bool) renderer {
	return renderer{stdout: stdout, stderr: stderr, structured: structured}
}

func (r renderer) JSON(value any) int {
	return writeJSON(r.stdout, r.stderr, value)
}

func (r renderer) Error(code int, err error, hint string) int {
	if err == nil {
		err = fmt.Errorf("command failed")
	}
	return r.ErrorMessage(code, err.Error(), hint)
}

func (r renderer) ErrorMessage(code int, message, hint string) int {
	if !r.structured {
		fmt.Fprintln(r.stderr, message)
		return code
	}
	result := errorResult{
		SchemaVersion: schemaVersionV1,
		Kind:          "error",
		Category:      exitCategory(code),
		ExitCode:      code,
		Message:       message,
		Hint:          firstNonEmpty(hint, defaultErrorHint(code, message)),
		Retryable:     errorRetryable(code),
	}
	if writeCode := r.JSON(result); writeCode != exitOK {
		return writeCode
	}
	return code
}

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
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "migrate-config":
		return runMigrateConfig(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "issue-policy":
		return runIssuePolicy(args[1:], stdout, stderr)
	case "pr-policy":
		return runPRPolicy(args[1:], stdout, stderr)
	case "sync-labels":
		return runSyncLabels(args[1:], stdout, stderr)
	case "queue":
		return runQueue(args[1:], stdout, stderr)
	case "prs":
		return runPRs(args[1:], stdout, stderr)
	case "pr":
		return runPR(args[1:], stdout, stderr)
	case "checks":
		return runChecks(args[1:], stdout, stderr)
	case "review-threads":
		return runReviewThreads(args[1:], stdout, stderr)
	case "next":
		return runNext(args[1:], stdout, stderr)
	case "lease":
		return runLease(args[1:], stdout, stderr)
	case "release":
		return runRelease(args[1:], stdout, stderr)
	case "leases":
		return runLeases(args[1:], stdout, stderr)
	case "prune":
		return runPrune(args[1:], stdout, stderr)
	case "complete":
		return runComplete(args[1:], stdout, stderr)
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
  baton init --dry-run|--apply [--profile default] [--go-install module@version|--install-command <cmd>] [--yes] [--json]
  baton migrate-config --dry-run|--apply [--from <path>] [--to <path>] [--yes] [--json]
  baton doctor [--config <path>] [--json]
  baton issue-policy --body-file <path> [--labels a,b] [--config <path>] [--json]
  baton issue-policy --event <path> [--apply] [--repo owner/name] [--config <path>] [--json]
  baton pr-policy --fixture <path> [--config <path>] [--json]
  baton pr-policy --event <path> [--config <path>] [--json]
  baton sync-labels --dry-run|--apply [--repo owner/name] [--labels-file <path>] [--json]
  baton queue --json [--repo owner/name] [--config <path>]
  baton prs --json [--repo owner/name] [--config <path>]
  baton pr <number> --json [--repo owner/name] [--config <path>]
  baton checks <number> --json [--repo owner/name] [--config <path>]
  baton review-threads <number> --json [--repo owner/name] [--config <path>]
  baton next --json [--repo owner/name] [--config <path>]
  baton lease --purpose <purpose> --branch <ref> [--repo owner/name] --json
  baton lease --purpose <purpose> --base <ref> --new-branch <ref> [--repo owner/name] --json
  baton release --lease <id>|--path <path> [--keep-dirty]
  baton leases --json
  baton prune --dry-run|--yes --json
  baton complete --summary <text> [--lease <id>] [--validation <text>] [--comment --repo owner/name --issue N|--pr N] [--json]
  baton ensure-branch [--apply] [--remote origin] [--base main] [--target agent] [--json]
  baton labels --file <path> [--json]

The current implementation includes policy checks, install planning, GitHub
label/policy writes, read-only queue inspection, branch setup, and native
worktree leases.
`)
}

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "preview installed files")
	apply := fs.Bool("apply", false, "write installed files")
	profile := fs.String("profile", "default", "template profile")
	goInstall := fs.String("go-install", "", "Go install target for Baton in generated workflows")
	installCommand := fs.String("install-command", "", "full Baton install command for generated workflows")
	yes := fs.Bool("yes", false, "overwrite changed files when applying")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "init requires exactly one of --dry-run or --apply", "Run `baton init --dry-run` to preview or `baton init --apply` to write files.")
	}
	if *profile != "default" {
		return out.ErrorMessage(exitUsage, "init currently supports only --profile default", "")
	}
	if *goInstall != "" && *installCommand != "" {
		return out.ErrorMessage(exitUsage, "init accepts only one of --go-install or --install-command", "")
	}
	options := install.Options{GoInstall: *goInstall, InstallCommand: *installCommand}
	var (
		plan install.Plan
		err  error
	)
	if *apply {
		plan, err = install.ApplyWithOptions(".", *yes, options)
	} else {
		plan, err = install.PreviewWithOptions(".", options)
	}
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	if *jsonOut {
		return out.JSON(plan)
	}
	for _, change := range plan.Changes {
		fmt.Fprintf(stdout, "%s %s\n", change.Action, change.Path)
	}
	return exitOK
}

func runMigrateConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate-config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", ".github/agent-issue-policy.yml", "legacy policy config path")
	to := fs.String("to", ".github/baton.yml", "Baton config output path")
	dryRun := fs.Bool("dry-run", false, "preview migrated config")
	apply := fs.Bool("apply", false, "write migrated config")
	yes := fs.Bool("yes", false, "overwrite changed target")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "migrate-config requires exactly one of --dry-run or --apply", "Run `baton migrate-config --dry-run` to preview or `baton migrate-config --apply` to write.")
	}
	cfg, err := config.Load(*from)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	action := "create"
	if existing, err := os.ReadFile(*to); err == nil {
		if string(existing) == string(content) {
			action = "unchanged"
		} else {
			action = "overwrite"
		}
	} else if !os.IsNotExist(err) {
		return out.Error(exitConfig, err, "")
	}
	result := struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		From          string `json:"from"`
		To            string `json:"to"`
		Action        string `json:"action"`
		Content       string `json:"content,omitempty"`
	}{SchemaVersion: 1, Kind: "configMigration", From: *from, To: *to, Action: action}
	if *dryRun {
		result.Content = string(content)
		if *jsonOut {
			return out.JSON(result)
		}
		fmt.Fprintf(stdout, "%s %s from %s\n\n%s", action, *to, *from, content)
		return exitOK
	}
	if action == "overwrite" && !*yes {
		return out.ErrorMessage(exitConfig, fmt.Sprintf("%s already exists with different content; rerun with --yes to overwrite", *to), "")
	}
	if action != "unchanged" {
		if err := os.MkdirAll(filepath.Dir(*to), 0o755); err != nil {
			return out.Error(exitConfig, err, "")
		}
		if err := os.WriteFile(*to, content, 0o644); err != nil {
			return out.Error(exitConfig, err, "")
		}
	}
	if *jsonOut {
		return out.JSON(result)
	}
	fmt.Fprintf(stdout, "%s %s\n", action, *to)
	return exitOK
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	result := doctor.Run(*configPath)
	if *jsonOut {
		if code := out.JSON(result); code != exitOK {
			return code
		}
	} else {
		for _, check := range result.Checks {
			if check.Message == "" {
				fmt.Fprintf(stdout, "%s: %s\n", check.Name, check.Status)
			} else {
				fmt.Fprintf(stdout, "%s: %s (%s)\n", check.Name, check.Status, check.Message)
			}
		}
	}
	if result.Failed() {
		return exitLocalGit
	}
	return exitOK
}

func runIssuePolicy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue-policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bodyFile := fs.String("body-file", "", "issue body markdown file")
	eventPath := fs.String("event", "", "GitHub issue event payload")
	labelsCSV := fs.String("labels", "", "comma-separated current labels")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	apply := fs.Bool("apply", false, "apply labels and policy comment to GitHub")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if (*bodyFile == "") == (*eventPath == "") {
		return out.ErrorMessage(exitUsage, "issue-policy requires exactly one of --body-file or --event", "Run `baton issue-policy --body-file <path>` or `baton issue-policy --event <path>`.")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	body := ""
	currentLabels := splitCSV(*labelsCSV)
	eventIssueNumber := 0
	eventRepo := ""
	if *eventPath != "" {
		content, err := os.ReadFile(*eventPath)
		if err != nil {
			return out.ErrorMessage(exitUsage, fmt.Sprintf("read issue event: %v", err), "")
		}
		event, err := gh.ParseIssueEvent(content)
		if err != nil {
			return out.Error(exitUsage, err, "")
		}
		body = event.Body
		currentLabels = event.Labels
		eventIssueNumber = event.Number
		eventRepo = event.Repository
	} else {
		content, err := os.ReadFile(*bodyFile)
		if err != nil {
			return out.ErrorMessage(exitUsage, fmt.Sprintf("read issue body: %v", err), "")
		}
		body = string(content)
	}

	decision := policy.ComputeIssuePolicy(policy.IssuePolicyInput{
		Body:          body,
		CurrentLabels: currentLabels,
		Policy:        cfg.IssuePolicy,
	})
	if !decision.IsFormIssue {
		if !*jsonOut {
			fmt.Fprintln(stdout, "Issue policy: body does not match the configured form.")
		}
	} else if !*jsonOut {
		fmt.Fprintf(stdout, "Issue policy: add %s; remove %s\n", strings.Join(decision.LabelsToAdd, ", "), strings.Join(decision.LabelsToRemove, ", "))
		if len(decision.MissingRequiredSections) > 0 {
			fmt.Fprintf(stdout, "Missing required sections: %s\n", strings.Join(decision.MissingRequiredSections, ", "))
		}
	}
	if code := applyIssueDecisionIfRequested(*apply, *eventPath, *repoFlag, eventRepo, eventIssueNumber, decision, cfg, out); code != exitOK {
		return code
	}
	if *jsonOut {
		return out.JSON(decision)
	}
	return exitOK
}

func runPRPolicy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr-policy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fixturePath := fs.String("fixture", "", "pure PR policy fixture JSON")
	eventPath := fs.String("event", "", "GitHub pull_request event payload")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if (*fixturePath == "") == (*eventPath == "") {
		return out.ErrorMessage(exitUsage, "pr-policy requires exactly one of --fixture or --event", "Run `baton pr-policy --fixture <path>` or `baton pr-policy --event <path>`.")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	var input policy.PRPolicyInput
	if *fixturePath != "" {
		content, err := os.ReadFile(*fixturePath)
		if err != nil {
			return out.ErrorMessage(exitUsage, fmt.Sprintf("read PR policy fixture: %v", err), "")
		}
		if err := json.Unmarshal(content, &input); err != nil {
			return out.ErrorMessage(exitUsage, fmt.Sprintf("parse PR policy fixture: %v", err), "")
		}
	} else {
		content, err := os.ReadFile(*eventPath)
		if err != nil {
			return out.ErrorMessage(exitUsage, fmt.Sprintf("read PR event: %v", err), "")
		}
		pr, err := gh.ParsePullRequestEvent(content)
		if err != nil {
			return out.Error(exitUsage, err, "")
		}
		input.PullRequest = pr
		repo := firstNonEmpty(*repoFlag, pr.BaseRepositoryFullName)
		if repo == "" {
			return out.ErrorMessage(exitUsage, "--repo, GITHUB_REPOSITORY, or pull_request.base.repo.full_name is required", "")
		}
		client, err := gh.NewClientFromEnv()
		if err != nil {
			return out.Error(exitAuth, err, "")
		}
		issueNumbers := gh.IssueNumbersForPR(pr)
		if len(issueNumbers) > 0 {
			issues, err := client.FetchIssueLabels(repo, issueNumbers)
			if err != nil {
				return out.Error(exitGitHub, err, "")
			}
			input.ReferencedIssues = issues
		}
		messages, reachedCap, err := client.FetchCommitListing(repo, pr.Number)
		if err != nil {
			return out.Error(exitGitHub, err, "")
		}
		input.CommitMessages = messages
		input.CommitListingReachedCap = reachedCap
	}
	input.Policy = cfg
	decision := policy.ComputePullRequestPolicy(input)
	if *jsonOut {
		if code := out.JSON(decision); code != exitOK {
			return code
		}
		if len(decision.Errors) > 0 {
			return exitPolicy
		}
		return exitOK
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

func runSyncLabels(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync-labels", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "preview label changes")
	apply := fs.Bool("apply", false, "apply label changes")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	labelsFile := fs.String("labels-file", ".github/labels.yml", "labels manifest path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "sync-labels requires exactly one of --dry-run or --apply", "Run `baton sync-labels --dry-run` to preview or `baton sync-labels --apply` to update GitHub labels.")
	}
	repo, err := gh.RepoFromEnvOrFlag(*repoFlag)
	if err != nil {
		return out.Error(exitUsage, err, "")
	}
	manifest, err := labels.LoadManifest(*labelsFile)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return out.Error(exitAuth, err, "")
	}
	existing, err := client.ListLabels(repo)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	plan := labels.PlanSync(repo, manifest.Labels, existing)
	if *apply {
		for _, change := range plan.Changes {
			switch change.Action {
			case "create":
				if err := client.CreateLabel(repo, labels.Label{Name: change.Name, Color: change.Color, Description: change.Description}); err != nil {
					return out.Error(exitGitHub, err, "")
				}
			case "update":
				if err := client.UpdateLabel(repo, labels.Label{Name: change.Name, Color: change.Color, Description: change.Description}); err != nil {
					return out.Error(exitGitHub, err, "")
				}
			}
		}
	}
	if *jsonOut {
		return out.JSON(plan)
	}
	if len(plan.Changes) == 0 {
		fmt.Fprintln(stdout, "changes[0]:")
		return exitOK
	}
	for _, change := range plan.Changes {
		fmt.Fprintf(stdout, "%s %s\n", change.Action, change.Name)
	}
	return exitOK
}

func runQueue(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, false, out)
	if code != exitOK {
		return code
	}
	if *jsonOut {
		return out.JSON(snapshot)
	}
	if len(snapshot.Issues) == 0 {
		fmt.Fprintln(stdout, "issues[0]:")
		fmt.Fprintln(stdout, "help[1]: Run `baton next --json`")
		return exitOK
	}
	for _, issue := range snapshot.Issues {
		fmt.Fprintf(stdout, "#%d eligible=%v %s\n", issue.Issue.Number, issue.Eligible, strings.Join(issue.Reasons, ", "))
	}
	return exitOK
}

func runPRs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, true, out)
	if code != exitOK {
		return code
	}
	result := buildPullRequestsResult(snapshot.Repo, snapshot.PullRequests)
	if *jsonOut {
		return out.JSON(result)
	}
	if len(result.PullRequests) == 0 {
		fmt.Fprintln(stdout, "pullRequests[0]:")
		fmt.Fprintln(stdout, "help[1]: Run `baton queue --json`")
		return exitOK
	}
	for _, pr := range result.PullRequests {
		fmt.Fprintf(stdout, "#%d %s checks=%s\n", pr.PullRequest.Number, pr.PullRequest.HeadRef, pr.PullRequest.CheckState)
	}
	return exitOK
}

func runPR(args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("pr", args, stdout, stderr)
	out := newRenderer(stdout, stderr, flags.json)
	if code != exitOK {
		return code
	}
	if code := validateOptionalConfig(flags.config, out); code != exitOK {
		return code
	}
	repo, client, code := githubClientForRepo(flags.repo, out)
	if code != exitOK {
		return code
	}
	pr, err := client.GetPullRequest(repo, number)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	checks, err := client.GetCheckRollup(repo, pr)
	if err == nil {
		pr.CheckState = checks.State
	}
	if flags.json {
		return out.JSON(struct {
			SchemaVersion int               `json:"schemaVersion"`
			Kind          string            `json:"kind"`
			Repo          string            `json:"repo"`
			PullRequest   queue.PullRequest `json:"pullRequest"`
		}{SchemaVersion: 1, Kind: "pullRequest", Repo: repo, PullRequest: pr})
	}
	fmt.Fprintf(stdout, "#%d %s -> %s checks=%s\n", pr.Number, pr.HeadRef, pr.BaseRef, pr.CheckState)
	return exitOK
}

func runChecks(args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("checks", args, stdout, stderr)
	out := newRenderer(stdout, stderr, flags.json)
	if code != exitOK {
		return code
	}
	if code := validateOptionalConfig(flags.config, out); code != exitOK {
		return code
	}
	repo, client, code := githubClientForRepo(flags.repo, out)
	if code != exitOK {
		return code
	}
	pr, err := client.GetPullRequest(repo, number)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	rollup, err := client.GetCheckRollup(repo, pr)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	if flags.json {
		return out.JSON(rollup)
	}
	fmt.Fprintf(stdout, "PR #%d checks: %s\n", number, rollup.State)
	if len(rollup.Checks) == 0 {
		fmt.Fprintln(stdout, "checks[0]:")
		return exitOK
	}
	for _, check := range rollup.Checks {
		fmt.Fprintf(stdout, "- %s %s %s\n", check.Name, check.Status, check.Conclusion)
	}
	return exitOK
}

func runReviewThreads(args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("review-threads", args, stdout, stderr)
	out := newRenderer(stdout, stderr, flags.json)
	if code != exitOK {
		return code
	}
	if code := validateOptionalConfig(flags.config, out); code != exitOK {
		return code
	}
	repo, client, code := githubClientForRepo(flags.repo, out)
	if code != exitOK {
		return code
	}
	threads, err := client.GetReviewThreads(repo, number)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	if flags.json {
		return out.JSON(threads)
	}
	if len(threads.Threads) == 0 {
		fmt.Fprintln(stdout, "reviewThreads[0]:")
		fmt.Fprintf(stdout, "help[1]: Run `baton pr %d --json`\n", number)
		return exitOK
	}
	for _, thread := range threads.Threads {
		fmt.Fprintf(stdout, "%s:%d resolved=%v outdated=%v\n", thread.Path, thread.Line, thread.IsResolved, thread.IsOutdated)
	}
	return exitOK
}

func runNext(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, true, out)
	if code != exitOK {
		return code
	}
	next := queue.RecommendNext(snapshot)
	if *jsonOut {
		return out.JSON(next)
	}
	fmt.Fprintf(stdout, "Next action: %s\nReason: %s\n", next.Action, next.Reason)
	return exitOK
}

func runLease(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lease", flag.ContinueOnError)
	fs.SetOutput(stderr)
	purpose := fs.String("purpose", "", "lease purpose")
	branch := fs.String("branch", "", "existing branch/ref")
	base := fs.String("base", "", "base ref for new branch")
	newBranch := fs.String("new-branch", "", "new branch name")
	repo := fs.String("repo", "", "repository owner/name for lease metadata")
	repoName := fs.String("repo-name", "", "repository name for lease metadata")
	stateRoot := fs.String("state-root", "", "Baton state root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	manager := lease.NewManager(*stateRoot)
	record, err := manager.Acquire(lease.AcquireRequest{
		Purpose:   *purpose,
		BaseRef:   *base,
		HeadRef:   *branch,
		NewBranch: *newBranch,
		Repo:      firstNonEmpty(*repo, *repoName),
	})
	if err != nil {
		return out.Error(exitLocalGit, err, "")
	}
	if *jsonOut {
		return out.JSON(record)
	}
	fmt.Fprintf(stdout, "Lease %s: %s\n", record.ID, record.WorktreePath)
	return exitOK
}

func runRelease(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	leaseID := fs.String("lease", "", "lease id")
	path := fs.String("path", "", "lease worktree path")
	keepDirty := fs.Bool("keep-dirty", false, "mark dirty lease released")
	stateRoot := fs.String("state-root", "", "Baton state root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if (*leaseID == "") == (*path == "") {
		return out.ErrorMessage(exitUsage, "release requires exactly one of --lease or --path", "Run `baton release --lease <id>` or `baton release --path <path>`.")
	}
	manager := lease.NewManager(*stateRoot)
	var (
		result lease.ReleaseResult
		err    error
	)
	if *leaseID != "" {
		result, err = manager.ReleaseByID(*leaseID, *keepDirty)
	} else {
		result, err = manager.ReleaseByPath(*path, *keepDirty)
	}
	if err != nil {
		if result.Dirty && !out.structured {
			out.JSON(result)
		}
		return out.Error(exitLocalGit, err, "")
	}
	if *jsonOut {
		return out.JSON(result)
	}
	fmt.Fprintf(stdout, "Released %s\n", result.Lease.ID)
	return exitOK
}

func runLeases(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("leases", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateRoot := fs.String("state-root", "", "Baton state root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	records, err := lease.NewManager(*stateRoot).List()
	if err != nil {
		return out.Error(exitLocalGit, err, "")
	}
	result := buildLeasesResult(records)
	if *jsonOut {
		return out.JSON(result)
	}
	if len(result.Leases) == 0 {
		fmt.Fprintln(stdout, "leases[0]:")
		fmt.Fprintln(stdout, "help[1]: Run `baton lease --purpose <purpose> --base <ref> --new-branch <ref>`")
		return exitOK
	}
	for _, record := range result.Leases {
		fmt.Fprintf(stdout, "%s %s %s\n", record.ID, record.Status, record.WorktreePath)
	}
	return exitOK
}

func runPrune(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "preview prune candidates")
	yes := fs.Bool("yes", false, "remove clean managed prune candidates")
	stateRoot := fs.String("state-root", "", "Baton state root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *yes {
		return out.ErrorMessage(exitUsage, "prune requires exactly one of --dry-run or --yes", "Run `baton prune --dry-run` to preview or `baton prune --yes` to remove safe candidates.")
	}
	manager := lease.NewManager(*stateRoot)
	if *dryRun {
		plan, err := manager.PruneDryRun(time.Now().UTC())
		if err != nil {
			return out.Error(exitLocalGit, err, "")
		}
		if *jsonOut {
			return out.JSON(plan)
		}
		if len(plan.Candidates) == 0 {
			fmt.Fprintln(stdout, "candidates[0]:")
			return exitOK
		}
		for _, record := range plan.Candidates {
			fmt.Fprintf(stdout, "%s %s\n", record.ID, record.Status)
		}
		return exitOK
	}
	result, err := manager.Prune(time.Now().UTC())
	if err != nil {
		return out.Error(exitLocalGit, err, "")
	}
	if *jsonOut {
		return out.JSON(result)
	}
	if len(result.Removed) == 0 && len(result.Skipped) == 0 {
		fmt.Fprintln(stdout, "removed[0]:")
		fmt.Fprintln(stdout, "skipped[0]:")
		return exitOK
	}
	for _, record := range result.Removed {
		fmt.Fprintf(stdout, "pruned %s\n", record.ID)
	}
	for _, skipped := range result.Skipped {
		fmt.Fprintf(stdout, "skipped %s: %s\n", skipped.Lease.ID, skipped.Reason)
	}
	return exitOK
}

func runComplete(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	leaseID := fs.String("lease", "", "lease id")
	summary := fs.String("summary", "", "completion summary")
	validation := fs.String("validation", "", "validation performed")
	comment := fs.Bool("comment", false, "post completion as a GitHub issue/PR comment")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	issueNumber := fs.Int("issue", 0, "issue number to comment on")
	prNumber := fs.Int("pr", 0, "PR number to comment on")
	stateRoot := fs.String("state-root", "", "Baton state root")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	record, err := complete.Write(*stateRoot, *leaseID, *summary, *validation, time.Now().UTC())
	if err != nil {
		return out.Error(exitUsage, err, "")
	}
	if *comment {
		target := *issueNumber
		if *prNumber != 0 {
			if target != 0 {
				return out.ErrorMessage(exitUsage, "complete --comment requires only one of --issue or --pr", "")
			}
			target = *prNumber
		}
		if target == 0 {
			return out.ErrorMessage(exitUsage, "complete --comment requires --issue or --pr", "")
		}
		repo, client, code := githubClientForRepo(*repoFlag, out)
		if code != exitOK {
			return code
		}
		if err := client.CreateIssueComment(repo, target, complete.CommentBody(record)); err != nil {
			return out.Error(exitGitHub, err, "")
		}
	}
	if *jsonOut {
		return out.JSON(record)
	}
	fmt.Fprintf(stdout, "Recorded completion %s\n", record.ID)
	return exitOK
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
	apply := fs.Bool("apply", false, "run planned git commands")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	input := git.AgentBranchPlanInput{
		Remote:              *remote,
		BaseBranch:          *base,
		TargetBranch:        *target,
		RemoteBaseSHA:       *remoteBase,
		RemoteTargetSHA:     *remoteTarget,
		LocalTargetSHA:      *localTarget,
		LocalTargetUpstream: *localUpstream,
	}
	if *remoteBase == "" && *remoteTarget == "" && *localTarget == "" && *localUpstream == "" {
		inspected, err := git.InspectAgentBranchRefs(input)
		if err != nil {
			return out.Error(exitLocalGit, err, "")
		}
		input = inspected
	}
	plan := git.ComputeAgentBranchPlan(input)
	if *jsonOut {
		if code := out.JSON(plan); code != exitOK {
			return code
		}
	}
	if !*jsonOut {
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
	}
	if len(plan.Errors) > 0 {
		return exitLocalGit
	}
	if len(plan.ApplyCommands) == 0 {
		if !*jsonOut {
			fmt.Fprintln(stdout, "No branch setup commands are needed.")
		}
		return exitOK
	}
	if *apply {
		if err := git.ApplyAgentBranchPlan(plan); err != nil {
			return out.Error(exitLocalGit, err, "")
		}
		return exitOK
	}
	if !*jsonOut {
		fmt.Fprintln(stdout, "Dry run. Would run:")
		for _, command := range plan.ApplyCommands {
			fmt.Fprintf(stdout, "- %s\n", command.Description)
			fmt.Fprintf(stdout, "  git %s\n", strings.Join(command.Args, " "))
		}
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
	out := newRenderer(stdout, stderr, *jsonOut)
	manifest, err := labels.LoadManifest(*path)
	if err != nil {
		return out.Error(exitConfig, err, "")
	}
	if *jsonOut {
		return out.JSON(manifest)
	}
	if len(manifest.Labels) == 0 {
		fmt.Fprintln(stdout, "labels[0]:")
		return exitOK
	}
	for _, label := range manifest.Labels {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", label.Name, label.Color, label.Description)
	}
	return exitOK
}

func applyIssueDecisionIfRequested(apply bool, eventPath, repoFlag, eventRepo string, issueNumber int, decision policy.IssuePolicyDecision, cfg config.Config, out renderer) int {
	if !apply {
		return exitOK
	}
	if eventPath == "" {
		return out.ErrorMessage(exitUsage, "issue-policy --apply requires --event", "")
	}
	repo := firstNonEmpty(repoFlag, eventRepo, os.Getenv("GITHUB_REPOSITORY"))
	if repo == "" || issueNumber == 0 {
		return out.ErrorMessage(exitUsage, "issue-policy --apply requires a repository and issue number", "")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return out.Error(exitAuth, err, "")
	}
	if err := client.ApplyIssueDecision(repo, issueNumber, decision, cfg.IssuePolicy.PolicyCommentMarker); err != nil {
		return out.Error(exitGitHub, err, "")
	}
	return exitOK
}

type numberFlags struct {
	repo   string
	config string
	json   bool
}

func parseNumberCommand(name string, args []string, stdout, stderr io.Writer) (int, numberFlags, int) {
	structured := hasFlag(args, "json")
	out := newRenderer(stdout, stderr, structured)
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, numberFlags{json: structured}, out.ErrorMessage(exitUsage, fmt.Sprintf("%s requires a number", name), fmt.Sprintf("Run `baton %s <number> --json`.", name))
	}
	number, err := gh.IssueNumberFromString(args[0])
	if err != nil {
		return 0, numberFlags{json: structured}, out.ErrorMessage(exitUsage, fmt.Sprintf("%s number: %v", name, err), "")
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return 0, numberFlags{json: structured}, out.Error(exitUsage, err, "")
	}
	return number, numberFlags{repo: *repoFlag, config: *configPath, json: *jsonOut}, exitOK
}

func validateOptionalConfig(path string, out renderer) int {
	if path == "" {
		return exitOK
	}
	if _, err := loadConfig(path); err != nil {
		return out.Error(exitConfig, err, "")
	}
	return exitOK
}

func fetchQueueSnapshot(repoFlag, configPath string, includeChecks bool, out renderer) (queue.Snapshot, int) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return queue.Snapshot{}, out.Error(exitConfig, err, "")
	}
	repo, client, code := githubClientForRepo(repoFlag, out)
	if code != exitOK {
		return queue.Snapshot{}, code
	}
	issues, err := client.ListOpenIssues(repo)
	if err != nil {
		return queue.Snapshot{}, out.Error(exitGitHub, err, "")
	}
	prs, err := client.ListOpenPullRequests(repo, cfg.Repository.StagingBranch)
	if err != nil {
		return queue.Snapshot{}, out.Error(exitGitHub, err, "")
	}
	if cfg.Repository.BaseBranch != "" && cfg.Repository.BaseBranch != cfg.Repository.StagingBranch {
		promotionPRs, err := client.ListOpenPullRequests(repo, cfg.Repository.BaseBranch)
		if err != nil {
			return queue.Snapshot{}, out.Error(exitGitHub, err, "")
		}
		for _, pr := range promotionPRs {
			if pr.HeadRef == cfg.Repository.StagingBranch {
				prs = append(prs, pr)
			}
		}
	}
	if includeChecks {
		for i := range prs {
			rollup, err := client.GetCheckRollup(repo, prs[i])
			if err != nil {
				return queue.Snapshot{}, out.Error(exitGitHub, err, "")
			}
			prs[i].CheckState = rollup.State
		}
	}
	branchHealth, err := client.GetBranchHealth(repo, cfg.Repository.StagingBranch)
	if err != nil {
		return queue.Snapshot{}, out.Error(exitGitHub, err, "")
	}
	return queue.BuildSnapshotWithBranchHealth(repo, cfg, issues, prs, branchHealth), exitOK
}

func githubClientForRepo(repoFlag string, out renderer) (string, *gh.Client, int) {
	repo, err := gh.RepoFromEnvOrFlag(repoFlag)
	if err != nil {
		return "", nil, out.Error(exitUsage, err, "")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return "", nil, out.Error(exitAuth, err, "")
	}
	return repo, client, exitOK
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
		return config.Config{}, err
	}
	return config.Config{}, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

func hasFlag(args []string, name string) bool {
	long := "--" + name
	for _, arg := range args {
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func buildPullRequestsResult(repo string, prs []queue.PullState) pullRequestsResult {
	return pullRequestsResult{
		SchemaVersion: schemaVersionV1,
		Kind:          "pullRequests",
		Repo:          repo,
		Count:         len(prs),
		Counts:        countPullRequests(prs),
		PullRequests:  prs,
		Help:          pullRequestsHelp(prs),
	}
}

func countPullRequests(prs []queue.PullState) pullRequestCounts {
	counts := pullRequestCounts{}
	for _, pr := range prs {
		switch pr.PullRequest.CheckState {
		case "success":
			counts.Success++
		case "failure":
			counts.Failure++
		case "pending":
			counts.Pending++
		default:
			counts.Unknown++
		}
	}
	return counts
}

func pullRequestsHelp(prs []queue.PullState) []string {
	if len(prs) == 0 {
		return []string{"Run `baton queue --json` to inspect eligible issue work."}
	}
	return []string{
		"Run `baton pr <number> --json` for PR details.",
		"Run `baton checks <number> --json` to inspect check status.",
		"Run `baton review-threads <number> --json` before completing review work.",
	}
}

func buildLeasesResult(records []lease.Record) leasesResult {
	return leasesResult{
		SchemaVersion: schemaVersionV1,
		Kind:          "leases",
		Count:         len(records),
		Counts:        countLeases(records),
		Leases:        records,
		Help:          leasesHelp(records),
	}
}

func countLeases(records []lease.Record) leaseCounts {
	counts := leaseCounts{}
	for _, record := range records {
		switch record.Status {
		case "active":
			counts.Active++
		case "released":
			counts.Released++
		case "pruned":
			counts.Pruned++
		}
	}
	return counts
}

func leasesHelp(records []lease.Record) []string {
	if len(records) == 0 {
		return []string{"Run `baton lease --purpose <purpose> --base <ref> --new-branch <ref> --json` to acquire a worktree lease."}
	}
	return []string{
		"Run `baton release --lease <id>` when work is complete.",
		"Run `baton prune --dry-run --json` to inspect cleanup candidates.",
	}
}

func exitCategory(code int) string {
	switch code {
	case exitPolicy:
		return "policy"
	case exitUsage:
		return "usage"
	case exitConfig:
		return "config"
	case exitAuth:
		return "auth"
	case exitGitHub:
		return "github"
	case exitLocalGit:
		return "localGit"
	default:
		return "unknown"
	}
}

func errorRetryable(code int) bool {
	return code == exitGitHub
}

func defaultErrorHint(code int, message string) string {
	switch code {
	case exitUsage:
		return "Run `baton --help` or the command with `--help`."
	case exitConfig:
		if strings.Contains(message, config.ErrConfigNotFound.Error()) {
			return "Run `baton init --dry-run` or pass `--config <path>`."
		}
		return "Check the Baton config path and contents, then retry."
	case exitAuth:
		return "Run `gh auth status` and ensure GitHub authentication is available."
	case exitGitHub:
		return "Check GitHub API access and retry after the upstream issue is resolved."
	case exitLocalGit:
		return "Inspect the local git or worktree state, then retry."
	case exitPolicy:
		return "Inspect the policy decision output before continuing."
	default:
		return ""
	}
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "encode JSON: %v\n", err)
		return exitUsage
	}
	return exitOK
}
