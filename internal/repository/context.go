package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	gitadapter "github.com/sjunepark/baton/internal/git"
)

type ErrorCode string

const (
	ErrorMissingRepository  ErrorCode = "missing_repository"
	ErrorInvalidRepository  ErrorCode = "invalid_repository"
	ErrorRepositoryMismatch ErrorCode = "repository_mismatch"
	ErrorConfiguredRemote   ErrorCode = "configured_remote"
	ErrorRemoteHost         ErrorCode = "remote_host"
)

// ResolveError is a typed repository-context failure. Its public rendering
// remains the structured error v1 config/usage contract owned by the CLI.
type ResolveError struct {
	Code        ErrorCode
	LeftSource  string
	Left        string
	RightSource string
	Right       string
	Remote      string
	Cause       error
}

func (e *ResolveError) Error() string {
	switch e.Code {
	case ErrorMissingRepository:
		return "--repo, GITHUB_REPOSITORY, or a GitHub checkout remote is required"
	case ErrorInvalidRepository:
		return fmt.Sprintf("%s %q must be a GitHub owner/name", e.LeftSource, e.Left)
	case ErrorRepositoryMismatch:
		return fmt.Sprintf("repository mismatch: %s %q does not match %s %q; run Baton from the matching checkout and use one repository identity", e.LeftSource, e.Left, e.RightSource, e.Right)
	case ErrorConfiguredRemote:
		return fmt.Sprintf("configured remote %q cannot be resolved; fix repository.default_remote or the checkout remote", e.Remote)
	case ErrorRemoteHost:
		return fmt.Sprintf("configured remote %q is not hosted by the GitHub API endpoint; use the matching checkout or GITHUB_API_URL", e.Remote)
	default:
		return "resolve repository context failed"
	}
}

func (e *ResolveError) Unwrap() error { return e.Cause }

type LocalGitError struct {
	Operation string
	Cause     error
}

func (e *LocalGitError) Error() string { return fmt.Sprintf("%s: %v", e.Operation, e.Cause) }
func (e *LocalGitError) Unwrap() error { return e.Cause }

type Options struct {
	WorkingDir      string
	Repository      string
	ConfigPath      string
	EnvironmentRepo string
	GitHubAPIURL    string
}

type Context struct {
	Root       string
	Repository string
	ConfigPath string
	Remote     string
	RemoteURL  string
	Config     config.Config
}

type Target struct {
	Root       string
	Repository string
	Remote     string
	RemoteURL  string
}

// Resolve binds local policy and the GitHub target into one validated context.
func Resolve(options Options) (Context, error) {
	return ResolveContext(context.Background(), options)
}

func ResolveContext(ctx context.Context, options Options) (Context, error) {
	root, workingDir, hasGitRepository, err := resolveRoot(ctx, options.WorkingDir)
	if err != nil {
		return Context{}, err
	}
	cfg, configPath, err := loadConfig(root, workingDir, options.ConfigPath)
	if err != nil {
		return Context{}, err
	}
	target, err := resolveTarget(ctx, options, root, hasGitRepository, cfg.Repository.DefaultRemote)
	if err != nil {
		return Context{}, err
	}
	return Context{
		Root: target.Root, Repository: target.Repository, ConfigPath: configPath,
		Remote: target.Remote, RemoteURL: target.RemoteURL, Config: cfg,
	}, nil
}

// ResolveTarget validates local checkout and GitHub identity without requiring
// Baton policy. Commands that only read GitHub facts can use this narrower seam.
func ResolveTarget(options Options, remote string) (Target, error) {
	return ResolveTargetContext(context.Background(), options, remote)
}

func ResolveTargetContext(ctx context.Context, options Options, remote string) (Target, error) {
	root, _, hasGitRepository, err := resolveRoot(ctx, options.WorkingDir)
	if err != nil {
		return Target{}, err
	}
	if remote == "" {
		remote = "origin"
	}
	return resolveTarget(ctx, options, root, hasGitRepository, remote)
}

