package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/lease"
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
	checks := []Check{}
	var cfg config.Config
	configOK := false
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			checks = append(checks, Check{Name: "config", Status: "fail", Message: err.Error()})
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, Check{Name: "config", Status: "ok", Message: configPath})
		}
	} else {
		loaded, err := config.LoadForRepo(".")
		if err != nil {
			checks = append(checks, Check{Name: "config", Status: "warn", Message: err.Error()})
		} else {
			cfg = loaded
			configOK = true
			checks = append(checks, Check{Name: "config", Status: "ok"})
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		checks = append(checks, Check{Name: "git", Status: "fail", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "git", Status: "ok"})
	}
	if out, err := gitOutput("rev-parse", "--show-toplevel"); err != nil {
		checks = append(checks, Check{Name: "repo-root", Status: "fail", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "repo-root", Status: "ok", Message: strings.TrimSpace(out)})
	}
	if out, err := gitOutput("remote", "get-url", "origin"); err != nil {
		checks = append(checks, Check{Name: "remote", Status: "warn", Message: "origin remote not resolved"})
	} else {
		checks = append(checks, Check{Name: "remote", Status: "ok", Message: strings.TrimSpace(out)})
	}
	if configOK {
		checks = append(checks, stagingBranchCheck(cfg))
	}
	if os.Getenv("GITHUB_TOKEN") != "" || os.Getenv("GH_TOKEN") != "" {
		checks = append(checks, Check{Name: "github-auth", Status: "ok"})
	} else if _, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		checks = append(checks, Check{Name: "github-auth", Status: "ok", Message: "gh auth token"})
	} else {
		checks = append(checks, Check{Name: "github-auth", Status: "warn", Message: "GITHUB_TOKEN, GH_TOKEN, or gh auth token is not available"})
	}
	root := filepath.Join(lease.DefaultStateRoot(), "worktrees")
	if err := os.MkdirAll(root, 0o755); err != nil {
		checks = append(checks, Check{Name: "worktree-root", Status: "fail", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "worktree-root", Status: "ok", Message: root})
	}
	counts := countChecks(checks)
	return Result{SchemaVersion: 1, Kind: "doctor", ReadyState: readyState(counts), Counts: counts, Checks: checks, Help: helpForChecks(checks)}
}

func stagingBranchCheck(cfg config.Config) Check {
	input, err := git.InspectAgentBranchRefs(git.AgentBranchPlanInput{
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

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}
