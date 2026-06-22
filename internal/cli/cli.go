package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
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

	defaultReviewThreadBodyLimit = 4096
	defaultDocumentBodyLimit     = 4096
)

type renderer struct {
	stdout io.Writer
	stderr io.Writer
	format outputFormat
}

type outputFormat string

const (
	formatText outputFormat = "text"
	formatJSON outputFormat = "json"
	formatTOON outputFormat = "toon"
)

type formatFlags struct {
	json   *bool
	format *string
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

type configMigrationResult struct {
	SchemaVersion    int    `json:"schemaVersion"`
	Kind             string `json:"kind"`
	From             string `json:"from"`
	To               string `json:"to"`
	Action           string `json:"action"`
	Content          string `json:"content,omitempty"`
	ContentChars     int    `json:"contentChars,omitempty"`
	ContentTruncated bool   `json:"contentTruncated,omitempty"`
	ContentPreview   string `json:"contentPreview,omitempty"`
	FullCommand      string `json:"fullCommand,omitempty"`
}

type completionResult struct {
	SchemaVersion       int       `json:"schemaVersion"`
	Kind                string    `json:"kind"`
	ID                  string    `json:"id"`
	LeaseID             string    `json:"leaseId,omitempty"`
	Summary             string    `json:"summary"`
	SummaryChars        int       `json:"summaryChars"`
	SummaryTruncated    bool      `json:"summaryTruncated"`
	SummaryPreview      string    `json:"summaryPreview,omitempty"`
	Validation          string    `json:"validation,omitempty"`
	ValidationChars     int       `json:"validationChars,omitempty"`
	ValidationTruncated bool      `json:"validationTruncated,omitempty"`
	ValidationPreview   string    `json:"validationPreview,omitempty"`
	FullCommand         string    `json:"fullCommand,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

type homeResult struct {
	SchemaVersion int        `json:"schemaVersion"`
	Kind          string     `json:"kind"`
	Bin           string     `json:"bin"`
	Description   string     `json:"description"`
	Repo          string     `json:"repo"`
	Config        string     `json:"config"`
	Auth          string     `json:"auth"`
	Leases        homeLeases `json:"leases"`
	Next          string     `json:"next"`
	Help          []string   `json:"help"`
}

type homeLeases struct {
	Active int `json:"active"`
	Total  int `json:"total"`
}

func newRenderer(stdout, stderr io.Writer, structured bool) renderer {
	if structured {
		return renderer{stdout: stdout, stderr: stderr, format: formatJSON}
	}
	return renderer{stdout: stdout, stderr: stderr, format: formatText}
}

func newFormatRenderer(stdout, stderr io.Writer, format outputFormat) renderer {
	return renderer{stdout: stdout, stderr: stderr, format: format}
}

func addFormatFlags(fs *flag.FlagSet) formatFlags {
	return formatFlags{
		json:   fs.Bool("json", false, "emit JSON"),
		format: fs.String("format", "", "output format: text, json, or toon"),
	}
}

func resolveFormat(flags formatFlags) (outputFormat, error) {
	if flags.format == nil || strings.TrimSpace(*flags.format) == "" {
		if flags.json != nil && *flags.json {
			return formatJSON, nil
		}
		return formatText, nil
	}
	value := outputFormat(strings.ToLower(strings.TrimSpace(*flags.format)))
	switch value {
	case formatText, formatJSON, formatTOON:
	default:
		return "", fmt.Errorf("--format must be one of text, json, or toon")
	}
	if flags.json != nil && *flags.json && value != formatJSON {
		return "", fmt.Errorf("--json cannot be combined with --format %s", value)
	}
	return value, nil
}

func rendererFromFormatFlags(stdout, stderr io.Writer, flags formatFlags) (renderer, outputFormat, int) {
	format, err := resolveFormat(flags)
	if err == nil {
		return newFormatRenderer(stdout, stderr, format), format, exitOK
	}
	out := newFormatRenderer(stdout, stderr, fallbackFormat(flags))
	return out, formatText, out.Error(exitUsage, err, "")
}

func fallbackFormat(flags formatFlags) outputFormat {
	if flags.json != nil && *flags.json {
		return formatJSON
	}
	if flags.format == nil {
		return formatText
	}
	switch outputFormat(strings.ToLower(strings.TrimSpace(*flags.format))) {
	case formatJSON:
		return formatJSON
	case formatTOON:
		return formatTOON
	default:
		return formatText
	}
}

func (r renderer) Structured() bool {
	return r.format == formatJSON || r.format == formatTOON
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
	if !r.Structured() {
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
	if r.format == formatTOON {
		return r.TOONError(result, code)
	}
	if writeCode := r.JSON(result); writeCode != exitOK {
		return writeCode
	}
	return code
}

func (r renderer) TOONError(result errorResult, code int) int {
	fmt.Fprintln(r.stdout, "kind: error")
	fmt.Fprintf(r.stdout, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(r.stdout, "category: %s\n", result.Category)
	fmt.Fprintf(r.stdout, "exitCode: %d\n", result.ExitCode)
	fmt.Fprintf(r.stdout, "message: %s\n", result.Message)
	if result.Hint != "" {
		fmt.Fprintf(r.stdout, "hint: %s\n", result.Hint)
	}
	fmt.Fprintf(r.stdout, "retryable: %v\n", result.Retryable)
	return code
}

// Run executes the Baton command line. It is small by design: command packages
// own deterministic decisions, and this layer only parses flags and renders.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return exitOK
	}
	if args[0] == "help" {
		if len(args) == 1 {
			printHelp(stdout)
			return exitOK
		}
		if len(args) > 2 {
			fmt.Fprintln(stderr, "help accepts at most one command")
			return exitUsage
		}
		if !printCommandHelp(stdout, args[1]) {
			fmt.Fprintf(stderr, "unknown command %q\n", args[1])
			return exitUsage
		}
		return exitOK
	}
	if len(args) >= 2 && (args[1] == "--help" || args[1] == "-h") {
		if !printCommandHelp(stdout, args[0]) {
			fmt.Fprintf(stderr, "unknown command %q\n", args[0])
			return exitUsage
		}
		return exitOK
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return exitOK
	case "home":
		return runHome(args[1:], stdout, stderr)
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
  baton home [--format text|json|toon] [--json]
  baton init --dry-run|--apply [--profile default] [--go-install module@version|--install-command <cmd>] [--yes] [--json]
  baton migrate-config --dry-run|--apply [--from <path>] [--to <path>] [--yes] [--full] [--body-limit <chars>] [--json]
  baton doctor [--config <path>] [--format text|json|toon] [--json]
  baton issue-policy --body-file <path> [--labels a,b] [--config <path>] [--json]
  baton issue-policy --event <path> [--apply] [--repo owner/name] [--config <path>] [--json]
  baton pr-policy --fixture <path> [--config <path>] [--json]
  baton pr-policy --event <path> [--config <path>] [--json]
  baton sync-labels --dry-run|--apply [--repo owner/name] [--labels-file <path>] [--json]
  baton queue [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]
  baton prs [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]
  baton pr <number> --json [--repo owner/name] [--config <path>]
  baton checks <number> [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]
  baton review-threads <number> [--repo owner/name] [--config <path>] [--full] [--body-limit <chars>] [--format text|json|toon] [--json]
  baton next [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]
  baton lease --purpose <purpose> --branch <ref> [--repo owner/name] --json
  baton lease --purpose <purpose> --base <ref> --new-branch <ref> [--repo owner/name] --json
  baton release --lease <id>|--path <path> [--keep-dirty]
  baton leases --json
  baton prune --dry-run|--yes --json
  baton complete --summary <text> [--lease <id>] [--validation <text>] [--comment --repo owner/name --issue N|--pr N] [--full] [--body-limit <chars>] [--json]
  baton ensure-branch [--apply] [--remote origin] [--base main] [--target agent] [--json]
  baton labels --file <path> [--json]

The current implementation includes policy checks, install planning, GitHub
label/policy writes, read-only queue inspection, branch setup, and native
worktree leases.
`)
}

