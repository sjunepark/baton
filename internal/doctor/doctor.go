package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/git"
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
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func Run(configPath string) Result {
	return RunWithOptionsContext(context.Background(), Options{
		ConfigPath: configPath, GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN"),
	})
}

type Options struct {
	WorkingDir  string
	ConfigPath  string
	GitHubToken string
	GHToken     string
}

func RunWithOptions(options Options) Result {
	return RunWithOptionsContext(context.Background(), options)
}

func RunWithOptionsContext(ctx context.Context, options Options) Result {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
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
			checks = append(checks, Check{Name: "config", Status: "fail", Message: err.Error()})
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, Check{Name: "config", Status: "ok", Message: configPath})
		}
	} else {
		loaded, err := config.LoadForRepo(root)
		if err != nil {
			checks = append(checks, Check{Name: "config", Status: "warn", Message: err.Error()})
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, Check{Name: "config", Status: "ok"})
		}
	}
	if err := git.Available(); err != nil {
		checks = append(checks, Check{Name: "git", Status: "fail", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "git", Status: "ok"})
	}
	if out, err := git.OutputAtContext(ctx, workingDir, "rev-parse", "--show-toplevel"); err != nil {
		checks = append(checks, Check{Name: "repo-root", Status: "fail", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "repo-root", Status: "ok", Message: strings.TrimSpace(out)})
	}
	remoteName := ""
	if configOK {
		remoteName = cfg.Repository.DefaultRemote
	}
	if remoteName == "" {
		checks = append(checks, Check{Name: "remote", Status: "warn", Message: "repository policy remote not available"})
	} else if out, err := git.OutputAtContext(ctx, root, "remote", "get-url", remoteName); err != nil {
		checks = append(checks, Check{Name: "remote", Status: "warn", Message: remoteName + " remote not resolved"})
	} else {
		checks = append(checks, Check{Name: "remote", Status: "ok", Message: git.RedactRemoteURL(strings.TrimSpace(out))})
	}
	if configOK {
		checks = append(checks, stagingBranchCheck(ctx, root, cfg))
	}
	if credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: options.GitHubToken, GHToken: options.GHToken}); err == nil {
		message := ""
		if credentials.Source() == auth.SourceGHCLI {
			message = "gh auth token"
		}
		checks = append(checks, Check{Name: "github-auth", Status: "ok", Message: message})
	} else {
		checks = append(checks, Check{Name: "github-auth", Status: "warn", Message: "GITHUB_TOKEN, GH_TOKEN, or gh auth token is not available"})
	}
	counts := countChecks(checks)
	return Result{SchemaVersion: 1, Kind: "doctor", ReadyState: readyState(counts), Counts: counts, Checks: checks, Help: helpForChecks(checks)}
}

func stagingBranchCheck(ctx context.Context, root string, cfg config.Config) Check {
	input, err := git.InspectAgentBranchRefsAtContext(ctx, root, git.AgentBranchPlanInput{
		Remote:       cfg.Repository.DefaultRemote,
		BaseBranch:   cfg.Repository.BaseBranch,
		TargetBranch: cfg.Repository.StagingBranch,
	})
	if err != nil {
		return Check{Name: "staging-branch", Status: "fail", Message: err.Error()}
	}
	plan := git.ComputeAgentBranchPlan(input)
	if len(plan.Errors) > 0 {
		return Check{Name: "staging-branch", Status: "fail", Message: strings.Join(plan.Errors, " ")}
	}
	if len(plan.ApplyCommands) > 0 {
		return Check{Name: "staging-branch", Status: "warn", Message: "setup needed; run `baton ensure-branch --json`"}
	}
	if len(plan.Warnings) > 0 {
		return Check{Name: "staging-branch", Status: "warn", Message: strings.Join(plan.Warnings, " ")}
	}
	return Check{Name: "staging-branch", Status: "ok", Message: cfg.Repository.StagingBranch}
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
	help := []string{"Run `baton queue --json` or `baton next --json` after doctor is ready."}
	for _, check := range checks {
		if check.Name == "config" && check.Status != "ok" {
			return []string{
				"Run `baton init --dry-run --json` to preview Baton config.",
				"Run `baton doctor --config <path> --json` to check a specific config.",
			}
		}
		if check.Name == "staging-branch" && check.Status != "ok" {
			return []string{
				"Run `baton ensure-branch --json` to preview staging branch setup.",
				"Run `baton ensure-branch --apply` after reviewing the plan.",
			}
		}
	}
	return help
}