func resolveRoot(ctx context.Context, workingDir string) (root string, absoluteWorkingDir string, hasGitRepository bool, err error) {
	if workingDir == "" {
		workingDir = "."
	}
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", "", false, err
	}
	absWorkingDir, err = filepath.EvalSymlinks(absWorkingDir)
	if err != nil {
		return "", "", false, err
	}
	root, err = gitadapter.RepositoryRootContext(ctx, absWorkingDir)
	hasGitRepository = true
	if errors.Is(err, gitadapter.ErrNotRepository) {
		hasGitRepository = false
		root = absWorkingDir
	} else if err != nil {
		return "", "", false, &LocalGitError{Operation: "resolve repository root", Cause: err}
	}
	return root, absWorkingDir, hasGitRepository, nil
}

func resolveTarget(ctx context.Context, options Options, root string, hasGitRepository bool, remote string) (Target, error) {
	target := Target{Root: root, Remote: remote}

	remoteURL, remoteErr := gitadapter.RemoteURLContext(ctx, root, target.Remote)
	if remoteErr != nil && hasGitRepository {
		return Target{}, &ResolveError{Code: ErrorConfiguredRemote, Remote: target.Remote, Cause: remoteErr}
	}
	if remoteErr == nil {
		target.RemoteURL = gitadapter.RedactRemoteURL(remoteURL)
	}
	inferred := ""
	if remoteURL != "" {
		remoteRepository, parseErr := gitadapter.ParseHostedRepositoryRemote(remoteURL)
		if parseErr != nil {
			return Target{}, parseErr
		}
		if !githubHostCompatible(remoteRepository.Host, options.GitHubAPIURL) {
			return Target{}, &ResolveError{Code: ErrorRemoteHost, Remote: target.Remote}
		}
		inferred = remoteRepository.Repository
	}

	identities := []repositoryIdentity{}
	for _, identity := range []repositoryIdentity{
		{source: "--repo", value: strings.TrimSpace(options.Repository)},
		{source: "GITHUB_REPOSITORY", value: strings.TrimSpace(options.EnvironmentRepo)},
		{source: fmt.Sprintf("remote %q", target.Remote), value: inferred},
	} {
		if identity.value == "" {
			continue
		}
		normalized, normalizeErr := gitadapter.NormalizeRepositoryName(identity.value)
		if normalizeErr != nil {
			return Target{}, &ResolveError{Code: ErrorInvalidRepository, LeftSource: identity.source, Left: identity.value, Cause: normalizeErr}
		}
		identity.value = normalized
		identities = append(identities, identity)
	}
	if len(identities) == 0 {
		return Target{}, &ResolveError{Code: ErrorMissingRepository}
	}
	for _, identity := range identities[1:] {
		if !strings.EqualFold(identities[0].value, identity.value) {
			return Target{}, &ResolveError{Code: ErrorRepositoryMismatch, LeftSource: identities[0].source, Left: identities[0].value, RightSource: identity.source, Right: identity.value, Remote: target.Remote}
		}
	}
	target.Repository = identities[0].value
	return target, nil
}

func githubHostCompatible(remoteHost, apiURL string) bool {
	remoteHost = strings.ToLower(remoteHost)
	if remoteHost == "ssh.github.com" {
		remoteHost = "github.com"
	}
	apiHost := "api.github.com"
	if strings.TrimSpace(apiURL) != "" {
		parsed, err := url.Parse(apiURL)
		if err != nil || parsed.Hostname() == "" {
			return false
		}
		apiHost = strings.ToLower(parsed.Hostname())
	}
	if apiHost == "api.github.com" {
		return remoteHost == "github.com"
	}
	return remoteHost == apiHost || strings.TrimPrefix(apiHost, "api.") == remoteHost
}

func loadConfig(root, workingDir, explicitPath string) (config.Config, string, error) {
	if explicitPath == "" {
		cfg, path, err := config.LoadForRepoWithPath(root)
		return cfg, path, err
	}
	path := explicitPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return config.Config{}, "", err
	}
	cfg, err := config.Load(abs)
	return cfg, abs, err
}

type repositoryIdentity struct {
	source string
	value  string
}