type commandHelp struct {
	Purpose  string
	Usage    string
	Flags    []string
	Examples []string
	Related  []string
}

var commandHelps = map[string]commandHelp{
	"version": {
		Purpose:  "Print the Baton version.",
		Usage:    "baton version",
		Examples: []string{"baton version"},
	},
	"home": {
		Purpose:  "Show a compact local Baton session dashboard.",
		Usage:    "baton home [--format text|json|toon] [--json]",
		Flags:    []string{"--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton home --format toon", "baton home --json"},
		Related:  []string{"baton doctor --format toon", "baton next --format toon"},
	},
	"init": {
		Purpose:  "Preview or install Baton repository automation files.",
		Usage:    "baton init --dry-run|--apply [--profile default] [--go-install module@version|--install-command <cmd>] [--yes] [--json]",
		Flags:    []string{"--dry-run: preview installed files", "--apply: write installed files", "--yes: overwrite changed files when applying", "--json: emit structured JSON"},
		Examples: []string{"baton init --dry-run --json", "baton init --apply --yes"},
		Related:  []string{"baton doctor --json", "baton labels --file internal/install/templates/.github/labels.yml --json"},
	},
	"migrate-config": {
		Purpose:  "Preview or write a Baton config migrated from the legacy policy config.",
		Usage:    "baton migrate-config --dry-run|--apply [--from <path>] [--to <path>] [--yes] [--full] [--body-limit <chars>] [--json]",
		Flags:    []string{"--dry-run: preview migrated config", "--apply: write migrated config", "--from: legacy policy config path", "--to: Baton config output path", "--full: include full generated config content", "--body-limit: maximum default content characters", "--json: emit structured JSON"},
		Examples: []string{"baton migrate-config --dry-run --json", "baton migrate-config --dry-run --full --json", "baton migrate-config --apply --yes"},
		Related:  []string{"baton doctor --config .github/baton.yml --json"},
	},
	"doctor": {
		Purpose:  "Check local Baton prerequisites and repository readiness.",
		Usage:    "baton doctor [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton doctor --format toon", "baton doctor --config .github/baton.yml --json"},
		Related:  []string{"baton init --dry-run --json", "baton queue --json"},
	},
	"issue-policy": {
		Purpose:  "Evaluate issue-form labels and optionally apply the policy result.",
		Usage:    "baton issue-policy --body-file <path>|--event <path> [--labels a,b] [--apply] [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--body-file: issue body markdown file", "--event: GitHub issue event payload", "--apply: apply labels and policy comment", "--json: emit structured JSON"},
		Examples: []string{"baton issue-policy --body-file issue.md --json", "baton issue-policy --event event.json --apply --repo owner/name --json"},
		Related:  []string{"baton queue --json"},
	},
	"pr-policy": {
		Purpose:  "Evaluate pull request policy from a fixture or GitHub event.",
		Usage:    "baton pr-policy --fixture <path>|--event <path> [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--fixture: pure PR policy fixture JSON", "--event: GitHub pull_request event payload", "--json: emit structured JSON"},
		Examples: []string{"baton pr-policy --fixture pr.json --config .github/baton.yml --json"},
		Related:  []string{"baton pr <number> --json", "baton checks <number> --json"},
	},
	"sync-labels": {
		Purpose:  "Compare or apply GitHub repository labels from a labels manifest.",
		Usage:    "baton sync-labels --dry-run|--apply [--repo owner/name] [--labels-file <path>] [--json]",
		Flags:    []string{"--dry-run: preview label changes", "--apply: apply label changes", "--labels-file: labels manifest path", "--json: emit structured JSON"},
		Examples: []string{"baton sync-labels --dry-run --json", "baton sync-labels --apply --repo owner/name --json"},
		Related:  []string{"baton labels --file <path> --json"},
	},
	"queue": {
		Purpose:  "List open issues with Baton eligibility and linked PR state.",
		Usage:    "baton queue [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton queue --format toon", "baton queue --repo owner/name --config .github/baton.yml --json"},
		Related:  []string{"baton next --json", "baton prs --json", "baton lease --purpose <purpose> --base <ref> --new-branch <ref> --json"},
	},
	"prs": {
		Purpose:  "List open pull requests relevant to Baton queue work.",
		Usage:    "baton prs [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton prs --format toon"},
		Related:  []string{"baton pr <number> --json", "baton checks <number> --json", "baton review-threads <number> --json"},
	},
	"pr": {
		Purpose:  "Show a pull request summary.",
		Usage:    "baton pr <number> [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--json: emit structured JSON"},
		Examples: []string{"baton pr 12 --json"},
		Related:  []string{"baton checks <number> --json", "baton review-threads <number> --json"},
	},
	"checks": {
		Purpose:  "Show check rollup state for a pull request.",
		Usage:    "baton checks <number> [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton checks 12 --format toon"},
		Related:  []string{"baton pr <number> --json"},
	},
	"review-threads": {
		Purpose:  "Show pull request review threads and unresolved summary.",
		Usage:    "baton review-threads <number> [--repo owner/name] [--config <path>] [--full] [--body-limit <chars>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--full: include full comment bodies", "--body-limit: maximum default comment body characters", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton review-threads 12 --format toon", "baton review-threads 12 --full --json"},
		Related:  []string{"baton pr <number> --json", "baton checks <number> --json"},
	},
	"next": {
		Purpose:  "Recommend the next Baton action from queue and PR state.",
		Usage:    "baton next [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton next --format toon"},
		Related:  []string{"baton queue --json", "baton lease --purpose <purpose> --base <ref> --new-branch <ref> --json"},
	},
	"lease": {
		Purpose:  "Acquire a managed Baton worktree lease before editing.",
		Usage:    "baton lease --purpose <purpose> --branch <ref>|--base <ref> --new-branch <ref> [--repo owner/name] [--json]",
		Flags:    []string{"--purpose: lease purpose", "--branch: existing branch/ref", "--base: base ref for a new branch", "--new-branch: new branch name", "--json: emit structured JSON"},
		Examples: []string{"baton lease --purpose issue-123 --base origin/agent --new-branch agent-work/issue-123 --json"},
		Related:  []string{"baton leases --json", "baton release --lease <id>"},
	},
	"release": {
		Purpose:  "Release a managed Baton worktree lease.",
		Usage:    "baton release --lease <id>|--path <path> [--keep-dirty] [--json]",
		Flags:    []string{"--lease: lease id", "--path: lease worktree path", "--keep-dirty: mark dirty lease released", "--json: emit structured JSON"},
		Examples: []string{"baton release --lease <id>", "baton release --path <path> --keep-dirty --json"},
		Related:  []string{"baton leases --json", "baton prune --dry-run --json"},
	},
	"leases": {
		Purpose:  "List managed Baton worktree leases.",
		Usage:    "baton leases [--state-root <path>] [--json]",
		Flags:    []string{"--state-root: Baton state root", "--json: emit structured JSON"},
		Examples: []string{"baton leases --json"},
		Related:  []string{"baton lease --purpose <purpose> --base <ref> --new-branch <ref> --json", "baton prune --dry-run --json"},
	},
	"prune": {
		Purpose:  "Preview or remove safe managed worktree prune candidates.",
		Usage:    "baton prune --dry-run|--yes [--state-root <path>] [--json]",
		Flags:    []string{"--dry-run: preview prune candidates", "--yes: remove clean managed candidates", "--state-root: Baton state root", "--json: emit structured JSON"},
		Examples: []string{"baton prune --dry-run --json", "baton prune --yes --json"},
		Related:  []string{"baton leases --json"},
	},
	"complete": {
		Purpose:  "Record completion details and optionally post a GitHub comment.",
		Usage:    "baton complete --summary <text> [--lease <id>] [--validation <text>] [--comment --repo owner/name --issue N|--pr N] [--full] [--body-limit <chars>] [--json]",
		Flags:    []string{"--summary: completion summary", "--validation: validation performed", "--comment: post a GitHub comment", "--full: include full summary and validation text", "--body-limit: maximum default text characters", "--json: emit structured JSON"},
		Examples: []string{"baton complete --summary done --validation 'go test ./...' --json", "baton complete --summary done --full --json"},
		Related:  []string{"baton release --lease <id>"},
	},
	"ensure-branch": {
		Purpose:  "Plan or apply Baton staging branch setup.",
		Usage:    "baton ensure-branch [--apply] [--remote origin] [--base main] [--target agent] [--json]",
		Flags:    []string{"--apply: run planned git commands", "--remote: remote name", "--base: base branch", "--target: staging branch", "--json: emit structured JSON"},
		Examples: []string{"baton ensure-branch --json", "baton ensure-branch --apply"},
		Related:  []string{"baton doctor --json"},
	},
	"labels": {
		Purpose:  "Read and validate a Baton labels manifest.",
		Usage:    "baton labels --file <path> [--json]",
		Flags:    []string{"--file: labels manifest path", "--json: emit structured JSON"},
		Examples: []string{"baton labels --file internal/install/templates/.github/labels.yml --json"},
		Related:  []string{"baton sync-labels --dry-run --json"},
	},
}

