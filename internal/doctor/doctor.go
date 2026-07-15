package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/repository"
)

type Result struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	ReadyState    string   `json:"readyState"`
	Counts        Counts   `json:"counts"`
	Checks        []Check  `json:"checks"`
	Help          []string `json:"help,omitempty"`
}

type Counts struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type Check struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

func Run(configPath string) Result {
	return RunWithOptionsContext(context.Background(), Options{
		ConfigPath: configPath, GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	})
}

type Options struct {
	WorkingDir, ConfigPath, Repository, EnvironmentRepo, GitHubAPIURL string
	GitHubToken, GHToken, GoInstall, InstallCommand                   string
}

func RunWithOptions(options Options) Result {
	return RunWithOptionsContext(context.Background(), options)
}

func RunWithOptionsContext(ctx context.Context, options Options) Result {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	root := workingDir
	if resolved, err := git.RepositoryRootContext(ctx, workingDir); err == nil {
		root = resolved
	}
	checks := []Check{}
	var cfg config.Config
	configOK := false
	if options.ConfigPath != "" {
		configPath := options.ConfigPath
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workingDir, configPath)
		}
		loaded, err := config.Load(configPath)
		if err != nil {
			checks = append(checks, failCheck("config", err.Error(), "Run `baton init --dry-run --json` and reconcile a valid Baton config."))
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, okCheck("config", configPath))
		}
	} else {
		loaded, err := config.LoadForRepo(root)
		if err != nil {
			checks = append(checks, failCheck("config", err.Error(), "Run `baton init --dry-run --json` and reconcile a valid Baton config."))
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, okCheck("config", "repository config loaded"))
		}
	}
	if err := git.Available(); err != nil {
		checks = append(checks, failCheck("git", err.Error(), "Install git and rerun doctor."))
	} else {
		checks = append(checks, okCheck("git", "git is available"))
	}
	if out, err := git.OutputAtContext(ctx, workingDir, "rev-parse", "--show-toplevel"); err != nil {
		checks = append(checks, failCheck("repo-root", err.Error(), "Run doctor from an isolated checkout of the target repository."))
	} else {
		checks = append(checks, okCheck("repo-root", strings.TrimSpace(out)))
	}
	remoteName := ""
	if configOK {
		remoteName = cfg.Repository.DefaultRemote
	}
	if remoteName == "" {
		checks = append(checks, warnCheck("remote", "repository policy remote not available", "Configure repository.default_remote or pass `--repo owner/name`."))
	} else if out, err := git.OutputAtContext(ctx, root, "remote", "get-url", remoteName); err != nil {
		checks = append(checks, warnCheck("remote", remoteName+" remote not resolved", "Repair the configured remote or pass `--repo owner/name`."))
	} else {
		checks = append(checks, okCheck("remote", git.RedactRemoteURL(strings.TrimSpace(out))))
	}
	if configOK {
		checks = append(checks, stagingBranchCheck(ctx, root, cfg))
	}
	credentials, credentialsErr := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: options.GitHubToken, GHToken: options.GHToken})
	if credentialsErr != nil {
		checks = append(checks, failCheck("github-auth", "GITHUB_TOKEN, GH_TOKEN, or gh auth token is not available", "Authenticate with a token that can read repository metadata, rules, Actions policy, issues, and workflows."))
	} else if configOK {
		repositoryContext, resolveErr := repository.ResolveContext(ctx, repository.Options{
			WorkingDir: workingDir, ConfigPath: options.ConfigPath, Repository: options.Repository,
			EnvironmentRepo: firstNonBlank(options.EnvironmentRepo, os.Getenv("GITHUB_REPOSITORY")), GitHubAPIURL: firstNonBlank(options.GitHubAPIURL, os.Getenv("GITHUB_API_URL")),
		})
		if resolveErr != nil {
			checks = append(checks, failCheck("github-repository", resolveErr.Error(), "Pass `--repo owner/name` or repair the configured GitHub remote."))
			checks = append(checks, failCheck("github-auth", "authenticated repository access was not verified", "Resolve the repository identity and rerun doctor."))
		} else {
			client := gh.NewClientWithCredentials(firstNonBlank(options.GitHubAPIURL, os.Getenv("GITHUB_API_URL")), credentials, nil)
			facts := acquireCompatibilityFacts(ctx, client, repositoryContext.Repository, root, cfg, install.Options{GoInstall: options.GoInstall, InstallCommand: options.InstallCommand})
			checks = append(checks, EvaluateCompatibility(cfg, facts)...)
		}
	}
	counts := countChecks(checks)
	return Result{SchemaVersion: 2, Kind: "doctor", ReadyState: readyState(counts), Counts: counts, Checks: checks, Help: helpForChecks(checks)}
}

