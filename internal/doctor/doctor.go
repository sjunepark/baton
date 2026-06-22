package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/lease"
)

type Result struct {
	SchemaVersion int     `json:"schemaVersion"`
	Kind          string  `json:"kind"`
	Checks        []Check `json:"checks"`
}

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func Run(configPath string) Result {
	checks := []Check{}
	if configPath != "" {
		if _, err := config.Load(configPath); err != nil {
			checks = append(checks, Check{Name: "config", Status: "fail", Message: err.Error()})
		} else {
			checks = append(checks, Check{Name: "config", Status: "ok", Message: configPath})
		}
	} else if _, err := config.LoadForRepo("."); err != nil {
		checks = append(checks, Check{Name: "config", Status: "warn", Message: err.Error()})
	} else {
		checks = append(checks, Check{Name: "config", Status: "ok"})
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
	return Result{SchemaVersion: 1, Kind: "doctor", Checks: checks}
}

func (r Result) Failed() bool {
	for _, check := range r.Checks {
		if check.Status == "fail" {
			return true
		}
	}
	return false
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return string(out), err
}
