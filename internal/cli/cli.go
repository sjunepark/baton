package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/doctor"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/snapshot"
	"github.com/sjunepark/baton/internal/workflow"
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
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Category      string            `json:"category"`
	ExitCode      int               `json:"exitCode"`
	Message       string            `json:"message"`
	Hint          string            `json:"hint,omitempty"`
	Retryable     bool              `json:"retryable"`
	HTTPStatus    int               `json:"httpStatus,omitempty"`
	RequestID     string            `json:"requestId,omitempty"`
	RetryAfter    int64             `json:"retryAfterSeconds,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
	Report        *operation.Report `json:"report,omitempty"`
}

type issuePolicyOutput struct {
	policy.IssuePolicyDecision
	Ownership policy.IssueOwnershipDecision `json:"ownership"`
	Report    *operation.Report             `json:"report,omitempty"`
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
	applicationError := apperror.Wrap(categoryForExit(code), err.Error(), err, hint)
	applicationError.Retryable = errorRetryable(code)
	return r.ApplicationError(applicationError)
}

func (r renderer) ErrorMessage(code int, message, hint string) int {
	return r.ApplicationError(apperror.New(categoryForExit(code), message, hint))
}

func (r renderer) ApplicationError(applicationError *apperror.Error) int {
	if applicationError == nil {
		applicationError = apperror.New(apperror.Usage, "command failed", "")
	}
	code := applicationError.ExitCode()
	if !r.Structured() {
		fmt.Fprintln(r.stderr, applicationError.Error())
		return code
	}
	result := errorResult{
		SchemaVersion: schemaVersionV1,
		Kind:          "error",
		Category:      string(applicationError.Category),
		ExitCode:      code,
		Message:       applicationError.Error(),
		Hint:          firstNonEmpty(applicationError.Hint, defaultErrorHint(code, applicationError.Error())),
		Retryable:     publicErrorRetryable(applicationError),
		HTTPStatus:    applicationError.HTTPStatus,
		RequestID:     applicationError.RequestID,
		RetryAfter:    int64(applicationError.RetryAfter / time.Second),
		Details:       applicationError.Details,
		Report:        applicationError.Report,
	}
	if r.format == formatTOON {
		return r.TOONError(result, code)
	}
	if writeCode := r.JSON(result); writeCode != exitOK {
		return writeCode
	}
	return code
}

func publicErrorRetryable(applicationError *apperror.Error) bool {
	return applicationError.Retryable || applicationError.HTTPStatus == http.StatusTooManyRequests ||
		(applicationError.HTTPStatus >= 500 && applicationError.HTTPStatus <= 599) || applicationError.RetryAfter > 0
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
	if result.Report != nil {
		fmt.Fprintf(r.stdout, "report:\n  kind: %s\n  schemaVersion: %d\n  status: %s\n  operations[%d]:\n", result.Report.Kind, result.Report.SchemaVersion, result.Report.Status, len(result.Report.Operations))
		for _, operationResult := range result.Report.Operations {
			fmt.Fprintf(r.stdout, "    - id: %s\n      resource: %s\n      action: %s\n      status: %s\n", operationResult.ID, operationResult.Resource, operationResult.Action, operationResult.Status)
		}
	}
	return code
}

// Run executes the Baton command line. It is small by design: command packages
// own deterministic decisions, and this layer only parses flags and renders.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	return RunContext(context.Background(), args, stdout, stderr, version)
}

func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		return runHome(ctx, nil, stdout, stderr)
	}
	if args[0] == "--version" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, "--version accepts no arguments")
			return exitUsage
		}
		fmt.Fprintln(stdout, version)
		return exitOK
	}
	if args[0] == "--help" || args[0] == "-h" {
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
		return runHome(ctx, args[1:], stdout, stderr)
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "migrate-config":
		return runMigrateConfig(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "issue-policy":
		return runIssuePolicy(ctx, args[1:], stdout, stderr)
	case "pr-policy":
		return runPRPolicy(ctx, args[1:], stdout, stderr)
	case "pr-transition":
		return runPRTransition(ctx, args[1:], stdout, stderr)
	case "delivery-record":
		return runDeliveryRecord(ctx, args[1:], stdout, stderr)
	case "delivery-bootstrap":
		return runDeliveryBootstrap(ctx, args[1:], stdout, stderr)
	case "sync-labels":
		return runSyncLabels(ctx, args[1:], stdout, stderr)
	case "snapshot":
		return runSnapshot(ctx, args[1:], stdout, stderr)
	case "queue":
		return runQueue(ctx, args[1:], stdout, stderr)
	case "prs":
		return runPRs(ctx, args[1:], stdout, stderr)
	case "pr":
		return runPR(ctx, args[1:], stdout, stderr)
	case "checks":
		return runChecks(ctx, args[1:], stdout, stderr)
	case "review-threads":
		return runReviewThreads(ctx, args[1:], stdout, stderr)
	case "next":
		return runNext(ctx, args[1:], stdout, stderr)
	case "ensure-branch":
		return runEnsureBranch(ctx, args[1:], stdout, stderr)
	case "labels":
		return runLabels(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return exitUsage
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "baton coordinates reusable GitHub issue/PR agent workflows.")
	fmt.Fprint(w, "\nUsage:\n  baton --help\n  baton --version\n")
	for _, name := range commandOrder {
		fmt.Fprintf(w, "  %s\n", commandHelps[name].Usage)
	}
	fmt.Fprintln(w, "\nBaton owns deterministic GitHub policy, observation, recommendation, and")
	fmt.Fprintln(w, "work-item transitions. Callers own checkout isolation and execution state.")
}

type commandHelp struct {
	Purpose  string
	Usage    string
	Flags    []string
	Examples []string
	Related  []string
}

var commandOrder = []string{
	"version", "home", "init", "migrate-config", "doctor", "issue-policy",
	"pr-policy", "pr-transition", "delivery-record", "delivery-bootstrap", "sync-labels", "snapshot", "queue", "prs",
	"pr", "checks", "review-threads", "next", "ensure-branch", "labels",
}

var commandHelps = map[string]commandHelp{
	"version": {
		Purpose:  "Print the Baton version.",
		Usage:    "baton version",
		Examples: []string{"baton version", "baton --version"},
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
		Flags:    []string{"--dry-run: preview installed files", "--apply: write installed files", "--profile: install profile", "--go-install: exact Go module install target", "--install-command: trusted custom install command for non-mutating workflows", "--yes: overwrite changed files when applying", "--json: emit structured JSON"},
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
		Purpose:  "Prove local and live GitHub adoption compatibility.",
		Usage:    "baton doctor [--repo owner/name] [--config <path>] [--go-install module@version|--install-command <cmd>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--go-install: reviewed Go install target used by init", "--install-command: reviewed policy-workflow install command used by init", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton doctor --repo owner/name --format toon", "baton doctor --config .github/baton.yml --json"},
		Related:  []string{"baton init --dry-run --json", "baton queue --json"},
	},
	"issue-policy": {
		Purpose:  "Evaluate issue-form labels and optionally apply the policy result.",
		Usage:    "baton issue-policy --body-file <path>|--event <path> [--labels a,b] [--apply] [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--body-file: issue body markdown file", "--event: GitHub issue event payload", "--labels: comma-separated current labels", "--apply: apply labels and policy comment", "--repo: GitHub repository owner/name", "--config: policy config path", "--json: emit structured JSON"},
		Examples: []string{"baton issue-policy --body-file issue.md --json", "baton issue-policy --event event.json --apply --repo owner/name --json"},
		Related:  []string{"baton queue --json"},
	},
	"pr-policy": {
		Purpose:  "Evaluate pull request policy from a fixture or GitHub event.",
		Usage:    "baton pr-policy --fixture <path>|--event <path> [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--fixture: pure PR policy fixture JSON", "--event: GitHub pull_request event payload", "--repo: GitHub repository owner/name", "--config: policy config path", "--json: emit structured JSON"},
		Examples: []string{"baton pr-policy --fixture pr.json --config .github/baton.yml --json"},
		Related:  []string{"baton pr <number> --json", "baton checks <number> --json"},
	},
	"pr-transition": {
		Purpose:  "Plan or apply GitHub-authoritative work-item transitions from a pull request event.",
		Usage:    "baton pr-transition --event <path> --dry-run|--apply [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--event: GitHub pull_request event payload", "--dry-run: preview transition operations", "--apply: apply idempotent work labels or promotion completion", "--repo: GitHub repository owner/name", "--config: policy config path", "--json: emit structured JSON"},
		Examples: []string{"baton pr-transition --event event.json --dry-run --json", "baton pr-transition --event event.json --apply --repo owner/name --json"},
		Related:  []string{"baton pr-policy --event <path> --json", "baton queue --json"},
	},
	"delivery-record": {
		Purpose:  "Plan or apply a delivery-ledger record for merged staging work.",
		Usage:    "baton delivery-record [--event <path>] --dry-run|--apply [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--event: optional merged pull_request event; omit to reconcile staging", "--dry-run: preview ledger operations", "--apply: append records and commit the checkpoint", "--repo: GitHub repository owner/name", "--config: repository policy path with a pinned delivery locator", "--json: emit structured JSON"},
		Examples: []string{"baton delivery-record --event event.json --dry-run --json", "baton delivery-record --apply --repo owner/name --json"},
		Related:  []string{"baton delivery-bootstrap --dry-run --json", "baton pr-policy --event <path> --json"},
	},
	"delivery-bootstrap": {
		Purpose:  "Plan or apply reviewed migration facts into a pinned delivery ledger.",
		Usage:    "baton delivery-bootstrap --dry-run|--apply [--plan-id <sha256>] [--initialize --ledger-issue <number> --ledger-id <id> --genesis-staging-sha <sha> --observed-at <rfc3339>|--genesis-promotion <number>] [--repo owner/name] [--config <path>] [--json]",
		Flags:    []string{"--dry-run: emit the complete reviewed bootstrap plan", "--apply: apply an unchanged reviewed plan", "--plan-id: exact reviewed plan identity required by apply", "--initialize: create the first checkpoint in an existing locked ledger issue", "--ledger-issue: existing locked ledger issue number", "--ledger-id: stable delivery ledger identity", "--observed-at: fixed RFC3339 genesis observation time", "--genesis-promotion: last acknowledged promotion pull request", "--genesis-staging-sha: explicit acknowledged staging boundary", "--repo: GitHub repository owner/name", "--config: repository policy path; migration requires a pinned delivery locator", "--json: emit structured JSON"},
		Examples: []string{"gh workflow run delivery-recorder.yml -f mode=bootstrap-migrate -f genesis_promotion=42", "gh workflow run delivery-recorder.yml -f mode=bootstrap-initialize -f ledger_issue=900 -f ledger_id=delivery-v1 -f genesis_staging_sha=<sha> -f observed_at=<rfc3339>"},
		Related:  []string{"baton delivery-record --dry-run --json", "baton doctor --json"},
	},
	"sync-labels": {
		Purpose:  "Compare or apply GitHub repository labels from a labels manifest.",
		Usage:    "baton sync-labels --dry-run|--apply [--repo owner/name] [--config <path>] [--labels-file <path>] [--json]",
		Flags:    []string{"--dry-run: preview label changes", "--apply: apply label changes", "--repo: GitHub repository owner/name", "--config: repository policy path", "--labels-file: override policy manifest path", "--json: emit structured JSON"},
		Examples: []string{"baton sync-labels --dry-run --json", "baton sync-labels --apply --repo owner/name --json"},
		Related:  []string{"baton labels --file <path> --json"},
	},
	"queue": {
		Purpose:  "List open issues with Baton eligibility and linked PR state.",
		Usage:    "baton queue [--repo owner/name] [--config <path>] [--fields a,b] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--fields: compact fields, for example number,title,action,reasons", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton queue --format toon", "baton queue --fields number,title,action,reasons --format toon", "baton queue --repo owner/name --config .github/baton.yml --json"},
		Related:  []string{"baton next --json", "baton prs --json"},
	},
	"snapshot": {
		Purpose:  "Observe repository facts and return one typed Baton recommendation.",
		Usage:    "baton snapshot [--repo owner/name] [--config <path>] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton snapshot --format toon", "baton snapshot --repo owner/name --json"},
		Related:  []string{"baton next --json", "baton queue --json"},
	},
	"prs": {
		Purpose:  "List open pull requests relevant to Baton queue work.",
		Usage:    "baton prs [--repo owner/name] [--config <path>] [--fields a,b] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--fields: compact fields, for example number,title,headRef,checkState", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton prs --format toon", "baton prs --fields number,title,headRef,checkState --format toon"},
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
		Usage:    "baton checks <number> [--repo owner/name] [--config <path>] [--fields a,b] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--fields: compact fields, for example name,state,url", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton checks 12 --format toon", "baton checks 12 --fields name,state,url --format toon"},
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
		Purpose:  "Return the next Baton candidate set from queue and PR state.",
		Usage:    "baton next [--repo owner/name] [--config <path>] [--action issue-investigation] [--format text|json|toon] [--json]",
		Flags:    []string{"--repo: GitHub repository owner/name", "--config: policy config path", "--action: inspect eligible investigation candidates instead of default automation priority", "--format: output format text, json, or toon", "--json: emit structured JSON"},
		Examples: []string{"baton next --format toon", "baton next --action issue-investigation --format toon"},
		Related:  []string{"baton queue --json", "baton prs --json"},
	},
	"ensure-branch": {
		Purpose:  "Plan or apply Baton staging branch setup.",
		Usage:    "baton ensure-branch [--apply] [--config <path>] [--remote <name>] [--base <branch>] [--target <branch>] [--remote-base <sha>] [--remote-target <sha>] [--local-target <sha>] [--local-upstream <ref>] [--json]",
		Flags:    []string{"--apply: run planned git commands", "--config: repository policy path", "--remote: remote name", "--base: base branch", "--target: staging branch", "--remote-base: observed remote base SHA", "--remote-target: observed remote target SHA", "--local-target: observed local target SHA", "--local-upstream: observed local upstream ref", "--json: emit structured JSON"},
		Examples: []string{"baton ensure-branch --json", "baton ensure-branch --apply"},
		Related:  []string{"baton doctor --json"},
	},
	"labels": {
		Purpose:  "Read and validate a Baton labels manifest.",
		Usage:    "baton labels [--config <path>] [--file <path>] [--json]",
		Flags:    []string{"--config: repository policy path", "--file: override policy manifest path", "--json: emit structured JSON"},
		Examples: []string{"baton labels --json", "baton labels --file internal/install/templates/.github/labels.yml --json"},
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

func runHome(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("home", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	result := workflow.NewHomeWorkflow().RunContext(ctx, workflow.HomeInput{
		EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	})
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
	plan, err := (workflow.RepositoryFilesWorkflow{}).Init(workflow.InitInput{
		Apply: *apply, Overwrite: *yes, GoInstall: *goInstall, InstallCommand: *installCommand,
	})
	if err != nil {
		return renderWorkflowError(out, err)
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
	result, err := (workflow.RepositoryFilesWorkflow{}).MigrateConfig(workflow.ConfigMigrationInput{
		From: *from, To: *to, Apply: *apply, Overwrite: *yes, Full: *full, BodyLimit: *bodyLimit,
	})
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if *dryRun {
		if *jsonOut {
			return out.JSON(result)
		}
		fmt.Fprintf(stdout, "%s %s from %s\n\n%s", result.Action, *to, *from, result.Content)
		return exitOK
	}
	if *jsonOut {
		return out.JSON(result)
	}
	fmt.Fprintf(stdout, "%s %s\n", result.Action, *to)
	return exitOK
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "policy config path")
	repo := fs.String("repo", "", "GitHub repository owner/name")
	goInstall := fs.String("go-install", "", "reviewed Go install target used by init")
	installCommand := fs.String("install-command", "", "reviewed policy-workflow install command used by init")
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *goInstall != "" && *installCommand != "" {
		fmt.Fprintln(stderr, "doctor accepts only one of --go-install or --install-command")
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	result := doctor.RunWithOptionsContext(ctx, doctor.Options{
		ConfigPath: *configPath, Repository: *repo, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), GitHubAPIURL: os.Getenv("GITHUB_API_URL"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"), GoInstall: *goInstall, InstallCommand: *installCommand,
	})
	if format == formatJSON {
		if code := out.JSON(result); code != exitOK {
			return code
		}
	} else if format == formatTOON {
		if code := writeDoctorTOON(stdout, result); code != exitOK {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "doctor: %s (ok=%d warn=%d fail=%d)\n", result.ReadyState, result.Counts.OK, result.Counts.Warn, result.Counts.Fail)
		for _, check := range result.Checks {
			if check.Message == "" {
				fmt.Fprintf(stdout, "%s: %s\n", check.Name, check.Status)
			} else {
				fmt.Fprintf(stdout, "%s: %s (%s)\n", check.Name, check.Status, check.Message)
			}
			if check.Remediation != "" {
				fmt.Fprintf(stdout, "  remediation: %s\n", check.Remediation)
			}
		}
	}
	if result.Failed() {
		return exitLocalGit
	}
	return exitOK
}

func runIssuePolicy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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

	result, workflowErr := workflow.NewIssuePolicyWorkflow().RunContext(ctx, workflow.IssuePolicyInput{
		BodyPath: *bodyFile, EventPath: *eventPath, CurrentLabels: splitCSV(*labelsCSV), ConfigPath: *configPath,
		Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), Apply: *apply,
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	})
	if workflowErr != nil && !result.Evaluated {
		return renderWorkflowError(out, workflowErr)
	}
	decision := result.Decision
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
	if workflowErr != nil {
		return renderWorkflowError(out, workflowErr)
	}
	if *jsonOut {
		return out.JSON(issuePolicyOutput{IssuePolicyDecision: decision, Ownership: result.Ownership, Report: result.Report})
	}
	return exitOK
}

func runPRPolicy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	decision, err := workflow.NewPullRequestPolicyWorkflow().RunContext(ctx, workflow.PullRequestPolicyInput{
		FixturePath: *fixturePath, EventPath: *eventPath, ConfigPath: *configPath,
		Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), GitHubAPIURL: os.Getenv("GITHUB_API_URL"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
		WorkflowName: os.Getenv("GITHUB_WORKFLOW"), RunID: githubRunID(),
	})
	if err != nil {
		return renderWorkflowError(out, err)
	}
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
		writePromotionPolicyFacts(stdout, decision)
		fmt.Fprintln(stdout, "PR policy check passed.")
		return exitOK
	}
	writePromotionPolicyFacts(stderr, decision)
	fmt.Fprintln(stderr, "PR policy check failed:")
	for _, msg := range decision.Errors {
		fmt.Fprintf(stderr, "- %s\n", msg)
	}
	return exitPolicy
}

func writePromotionPolicyFacts(writer io.Writer, decision policy.PRPolicyDecision) {
	if decision.PromotionFacts == nil {
		return
	}
	fmt.Fprintf(writer, "Promotion evidence: complete=%t expectedIssues=%s\n", decision.PromotionFacts.Complete, intList(decision.PromotionFacts.ExpectedIssues))
}

func runPRTransition(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr-transition", flag.ContinueOnError)
	fs.SetOutput(stderr)
	eventPath := fs.String("event", "", "GitHub pull_request event payload")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	dryRun := fs.Bool("dry-run", false, "preview transition operations")
	apply := fs.Bool("apply", false, "apply transition operations to GitHub")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if strings.TrimSpace(*eventPath) == "" {
		return out.ErrorMessage(exitUsage, "pr-transition requires --event", "Run `baton pr-transition --event <path> --dry-run`.")
	}
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "pr-transition requires exactly one of --dry-run or --apply", "Preview with --dry-run before applying.")
	}
	result, err := workflow.NewPullRequestTransitionWorkflow().RunContext(ctx, workflow.PullRequestTransitionInput{
		EventPath: *eventPath, ConfigPath: *configPath, Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), Apply: *apply,
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
		WorkflowName: os.Getenv("GITHUB_WORKFLOW"), RunID: githubRunID(),
	})
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if *jsonOut {
		return out.JSON(result)
	}
	mode := "Planned"
	if *apply {
		mode = "Applied"
	}
	fmt.Fprintf(stdout, "%s %d work-item transition operation(s) for PR #%d.\n", mode, len(result.Operations), result.PullRequestNumber)
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "Warning: %s\n", warning)
	}
	return exitOK
}

func runDeliveryRecord(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("delivery-record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	eventPath := fs.String("event", "", "optional merged pull_request event payload")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	dryRun := fs.Bool("dry-run", false, "preview delivery ledger operations")
	apply := fs.Bool("apply", false, "apply delivery ledger operations")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "delivery-record requires exactly one of --dry-run or --apply", "Preview with --dry-run before applying.")
	}
	result, err := workflow.NewDeliveryRecordWorkflow().RunContext(ctx, workflow.DeliveryRecordInput{
		EventPath: *eventPath, ConfigPath: *configPath, Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), Apply: *apply,
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
		WorkflowName: os.Getenv("GITHUB_WORKFLOW"), RunID: githubRunID(),
	})
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if *jsonOut {
		return out.JSON(result)
	}
	mode := "Planned"
	if *apply {
		mode = "Applied"
	}
	fmt.Fprintf(stdout, "%s %d delivery record operation(s).\n", mode, len(result.Operations))
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "Warning: %s\n", warning)
	}
	return exitOK
}

func runDeliveryBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("delivery-bootstrap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	genesisPromotion := fs.Int("genesis-promotion", 0, "last acknowledged promotion pull request")
	genesisStagingSHA := fs.String("genesis-staging-sha", "", "explicit acknowledged staging boundary")
	initialize := fs.Bool("initialize", false, "create the first checkpoint in an existing locked ledger issue")
	ledgerIssue := fs.Int("ledger-issue", 0, "existing locked ledger issue number")
	ledgerID := fs.String("ledger-id", "", "stable delivery ledger identity")
	observedAt := fs.String("observed-at", "", "fixed RFC3339 genesis observation time")
	planID := fs.String("plan-id", "", "exact reviewed bootstrap plan identity")
	dryRun := fs.Bool("dry-run", false, "preview bootstrap operations")
	apply := fs.Bool("apply", false, "apply reviewed bootstrap operations")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "delivery-bootstrap requires exactly one of --dry-run or --apply", "Review a --dry-run plan before applying.")
	}
	if *genesisPromotion > 0 && strings.TrimSpace(*genesisStagingSHA) != "" {
		return out.ErrorMessage(exitUsage, "delivery-bootstrap accepts only one genesis selector", "Choose --genesis-promotion or --genesis-staging-sha.")
	}
	if *initialize && (*ledgerIssue <= 0 || strings.TrimSpace(*ledgerID) == "" || strings.TrimSpace(*genesisStagingSHA) == "" || strings.TrimSpace(*observedAt) == "") {
		return out.ErrorMessage(exitUsage, "delivery-bootstrap --initialize requires --ledger-issue, --ledger-id, --genesis-staging-sha, and --observed-at", "Create and lock the reserved ledger issue before initialization.")
	}
	if *initialize && *genesisPromotion > 0 {
		return out.ErrorMessage(exitUsage, "delivery-bootstrap initialization cannot use --genesis-promotion", "Pass the exact reviewed staging SHA.")
	}
	if *apply && strings.TrimSpace(*planID) == "" {
		return out.ErrorMessage(exitUsage, "delivery-bootstrap --apply requires --plan-id", "Pass the exact planId from the reviewed dry-run output.")
	}
	result, err := workflow.NewDeliveryBootstrapWorkflow().RunContext(ctx, workflow.DeliveryBootstrapInput{
		ConfigPath: *configPath, Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), Apply: *apply,
		ReviewedPlanID: strings.TrimSpace(*planID), GenesisPromotion: *genesisPromotion, GenesisStagingSHA: strings.TrimSpace(*genesisStagingSHA),
		Initialize: *initialize, LedgerIssue: *ledgerIssue, LedgerID: strings.TrimSpace(*ledgerID), ObservedAt: strings.TrimSpace(*observedAt),
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
		WorkflowName: os.Getenv("GITHUB_WORKFLOW"), RunID: githubRunID(),
	})
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if *jsonOut {
		return out.JSON(result)
	}
	mode := "Planned"
	if *apply {
		mode = "Applied"
	}
	fmt.Fprintf(stdout, "%s delivery bootstrap %s with %d operation(s).\n", mode, result.PlanID, len(result.Operations))
	for _, ambiguity := range result.Ambiguities {
		fmt.Fprintf(stdout, "Ambiguity: %s\n", ambiguity.Message)
	}
	return exitOK
}

func githubRunID() int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(os.Getenv("GITHUB_RUN_ID")), 10, 64)
	return value
}

func runSyncLabels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync-labels", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "preview label changes")
	apply := fs.Bool("apply", false, "apply label changes")
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	labelsFile := fs.String("labels-file", "", "override repository-policy labels manifest path")
	configPath := fs.String("config", "", "repository policy path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	if *dryRun == *apply {
		return out.ErrorMessage(exitUsage, "sync-labels requires exactly one of --dry-run or --apply", "Run `baton sync-labels --dry-run` to preview or `baton sync-labels --apply` to update GitHub labels.")
	}
	plan, err := workflow.NewLabelSyncWorkflow().RunContext(ctx, workflow.LabelSyncInput{
		Repository: *repoFlag, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), ManifestPath: *labelsFile, ConfigPath: *configPath, Apply: *apply,
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	})
	if err != nil {
		return renderWorkflowError(out, err)
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

func runQueue(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	fieldsFlag := fs.String("fields", "", "compact fields")
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	snapshot, err := workflow.NewObservationWorkflow().QueueContext(ctx, observationInput(*repoFlag, *configPath))
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if format == formatJSON {
		return out.JSON(snapshot)
	}
	if format == formatTOON {
		fields, err := parseFields(*fieldsFlag, queueFieldSet())
		if err != nil {
			return out.Error(exitUsage, err, "")
		}
		return writeQueueTOONFields(stdout, snapshot, fields)
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

func runSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
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
	result, err := workflow.NewObservationWorkflow().SnapshotContext(ctx, observationInput(*repoFlag, *configPath), "")
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if format == formatJSON {
		return out.JSON(result)
	}
	if format == formatTOON {
		return writeRepositorySnapshotTOON(stdout, result)
	}
	fmt.Fprintf(stdout, "Repository: %s\nCompleteness: %s\nOutcome: %s\n", result.Repository, result.Completeness, result.Recommendation.Outcome)
	if result.Recommendation.Action != nil {
		fmt.Fprintf(stdout, "Action: %s\n", *result.Recommendation.Action)
	}
	fmt.Fprintf(stdout, "Candidates: %d\nWarnings: %d\n", len(result.Recommendation.Candidates), len(result.Warnings))
	return exitOK
}

func runPRs(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	fieldsFlag := fs.String("fields", "", "compact fields")
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	result, err := workflow.NewObservationWorkflow().PullRequestListingContext(ctx, observationInput(*repoFlag, *configPath))
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if format == formatJSON {
		return out.JSON(result)
	}
	if format == formatTOON {
		fields, err := parseFields(*fieldsFlag, prFieldSet())
		if err != nil {
			return out.Error(exitUsage, err, "")
		}
		return writePRsTOONFields(stdout, result, fields)
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

func runPR(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("pr", args, stdout, stderr)
	out := newRenderer(stdout, stderr, flags.json)
	if code != exitOK {
		return code
	}
	result, err := workflow.NewPullRequestWorkflow().DashboardContext(ctx, pullRequestInput(number, flags))
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if flags.json {
		return out.JSON(result)
	}
	fmt.Fprintf(stdout, "#%d %s -> %s checks=%s reviewThreads=%d unresolved=%d\n", result.PullRequest.Number, result.PullRequest.HeadRef, result.PullRequest.BaseRef, result.Checks.State, result.ReviewThreads.Count, result.ReviewThreads.Summary.Unresolved)
	if len(result.ReferencedIssues) > 0 {
		fmt.Fprintf(stdout, "referencedIssues=%s\n", intList(result.ReferencedIssues))
	}
	if result.LikelyNextCommand != "" {
		fmt.Fprintf(stdout, "next: %s\n", result.LikelyNextCommand)
	}
	return exitOK
}

func runChecks(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("checks", args, stdout, stderr)
	out := newFormatRenderer(stdout, stderr, flags.format)
	if code != exitOK {
		return code
	}
	rollup, err := workflow.NewPullRequestWorkflow().ChecksContext(ctx, pullRequestInput(number, flags))
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if flags.format == formatJSON {
		return out.JSON(rollup)
	}
	if flags.format == formatTOON {
		return writeChecksTOONFields(stdout, rollup, flags.fields)
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

func runReviewThreads(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	number, flags, code := parseNumberCommand("review-threads", args, stdout, stderr)
	out := newFormatRenderer(stdout, stderr, flags.format)
	if code != exitOK {
		return code
	}
	if flags.bodyLimit < 0 {
		return out.ErrorMessage(exitUsage, "review-threads --body-limit must be non-negative", "")
	}
	threads, err := workflow.NewPullRequestWorkflow().ReviewThreadsContext(ctx, pullRequestInput(number, flags))
	if err != nil {
		return renderWorkflowError(out, err)
	}
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

func runNext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository owner/name")
	configPath := fs.String("config", "", "policy config path")
	actionFlag := fs.String("action", "", "issue action to inspect")
	formats := addFormatFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out, format, code := rendererFromFormatFlags(stdout, stderr, formats)
	if code != exitOK {
		return code
	}
	if *actionFlag != "" && *actionFlag != "issue-investigation" {
		return out.ErrorMessage(exitUsage, "next --action currently supports issue-investigation only", "Run `baton next --action issue-investigation --format toon`.")
	}
	next, err := workflow.NewObservationWorkflow().NextContext(ctx, observationInput(*repoFlag, *configPath), *actionFlag)
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if format == formatJSON {
		return out.JSON(next)
	}
	if format == formatTOON {
		return writeNextTOON(stdout, next)
	}
	fmt.Fprintf(stdout, "Next action: %s\nReason: %s\nCandidates: %d\n", next.SelectedAction, next.Reason, len(next.Candidates))
	for _, candidate := range next.Candidates {
		if candidate.Number != 0 {
			fmt.Fprintf(stdout, "- %s #%d %s\n", candidate.Type, candidate.Number, candidate.Title)
			continue
		}
		fmt.Fprintf(stdout, "- %s %s\n", candidate.Type, candidate.Ref)
	}
	return exitOK
}

func observationInput(repo, configPath string) workflow.RepositoryInput {
	return workflow.RepositoryInput{
		Repository: repo, ConfigPath: configPath,
		EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), GitHubAPIURL: os.Getenv("GITHUB_API_URL"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	}
}

func pullRequestInput(number int, flags numberFlags) workflow.PullRequestInput {
	return workflow.PullRequestInput{
		Number: number, Repository: flags.repo, EnvironmentRepo: os.Getenv("GITHUB_REPOSITORY"), ConfigPath: flags.config,
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"), GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
		BodyLimit: flags.bodyLimit, Full: flags.full,
	}
}

func renderWorkflowError(out renderer, err error) int {
	if applicationError := apperror.As(err); applicationError != nil {
		return out.ApplicationError(applicationError)
	}
	return out.ApplicationError(apperror.Wrap(apperror.Usage, "command failed", err, ""))
}

func runEnsureBranch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ensure-branch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remote := fs.String("remote", "", "override repository-policy remote name")
	base := fs.String("base", "", "override repository-policy base branch")
	target := fs.String("target", "", "override repository-policy staging branch")
	configPath := fs.String("config", "", "repository policy path")
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
	plan, err := workflow.NewBranchWorkflow().RunContext(ctx, workflow.BranchInput{ConfigPath: *configPath, Plan: git.AgentBranchPlanInput{
		Remote:              *remote,
		BaseBranch:          *base,
		TargetBranch:        *target,
		RemoteBaseSHA:       *remoteBase,
		RemoteTargetSHA:     *remoteTarget,
		LocalTargetSHA:      *localTarget,
		LocalTargetUpstream: *localUpstream,
	}, Apply: *apply})
	if err != nil {
		return renderWorkflowError(out, err)
	}
	if *jsonOut {
		if code := out.JSON(plan); code != exitOK {
			return code
		}
	}
	if !*jsonOut {
		fmt.Fprintln(stdout, "Staging branch plan:")
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

func runLabels(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("labels", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "", "override repository-policy labels manifest path")
	configPath := fs.String("config", "", "repository policy path")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	out := newRenderer(stdout, stderr, *jsonOut)
	manifest, err := (workflow.RepositoryFilesWorkflow{}).LabelsContext(ctx, workflow.LabelsInput{Path: *path, ConfigPath: *configPath})
	if err != nil {
		return renderWorkflowError(out, err)
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

type numberFlags struct {
	repo      string
	config    string
	json      bool
	format    outputFormat
	fields    []string
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
	fieldsFlag := ""
	if name == "checks" {
		fs.StringVar(&fieldsFlag, "fields", "", "compact fields")
	}
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
	fields := []string(nil)
	if name == "checks" {
		parsed, err := parseFields(fieldsFlag, checkFieldSet())
		if err != nil {
			out := newFormatRenderer(stdout, stderr, format)
			return 0, numberFlags{json: format == formatJSON, format: format}, out.Error(exitUsage, err, "")
		}
		fields = parsed
	}
	return number, numberFlags{repo: *repoFlag, config: *configPath, json: format == formatJSON, format: format, fields: fields, full: full, bodyLimit: bodyLimit}, exitOK
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

func writeHomeText(w io.Writer, result workflow.HomeResult) int {
	fmt.Fprintf(w, "bin: %s\n", result.Bin)
	fmt.Fprintf(w, "description: %s\n", result.Description)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "config: %s\n", result.Config)
	fmt.Fprintf(w, "auth: %s\n", result.Auth)
	fmt.Fprintf(w, "next: %s\n", result.Next)
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeHomeTOON(w io.Writer, result workflow.HomeResult) int {
	fmt.Fprintln(w, "kind: home")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "bin: %s\n", result.Bin)
	fmt.Fprintf(w, "description: %s\n", result.Description)
	fmt.Fprintf(w, "repo: %s\n", result.Repo)
	fmt.Fprintf(w, "config: %s\n", result.Config)
	fmt.Fprintf(w, "auth: %s\n", result.Auth)
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
		if check.Remediation != "" {
			line += " remediation=" + oneLine(check.Remediation)
		}
		fmt.Fprintln(w, line)
	}
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeQueueTOON(w io.Writer, snapshot queue.Snapshot) int {
	return writeQueueTOONFields(w, snapshot, nil)
}

func writeQueueTOONFields(w io.Writer, snapshot queue.Snapshot, fields []string) int {
	if len(fields) == 0 {
		fields = []string{"number", "eligible", "action", "priorityLabel", "title", "reasons"}
	}
	fmt.Fprintln(w, "kind: queueSnapshot")
	fmt.Fprintf(w, "schemaVersion: %d\n", snapshot.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", snapshot.Repo)
	fmt.Fprintf(w, "counts.totalIssues: %d\n", snapshot.Counts.TotalIssues)
	fmt.Fprintf(w, "counts.eligibleIssues: %d\n", snapshot.Counts.EligibleIssues)
	writeActionCounts(w, "counts.eligibleByAction", snapshot.Counts.EligibleByAction)
	fmt.Fprintf(w, "counts.skippedIssues: %d\n", snapshot.Counts.SkippedIssues)
	fmt.Fprintf(w, "counts.openPullRequests: %d\n", snapshot.Counts.OpenPullRequests)
	if snapshot.Counts.BranchHealthState != "" {
		fmt.Fprintf(w, "counts.branchHealthState: %s\n", snapshot.Counts.BranchHealthState)
	}
	fmt.Fprintf(w, "issues[%d]:\n", len(snapshot.Issues))
	for _, issue := range snapshot.Issues {
		fmt.Fprintf(w, "  - %s\n", strings.Join(queueFieldValues(issue, fields), " "))
	}
	writeHelpLines(w, snapshot.Help)
	return exitOK
}

func writePRsTOON(w io.Writer, result workflow.PullRequestsResult) int {
	return writePRsTOONFields(w, result, nil)
}

func writePRsTOONFields(w io.Writer, result workflow.PullRequestsResult, fields []string) int {
	if len(fields) == 0 {
		fields = []string{"number", "headRef", "baseRef", "checkState", "title"}
	}
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
		fmt.Fprintf(w, "  - %s\n", strings.Join(prFieldValues(pr, fields), " "))
	}
	writeHelpLines(w, result.Help)
	return exitOK
}

func writeChecksTOON(w io.Writer, rollup gh.CheckRollup) int {
	return writeChecksTOONFields(w, rollup, nil)
}

func writeChecksTOONFields(w io.Writer, rollup gh.CheckRollup, fields []string) int {
	if len(fields) == 0 {
		fields = []string{"name", "status", "conclusion"}
	}
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
		fmt.Fprintf(w, "  - %s\n", strings.Join(checkFieldValues(check, fields), " "))
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

func writeNextTOON(w io.Writer, next queue.NextCandidates) int {
	fmt.Fprintln(w, "kind: nextCandidates")
	fmt.Fprintf(w, "schemaVersion: %d\n", next.SchemaVersion)
	fmt.Fprintf(w, "repo: %s\n", next.Repo)
	fmt.Fprintf(w, "selectedAction: %s\n", next.SelectedAction)
	fmt.Fprintf(w, "reason: %s\n", next.Reason)
	fmt.Fprintf(w, "selectionReason: %s\n", next.SelectionReason)
	fmt.Fprintf(w, "selectionRequired: %v\n", next.SelectionRequired)
	writeNextCandidateLines(w, "candidates", next.Candidates)
	writeNextCandidateLines(w, "deferredEligibleItems", next.DeferredEligibleItems)
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

func writeRepositorySnapshotTOON(w io.Writer, result snapshot.RepositorySnapshot) int {
	fmt.Fprintln(w, "kind: repositorySnapshot")
	fmt.Fprintf(w, "schemaVersion: %d\n", result.SchemaVersion)
	fmt.Fprintf(w, "repository: %s\n", result.Repository)
	fmt.Fprintf(w, "acquisition.startedAt: %s\n", result.Acquisition.StartedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(w, "acquisition.completedAt: %s\n", result.Acquisition.CompletedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(w, "completeness: %s\n", result.Completeness)
	fmt.Fprintf(w, "warnings[%d]:\n", len(result.Warnings))
	for _, warning := range result.Warnings {
		fmt.Fprintf(w, "  - code=%s scope=%s retryable=%v", warning.Code, warning.Scope, warning.Retryable)
		if warning.HTTPStatus != 0 {
			fmt.Fprintf(w, " httpStatus=%d", warning.HTTPStatus)
		}
		if warning.RequestID != "" {
			fmt.Fprintf(w, " requestId=%s", warning.RequestID)
		}
		fmt.Fprintf(w, " message=%s\n", oneLine(warning.Message))
	}
	fmt.Fprintf(w, "recommendation.outcome: %s\n", result.Recommendation.Outcome)
	if result.Recommendation.Action != nil {
		fmt.Fprintf(w, "recommendation.action: %s\n", *result.Recommendation.Action)
	}
	fmt.Fprintf(w, "recommendation.selectionRequired: %v\n", result.Recommendation.SelectionRequired)
	writeSnapshotCandidateLines(w, "recommendation.candidates", result.Recommendation.Candidates)
	writeSnapshotCandidateLines(w, "recommendation.deferredCandidates", result.Recommendation.DeferredCandidates)
	fmt.Fprintf(w, "recommendation.reasons[%d]:\n", len(result.Recommendation.Reasons))
	for _, reason := range result.Recommendation.Reasons {
		fmt.Fprintf(w, "  - %s\n", oneLine(reason))
	}
	fmt.Fprintf(w, "recommendation.instructions[%d]:\n", len(result.Recommendation.Instructions))
	for _, instruction := range result.Recommendation.Instructions {
		fmt.Fprintf(w, "  - %s\n", oneLine(instruction))
	}
	return exitOK
}

func writeSnapshotCandidateLines(w io.Writer, key string, candidates []snapshot.Candidate) {
	fmt.Fprintf(w, "%s[%d]:\n", key, len(candidates))
	for _, candidate := range candidates {
		identity := candidate.Identity
		switch identity.Kind {
		case snapshot.CandidateIssue:
			fmt.Fprintf(w, "  - kind=%s repository=%s number=%d state=%s", identity.Kind, identity.Repository, identity.Number, candidate.State)
		case snapshot.CandidatePullRequest:
			fmt.Fprintf(w, "  - kind=%s repository=%s number=%d headSha=%s baseSha=%s state=%s", identity.Kind, identity.Repository, identity.Number, identity.HeadSHA, identity.BaseSHA, candidate.State)
		case snapshot.CandidateBranch:
			fmt.Fprintf(w, "  - kind=%s repository=%s ref=%s sha=%s state=%s", identity.Kind, identity.Repository, identity.Ref, identity.SHA, candidate.State)
		}
		if len(candidate.Reasons) > 0 {
			fmt.Fprintf(w, " reasons=%s", oneLine(strings.Join(candidate.Reasons, ",")))
		}
		fmt.Fprintln(w)
	}
}

func writeNextCandidateLines(w io.Writer, key string, candidates []queue.NextCandidate) {
	fmt.Fprintf(w, "%s[%d]:\n", key, len(candidates))
	for _, candidate := range candidates {
		values := []string{"type=" + candidate.Type}
		if candidate.Number != 0 {
			values = append(values, fmt.Sprintf("number=%d", candidate.Number))
		}
		if candidate.Title != "" {
			values = append(values, "title="+oneLine(candidate.Title))
		}
		if candidate.HeadRef != "" {
			values = append(values, "headRef="+candidate.HeadRef)
		}
		if candidate.BaseRef != "" {
			values = append(values, "baseRef="+candidate.BaseRef)
		}
		if candidate.Ref != "" {
			values = append(values, "ref="+candidate.Ref)
		}
		if candidate.CheckState != "" {
			values = append(values, "checkState="+candidate.CheckState)
		}
		if candidate.PriorityLabel != "" {
			values = append(values, "priorityLabel="+candidate.PriorityLabel)
		}
		fmt.Fprintf(w, "  - %s\n", strings.Join(values, " "))
	}
}

func writeActionCounts(w io.Writer, key string, counts map[string]int) {
	for _, action := range orderedActions(counts) {
		fmt.Fprintf(w, "%s.%s: %d\n", key, action, counts[action])
	}
}

func orderedActions(counts map[string]int) []string {
	preferred := []string{"issue-implementation", "issue-investigation"}
	seen := map[string]struct{}{}
	actions := []string{}
	for _, action := range preferred {
		if counts[action] == 0 {
			continue
		}
		seen[action] = struct{}{}
		actions = append(actions, action)
	}
	extra := []string{}
	for action, count := range counts {
		if count == 0 {
			continue
		}
		if _, ok := seen[action]; ok {
			continue
		}
		extra = append(extra, action)
	}
	sort.Strings(extra)
	return append(actions, extra...)
}

func writeHelpLines(w io.Writer, help []string) {
	fmt.Fprintf(w, "help[%d]:\n", len(help))
	for _, item := range help {
		fmt.Fprintf(w, "  - %s\n", oneLine(item))
	}
}

func parseFields(value string, allowed map[string]struct{}) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	fields := []string{}
	for _, part := range strings.Split(value, ",") {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("unknown field %q", field)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func queueFieldSet() map[string]struct{} {
	return fieldSet("number", "title", "eligible", "action", "priorityLabel", "reasons", "linkedPrs")
}

func prFieldSet() map[string]struct{} {
	return fieldSet("number", "title", "headRef", "baseRef", "checkState", "referencedIssues")
}

func checkFieldSet() map[string]struct{} {
	return fieldSet("name", "state", "status", "conclusion", "url")
}

func fieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

func queueFieldValues(issue queue.IssueState, fields []string) []string {
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "number":
			values = append(values, fmt.Sprintf("number=%d", issue.Issue.Number))
		case "title":
			values = append(values, "title="+oneLine(issue.Issue.Title))
		case "eligible":
			values = append(values, fmt.Sprintf("eligible=%v", issue.Eligible))
		case "action":
			values = append(values, "action="+issue.Action)
		case "priorityLabel":
			values = append(values, "priorityLabel="+issue.PriorityLabel)
		case "reasons":
			values = append(values, "reasons="+strings.Join(issue.Reasons, "|"))
		case "linkedPrs":
			values = append(values, "linkedPrs="+intList(issue.LinkedPRs))
		}
	}
	return values
}

func prFieldValues(pr queue.PullState, fields []string) []string {
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "number":
			values = append(values, fmt.Sprintf("number=%d", pr.PullRequest.Number))
		case "title":
			values = append(values, "title="+oneLine(pr.PullRequest.Title))
		case "headRef":
			values = append(values, "headRef="+pr.PullRequest.HeadRef)
		case "baseRef":
			values = append(values, "baseRef="+pr.PullRequest.BaseRef)
		case "checkState":
			values = append(values, "checkState="+pr.PullRequest.CheckState)
		case "referencedIssues":
			values = append(values, "referencedIssues="+intList(pr.ReferencedIssues))
		}
	}
	return values
}

func checkFieldValues(check gh.CheckState, fields []string) []string {
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "name":
			values = append(values, "name="+oneLine(check.Name))
		case "state":
			values = append(values, "state="+firstNonEmpty(check.Conclusion, check.Status))
		case "status":
			values = append(values, "status="+check.Status)
		case "conclusion":
			values = append(values, "conclusion="+check.Conclusion)
		case "url":
			values = append(values, "url="+check.URL)
		}
	}
	return values
}

func intList(values []int) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return strings.Join(parts, "|")
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
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

func categoryForExit(code int) apperror.Category {
	return apperror.Category(exitCategory(code))
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
		return "Inspect the local git state, then retry."
	case exitPolicy:
		return "Inspect the policy decision output before continuing."
	default:
		return ""
	}
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "encode JSON: %v\n", err)
		return exitUsage
	}
	return exitOK
}