func SynchronizationCompatibilityCheck(settings gh.RepositorySettings, rules gh.BranchRules) Check {
	switch {
	case rules.RequiredLinearHistory:
		return failCheck("staging-synchronization", "staging requires linear history, which prevents ancestry-preserving base-to-staging merge commits", "Remove linear-history enforcement from staging before enabling Baton.")
	case !settings.AllowMergeCommit:
		return failCheck("staging-synchronization", "repository merge commits are disabled; squash/rebase-only synchronization is unsupported", "Enable merge commits for reviewed base-to-staging synchronization.")
	case rules.AllowedMergeMethodsSet && !containsFold(rules.AllowedMergeMethods, "merge"):
		return failCheck("staging-synchronization", "active staging rules do not allow merge commits", "Allow the merge method in every active staging pull-request ruleset.")
	default:
		return okCheck("staging-synchronization", "ancestry-preserving merge commits are allowed")
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stagingBranchCheck(ctx context.Context, root string, cfg config.Config) Check {
	input, err := git.InspectAgentBranchRefsAtContext(ctx, root, git.AgentBranchPlanInput{
		Remote:       cfg.Repository.DefaultRemote,
		BaseBranch:   cfg.Repository.BaseBranch,
		TargetBranch: cfg.Repository.StagingBranch,
	})
	if err != nil {
		return warnCheck("staging-branch", err.Error(), "Fetch or repair the configured base and staging branch refs; live GitHub branch facts remain authoritative for adoption readiness.")
	}
	plan := git.ComputeAgentBranchPlan(input)
	if len(plan.Errors) > 0 {
		return warnCheck("staging-branch", strings.Join(plan.Errors, " "), "Repair the local tracking refs; live GitHub branch facts remain authoritative for adoption readiness.")
	}
	if len(plan.ApplyCommands) > 0 {
		return warnCheck("staging-branch", "setup needed; run `baton ensure-branch --json`", "Review `baton ensure-branch --json`, then apply the approved branch plan.")
	}
	if len(plan.Warnings) > 0 {
		return warnCheck("staging-branch", strings.Join(plan.Warnings, " "), "Refresh the configured remote refs and rerun doctor.")
	}
	return okCheck("staging-branch", cfg.Repository.StagingBranch)
}

func (r Result) Failed() bool {
	for _, check := range r.Checks {
		if check.Status == "fail" {
			return true
		}
	}
	return false
}

func countChecks(checks []Check) Counts {
	counts := Counts{}
	for _, check := range checks {
		switch check.Status {
		case "ok":
			counts.OK++
		case "warn":
			counts.Warn++
		case "fail":
			counts.Fail++
		}
	}
	return counts
}

func readyState(counts Counts) string {
	switch {
	case counts.Fail > 0:
		return "blocked"
	case counts.Warn > 0:
		return "degraded"
	default:
		return "ready"
	}
}

func helpForChecks(checks []Check) []string {
	help := []string{}
	seen := map[string]struct{}{}
	for _, check := range checks {
		if check.Status == "ok" || strings.TrimSpace(check.Remediation) == "" {
			continue
		}
		if _, exists := seen[check.Remediation]; !exists {
			seen[check.Remediation] = struct{}{}
			help = append(help, check.Remediation)
		}
	}
	if len(help) == 0 {
		return []string{"Run `baton queue --json` or `baton next --json` after doctor is ready."}
	}
	return help
}

func okCheck(name, message string) Check {
	return Check{Name: name, Status: "ok", Message: message}
}

func warnCheck(name, message, remediation string) Check {
	return Check{Name: name, Status: "warn", Message: message, Remediation: remediation}
}

func failCheck(name, message, remediation string) Check {
	return Check{Name: name, Status: "fail", Message: message, Remediation: remediation}
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}
