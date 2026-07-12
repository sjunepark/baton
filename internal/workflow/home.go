package workflow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	gitadapter "github.com/sjunepark/baton/internal/git"
)

type HomeInput struct {
	WorkingDir      string
	EnvironmentRepo string
	GitHubToken     string
	GHToken         string
	ExecutablePath  string
	HomeDir         string
}

type HomeResult struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Bin           string   `json:"bin"`
	Description   string   `json:"description"`
	Repo          string   `json:"repo"`
	Config        string   `json:"config"`
	Auth          string   `json:"auth"`
	Next          string   `json:"next"`
	Help          []string `json:"help"`
}

type HomeWorkflow struct {
	discoverCredentials func(context.Context, auth.Inputs) (auth.Credentials, error)
}

func NewHomeWorkflow() HomeWorkflow {
	return HomeWorkflow{discoverCredentials: auth.DiscoverContext}
}

func (workflow HomeWorkflow) Run(input HomeInput) HomeResult {
	return workflow.RunContext(context.Background(), input)
}

func (workflow HomeWorkflow) RunContext(ctx context.Context, input HomeInput) HomeResult {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	workingDir := input.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	root := workingDir
	if resolved, err := gitadapter.RepositoryRootContext(ctx, workingDir); err == nil {
		root = resolved
	}
	cfgStatus := "missing (.github/baton.yml)"
	remoteName := ""
	if policy, err := config.LoadForRepo(root); err == nil {
		cfgStatus = "ok"
		remoteName = policy.Repository.DefaultRemote
	} else if !errors.Is(err, config.ErrConfigNotFound) {
		cfgStatus = "invalid (" + err.Error() + ")"
	}
	next := "run `baton next --format toon`"
	if cfgStatus != "ok" {
		next = "unavailable (" + cfgStatus + ")"
	}
	executablePath := input.ExecutablePath
	if executablePath == "" {
		executablePath = currentExecutablePath()
	}
	homeDir := input.HomeDir
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	return HomeResult{
		SchemaVersion: 1,
		Kind:          "home",
		Bin:           homeRelative(executablePath, homeDir),
		Description:   "Coordinate GitHub issue/PR agent workflows for this repository",
		Repo:          homeRepository(ctx, root, input.EnvironmentRepo, remoteName),
		Config:        cfgStatus,
		Auth:          workflow.authStatus(ctx, input),
		Next:          next,
		Help: []string{
			"Run `baton init --dry-run --json`.",
			"Run `baton doctor --format toon`.",
			"Run `baton --help`.",
		},
	}
}

func (workflow HomeWorkflow) authStatus(ctx context.Context, input HomeInput) string {
	credentials, err := workflow.discoverCredentials(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
	if err != nil {
		return "missing"
	}
	if credentials.Source() == auth.SourceGHCLI {
		return "ok (gh auth token)"
	}
	return "ok (token env)"
}

func homeRepository(ctx context.Context, root, environmentRepo, remoteName string) string {
	if repo := strings.TrimSpace(environmentRepo); repo != "" {
		return repo
	}
	if remoteName != "" {
		if remote, err := gitadapter.OutputAtContext(ctx, root, "remote", "get-url", remoteName); err == nil {
			if parsed, err := gitadapter.ParseHostedRepositoryRemote(remote); err == nil {
				return parsed.Repository
			}
			if repo, err := gitadapter.NormalizeRepositoryName(strings.TrimSpace(remote)); err == nil {
				return repo
			}
		}
	}
	if root == "" || root == "." {
		return "unknown"
	}
	return filepath.Base(root)
}

func currentExecutablePath() string {
	if executable, err := os.Executable(); err == nil && executable != "" {
		return executable
	}
	if len(os.Args) == 0 || os.Args[0] == "" {
		return ""
	}
	if resolved, err := exec.LookPath(os.Args[0]); err == nil && resolved != "" {
		return resolved
	}
	return os.Args[0]
}

func homeRelative(path, home string) string {
	if path == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if home == "" {
		return abs
	}
	relative, err := filepath.Rel(home, abs)
	if err == nil && relative != "." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != ".." {
		return filepath.Join("~", relative)
	}
	if abs == home {
		return "~"
	}
	return abs
}