func printCommandHelp(w io.Writer, name string) bool {
	help, ok := commandHelps[name]
	if !ok {
		return false
	}
	fmt.Fprintf(w, "baton %s\n\n", name)
	fmt.Fprintf(w, "Purpose:\n  %s\n\n", help.Purpose)
	fmt.Fprintf(w, "Usage:\n  %s\n", help.Usage)
	printHelpList(w, "Flags", help.Flags)
	printHelpList(w, "Examples", help.Examples)
	printHelpList(w, "Related", help.Related)
	return true
}

func printHelpList(w io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(w, "  %s\n", value)
	}
}

func runHome(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("home", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formats := addFormatFlags(fs)
	stateRoot := fs.String("state-root", "", "Baton state root")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	result := buildHomeResult(*stateRoot)
	switch format {
	case formatJSON:
		return out.JSON(result)
	case formatTOON:
		return writeHomeTOON(stdout, result)
	default:
		return writeHomeText(stdout, result)
	}
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
	full := fs.Bool("full", false, "include full generated config content")
	bodyLimit := fs.Int("body-limit", defaultDocumentBodyLimit, "maximum default content characters")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *bodyLimit < 0 {
		return out.ErrorMessage(exitUsage, "migrate-config --body-limit must be non-negative", "")
	}
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
	result := configMigrationResult{SchemaVersion: 1, Kind: "configMigration", From: *from, To: *to, Action: action}
	if *dryRun {
		result = withConfigMigrationContent(result, string(content), *bodyLimit, *full)
		if *jsonOut {
			return out.JSON(result)
		}
		fmt.Fprintf(stdout, "%s %s from %s\n\n%s", action, *to, *from, result.Content)
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
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	result := doctor.Run(*configPath)
	if format == formatJSON {
		if code := out.JSON(result); code != exitOK {
			return code
		}
	} else if format == formatTOON {
		if code := writeDoctorTOON(stdout, result); code != exitOK {
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
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, false, out)
	if code != exitOK {
		return code
	}
	if format == formatJSON {
		return out.JSON(snapshot)
	}
	if format == formatTOON {
		return writeQueueTOON(stdout, snapshot)
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
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, true, out)
	if code != exitOK {
		return code
	}
	result := buildPullRequestsResult(snapshot.Repo, snapshot.PullRequests)
	if format == formatJSON {
		return out.JSON(result)
	}
	if format == formatTOON {
		return writePRsTOON(stdout, result)
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
	out := newFormatRenderer(stdout, stderr, flags.format)
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
	if flags.format == formatJSON {
		return out.JSON(rollup)
	}
	if flags.format == formatTOON {
		return writeChecksTOON(stdout, rollup)
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
	out := newFormatRenderer(stdout, stderr, flags.format)
	if code != exitOK {
		return code
	}
	if code := validateOptionalConfig(flags.config, out); code != exitOK {
		return code
	}
	if flags.bodyLimit < 0 {
		return out.ErrorMessage(exitUsage, "review-threads --body-limit must be non-negative", "")
	}
	repo, client, code := githubClientForRepo(flags.repo, out)
	if code != exitOK {
		return code
	}
	threads, err := client.GetReviewThreads(repo, number)
	if err != nil {
		return out.Error(exitGitHub, err, "")
	}
	threads = truncateReviewThreadBodies(threads, flags.bodyLimit, flags.full)
	if flags.format == formatJSON {
		return out.JSON(threads)
	}
	if flags.format == formatTOON {
		return writeReviewThreadsTOON(stdout, threads)
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
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	snapshot, code := fetchQueueSnapshot(*repoFlag, *configPath, true, out)
	if code != exitOK {
		return code
	}
	next := queue.RecommendNext(snapshot)
	if format == formatJSON {
		return out.JSON(next)
	}
	if format == formatTOON {
		return writeNextTOON(stdout, next)
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
		if result.Dirty && !out.Structured() {
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
	full := fs.Bool("full", false, "include full summary and validation text")
	bodyLimit := fs.Int("body-limit", defaultDocumentBodyLimit, "maximum default text characters")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *bodyLimit < 0 {
		return out.ErrorMessage(exitUsage, "complete --body-limit must be non-negative", "")
	}
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
		return out.JSON(completionResultFromRecord(record, *bodyLimit, *full))
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
	repo      string
	config    string
	json      bool
	format    outputFormat
	full      bool
	bodyLimit int
}

func parseNumberCommand(name string, args []string, stdout, stderr io.Writer) (int, numberFlags, int) {
	rawFormat := outputFormatFromArgs(args)
	out := newFormatRenderer(stdout, stderr, rawFormat)
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, numberFlags{json: rawFormat == formatJSON, format: rawFormat}, out.ErrorMessage(exitUsage, fmt.Sprintf("%s requires a number", name), fmt.Sprintf("Run `baton %s <number> --json`.", name))
	}
	number, err := gh.IssueNumberFromString(args[0])
	if err != nil {
		return 0, numberFlags{json: rawFormat == formatJSON, format: rawFormat}, out.ErrorMessage(exitUsage, fmt.Sprintf("%s number: %v", name, err), "")
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	formats := addFormatFlags(fs)
	full := false
	bodyLimit := defaultReviewThreadBodyLimit
	if name == "review-threads" {
		fs.BoolVar(&full, "full", false, "include full comment bodies")
		fs.IntVar(&bodyLimit, "body-limit", defaultReviewThreadBodyLimit, "maximum default comment body characters")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 0, numberFlags{json: rawFormat == formatJSON, format: rawFormat}, out.Error(exitUsage, err, "")
	}
	format, err := resolveFormat(formats)
	if err != nil {
		out := newFormatRenderer(stdout, stderr, fallbackFormat(formats))
		return 0, numberFlags{json: rawFormat == formatJSON, format: fallbackFormat(formats)}, out.Error(exitUsage, err, "")
	}
	return number, numberFlags{repo: *repoFlag, config: *configPath, json: format == formatJSON, format: format, full: full, bodyLimit: bodyLimit}, exitOK
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

func outputFormatFromArgs(args []string) outputFormat {
	if hasFlag(args, "json") {
		return formatJSON
	}
	for i, arg := range args {
		if strings.HasPrefix(arg, "--format=") {
			value := strings.TrimPrefix(arg, "--format=")
			if value == string(formatJSON) {
				return formatJSON
			}
			if value == string(formatTOON) {
				return formatTOON
			}
			return formatText
		}
		if arg == "--format" && i+1 < len(args) {
			value := args[i+1]
			if value == string(formatJSON) {
				return formatJSON
			}
			if value == string(formatTOON) {
				return formatTOON
			}
			return formatText
		}
	}
	return formatText
}

func buildHomeResult(stateRoot string) homeResult {
	cfgStatus := "missing (.github/baton.yml)"
	if _, err := config.LoadForRepo("."); err == nil {
		cfgStatus = "ok"
	} else if !errors.Is(err, config.ErrConfigNotFound) {
		cfgStatus = "invalid (" + err.Error() + ")"
	}
	records, err := lease.NewManager(stateRoot).List()
	leases := homeLeases{}
	if err == nil {
		leases.Total = len(records)
		for _, record := range records {
			if record.Status == "active" {
				leases.Active++
			}
		}
	}
	next := "run `baton next --format toon`"
	if !strings.HasPrefix(cfgStatus, "ok") {
		next = "unavailable (" + cfgStatus + ")"
	}
	return homeResult{
		SchemaVersion: schemaVersionV1,
		Kind:          "home",
		Bin:           homeRelative(os.Args[0]),
		Description:   "Coordinate GitHub issue/PR agent workflows for this repository",
		Repo:          localRepoName(),
		Config:        cfgStatus,
		Auth:          localAuthStatus(),
		Leases:        leases,
		Next:          next,
		Help: []string{
			"Run `baton init --dry-run --format toon`.",
			"Run `baton doctor --format toon`.",
			"Run `baton --help`.",
		},
	}
}

func localRepoName() string {
	if repo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY")); repo != "" {
		return repo
	}
	remote, err := localGitOutput("remote", "get-url", "origin")
	if err == nil {
		if repo := repoNameFromRemote(strings.TrimSpace(remote)); repo != "" {
			return repo
		}
	}
	root, err := localGitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return "unknown"
	}
	return filepath.Base(strings.TrimSpace(root))
}

func localAuthStatus() string {
	if os.Getenv("GITHUB_TOKEN") != "" || os.Getenv("GH_TOKEN") != "" {
		return "ok (token env)"
	}
	if _, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return "ok (gh auth token)"
	}
	return "missing"
}

func localGitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func repoNameFromRemote(remote string) string {
	remote = strings.TrimSuffix(remote, ".git")
	if strings.HasPrefix(remote, "git@") {
		if idx := strings.Index(remote, ":"); idx >= 0 {
			return strings.TrimPrefix(remote[idx+1:], "/")
		}
	}
	if strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://") {
		parts := strings.Split(remote, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	if strings.Count(remote, "/") == 1 {
		return remote
	}
	return ""
}

func homeRelative(path string) string {
	if path == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(home, abs)
	if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
		return filepath.Join("~", rel)
	}
	if abs == home {
		return "~"
	}
	return abs
}

func writeHomeText(w io.Writer, result homeResult) int {
	fmt.Fprintf(w, "bin: %s\n", result.Bin)
	fmt.Fprintf(w, "description: %s\n", result.Description)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "config: %s\n", result.Config)
	fmt.Fprintf(w, "auth: %s\n", result.Auth)
	fmt.Fprintf(w, "leases: %d active, %d total\n", result.Leases.Active, result.Leases.Total)
	fmt.Fprintf(w, "next: %s\n", result.Next)
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeHomeTOON(w io.Writer, result homeResult) int {
	fmt.Fprintln(w, "kind: home")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "bin: %s\n", result.Bin)
	fmt.Fprintf(w, "description: %s\n", result.Description)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "config: %s\n", result.Config)
	fmt.Fprintf(w, "auth: %s\n", result.Auth)
	fmt.Fprintf(w, "leases.active: %d\n", result.Leases.Active)
	fmt.Fprintf(w, "leases.total: %d\n", result.Leases.Total)
	fmt.Fprintf(w, "next: %s\n", result.Next)
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeDoctorTOON(w io.Writer, result doctor.Result) int {
	fmt.Fprintln(w, "kind: doctor")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "readyState: %s\n", result.ReadyState)
	fmt.Fprintf(w, "counts.ok: %d\n", result.Counts.OK)
	fmt.Fprintf(w, "counts.warn: %d\n", result.Counts.Warn)
	fmt.Fprintf(w, "counts.fail: %d\n", result.Counts.Fail)
	fmt.Fprintf(w, "checks[%d]:\n", len(result.Checks))
	for _, check := range result.Checks {
		line := fmt.Sprintf("  - name=%s status=%s", check.Name, check.Status)
		if check.Message != "" {
			line += " message=" + oneLine(check.Message)
		}
		fmt.Fprintln(w, line)
	}
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeQueueTOON(w io.Writer, snapshot queue.Snapshot) int {
	fmt.Fprintln(w, "kind: queueSnapshot")
	fmt.Fprintf(w, "schemaVersion: %d\n", snapshot.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", snapshot.Repo)
	fmt.Fprintf(w, "counts.totalIssues: %d\n", snapshot.Counts.TotalIssues)
	fmt.Fprintf(w, "counts.eligibleIssues: %d\n", snapshot.Counts.EligibleIssues)
	fmt.Fprintf(w, "counts.skippedIssues: %d\n", snapshot.Counts.SkippedIssues)
	fmt.Fprintf(w, "counts.openPullRequests: %d\n", snapshot.Counts.OpenPullRequests)
	if snapshot.Counts.BranchHealthState != "" {
		fmt.Fprintf(w, "counts.branchHealthState: %s\n", snapshot.Counts.BranchHealthState)
	}
	fmt.Fprintf(w, "issues[%d]:\n", len(snapshot.Issues))
	for _, issue := range snapshot.Issues {
		fmt.Fprintf(w, "  - number=%d eligible=%v action=%s title=%s reasons=%s\n", issue.Issue.Number, issue.Eligible, issue.Action, oneLine(issue.Issue.Title), strings.Join(issue.Reasons, "|"))
	}
	writeHelpLines(w, snapshot.Help)
	return exitOK
}

func writePRsTOON(w io.Writer, result pullRequestsResult) int {
	fmt.Fprintln(w, "kind: pullRequests")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "count: %d\n", result.Count)
	fmt.Fprintf(w, "counts.success: %d\n", result.Counts.Success)
	fmt.Fprintf(w, "counts.failure: %d\n", result.Counts.Failure)
	fmt.Fprintf(w, "counts.pending: %d\n", result.Counts.Pending)
	fmt.Fprintf(w, "counts.unknown: %d\n", result.Counts.Unknown)
	fmt.Fprintf(w, "pullRequests[%d]:\n", len(result.PullRequests))
	for _, pr := range result.PullRequests {
		fmt.Fprintf(w, "  - number=%d headRef=%s baseRef=%s checkState=%s title=%s\n", pr.PullRequest.Number, pr.PullRequest.HeadRef, pr.PullRequest.BaseRef, pr.PullRequest.CheckState, oneLine(pr.PullRequest.Title))
	}
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeChecksTOON(w io.Writer, rollup gh.CheckRollup) int {
	fmt.Fprintln(w, "kind: checkRollup")
	fmt.Fprintf(w, "schemaVersion: %d\n", rollup.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", rollup.Repo)
	fmt.Fprintf(w, "prNumber: %d\n", rollup.PRNumber)
	fmt.Fprintf(w, "state: %s\n", rollup.State)
	fmt.Fprintf(w, "count: %d\n", rollup.Count)
	fmt.Fprintf(w, "summary.passed: %d\n", rollup.Summary.Passed)
	fmt.Fprintf(w, "summary.failed: %d\n", rollup.Summary.Failed)
	fmt.Fprintf(w, "summary.pending: %d\n", rollup.Summary.Pending)
	fmt.Fprintf(w, "summary.skipped: %d\n", rollup.Summary.Skipped)
	fmt.Fprintf(w, "summary.cancelled: %d\n", rollup.Summary.Cancelled)
	fmt.Fprintf(w, "summary.unknown: %d\n", rollup.Summary.Unknown)
	fmt.Fprintf(w, "checks[%d]:\n", len(rollup.Checks))
	for _, check := range rollup.Checks {
		fmt.Fprintf(w, "  - name=%s status=%s conclusion=%s\n", oneLine(check.Name), check.Status, check.Conclusion)
	}
	writeHelpLines(w, rollup.Help)
	return exitOK
}

func writeReviewThreadsTOON(w io.Writer, result gh.ReviewThreadResult) int {
	fmt.Fprintln(w, "kind: reviewThreads")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "prNumber: %d\n", result.PRNumber)
	fmt.Fprintf(w, "count: %d\n", result.Count)
	fmt.Fprintf(w, "summary.total: %d\n", result.Summary.Total)
	fmt.Fprintf(w, "summary.unresolved: %d\n", result.Summary.Unresolved)
	fmt.Fprintf(w, "summary.humanUnresolved: %d\n", result.Summary.HumanUnresolved)
	fmt.Fprintf(w, "summary.botUnresolved: %d\n", result.Summary.BotUnresolved)
	fmt.Fprintf(w, "summary.outdated: %d\n", result.Summary.Outdated)
	fmt.Fprintf(w, "threads[%d]:\n", len(result.Threads))
	for _, thread := range result.Threads {
		fmt.Fprintf(w, "  - path=%s line=%d resolved=%v outdated=%v comments=%d\n", thread.Path, thread.Line, thread.IsResolved, thread.IsOutdated, len(thread.Comments))
	}
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeNextTOON(w io.Writer, next queue.NextAction) int {
	fmt.Fprintln(w, "kind: nextAction")
	fmt.Fprintf(w, "schemaVersion: %d\n", next.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", next.Repo)
	fmt.Fprintf(w, "action: %s\n", next.Action)
	fmt.Fprintf(w, "reason: %s\n", next.Reason)
	if next.PR != nil {
		fmt.Fprintf(w, "pr.number: %d\n", next.PR.Number)
		fmt.Fprintf(w, "pr.headRef: %s\n", next.PR.HeadRef)
		fmt.Fprintf(w, "pr.baseRef: %s\n", next.PR.BaseRef)
	}
	if next.Issue != nil {
		fmt.Fprintf(w, "issue.number: %d\n", next.Issue.Number)
	}
	fmt.Fprintf(w, "blockedItems[%d]:\n", len(next.BlockedItems))
	for _, item := range next.BlockedItems {
		fmt.Fprintf(w, "  - %s\n", oneLine(item))
	}
	fmt.Fprintf(w, "instructions[%d]:\n", len(next.Instructions))
	for _, instruction := range next.Instructions {
		fmt.Fprintf(w, "  - %s\n", oneLine(instruction))
	}
	return exitOK
}

func writeHelpLines(w io.Writer, help []string) {
	fmt.Fprintf(w, "help[%d]:\n", len(help))
	for _, item := range help {
		fmt.Fprintf(w, "  - %s\n", oneLine(item))
	}
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
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

func truncateReviewThreadBodies(result gh.ReviewThreadResult, limit int, full bool) gh.ReviewThreadResult {
	threads := make([]gh.ReviewThread, len(result.Threads))
	copy(threads, result.Threads)
	result.Threads = threads
	for threadIndex := range result.Threads {
		comments := make([]gh.ReviewComment, len(result.Threads[threadIndex].Comments))
		for commentIndex, comment := range result.Threads[threadIndex].Comments {
			comments[commentIndex] = truncateReviewComment(comment, result.PRNumber, limit, full)
		}
		result.Threads[threadIndex].Comments = comments
	}
	return result
}

func truncateReviewComment(comment gh.ReviewComment, prNumber, limit int, full bool) gh.ReviewComment {
	bodyRunes := []rune(comment.Body)
	comment.BodyChars = len(bodyRunes)
	if full || len(bodyRunes) <= limit {
		comment.BodyTruncated = false
		return comment
	}
	preview := string(bodyRunes[:limit])
	comment.Body = preview
	comment.BodyPreview = preview
	comment.BodyTruncated = true
	comment.FullCommand = fmt.Sprintf("baton review-threads %d --full --json", prNumber)
	return comment
}

func withConfigMigrationContent(result configMigrationResult, content string, limit int, full bool) configMigrationResult {
	preview, chars, truncated := limitString(content, limit, full)
	result.Content = preview
	result.ContentChars = chars
	result.ContentTruncated = truncated
	if truncated {
		result.ContentPreview = preview
		result.FullCommand = "baton migrate-config --dry-run --full --json"
	}
	return result
}

func completionResultFromRecord(record complete.Record, limit int, full bool) completionResult {
	summary, summaryChars, summaryTruncated := limitString(record.Summary, limit, full)
	validation, validationChars, validationTruncated := limitString(record.Validation, limit, full)
	result := completionResult{
		SchemaVersion:       record.SchemaVersion,
		Kind:                record.Kind,
		ID:                  record.ID,
		LeaseID:             record.LeaseID,
		Summary:             summary,
		SummaryChars:        summaryChars,
		SummaryTruncated:    summaryTruncated,
		Validation:          validation,
		ValidationChars:     validationChars,
		ValidationTruncated: validationTruncated,
		CreatedAt:           record.CreatedAt,
	}
	if summaryTruncated {
		result.SummaryPreview = summary
		result.FullCommand = "baton complete --summary <text> --full --json"
	}
	if validationTruncated {
		result.ValidationPreview = validation
		result.FullCommand = "baton complete --summary <text> --validation <text> --full --json"
	}
	return result
}

func limitString(value string, limit int, full bool) (string, int, bool) {
	runes := []rune(value)
	if full || len(runes) <= limit {
		return value, len(runes), false
	}
	return string(runes[:limit]), len(runes), true
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
