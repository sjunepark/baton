package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type TaskOptions struct {
	Repository      string
	EnvironmentRepo string
	WorkingDir      string
	GitHubAPIURL    string
	ReadRemote      func(context.Context, string) (string, error)
}

type TaskResolveError struct {
	Code    string
	Message string
	Hint    string
	Usage   bool
	Cause   error
}

func (e *TaskResolveError) Error() string { return e.Message }
func (e *TaskResolveError) Unwrap() error { return e.Cause }

// ResolveTaskRepositoryContext returns at the first authoritative source.
// Explicit and ambient repository use therefore never inspect local git.
func ResolveTaskRepositoryContext(ctx context.Context, options TaskOptions) (string, error) {
	if value := strings.TrimSpace(options.Repository); value != "" {
		return normalizeTaskRepository(value, "--repo")
	}
	if value := strings.TrimSpace(options.EnvironmentRepo); value != "" {
		return normalizeTaskRepository(value, "GITHUB_REPOSITORY")
	}
	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	readRemote := options.ReadRemote
	if readRemote == nil {
		readRemote = readOriginRemote
	}
	remote, err := readRemote(ctx, workingDir)
	if err != nil {
		return "", &TaskResolveError{
			Code: "missing_repository", Message: "a GitHub repository is required",
			Hint: "Pass --repo owner/name or set GITHUB_REPOSITORY.", Cause: err,
		}
	}
	host, repository, err := parseTaskRemote(remote)
	if err != nil {
		return "", &TaskResolveError{
			Code: "invalid_repository", Message: "the local origin does not identify a GitHub repository",
			Hint: "Pass --repo owner/name explicitly.", Cause: err,
		}
	}
	if !githubHostCompatible(host, options.GitHubAPIURL) {
		return "", &TaskResolveError{
			Code: "invalid_repository", Message: "the local origin host does not match the GitHub API host",
			Hint: "Pass --repo owner/name or set the matching GITHUB_API_URL.",
		}
	}
	return repository, nil
}

func normalizeTaskRepository(value, source string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validRepositoryPart(parts[0]) || !validRepositoryPart(parts[1]) {
		return "", &TaskResolveError{
			Code: "invalid_repository", Message: fmt.Sprintf("%s must be a GitHub owner/name", source),
			Hint: "Use a repository such as owner/name.", Usage: source == "--repo",
		}
	}
	return value, nil
}

func validRepositoryPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

func readOriginRemote(ctx context.Context, workingDir string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", "-C", workingDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func parseTaskRemote(remote string) (host, repository string, err error) {
	value := strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	var path string
	switch {
	case strings.Contains(value, "://"):
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Hostname() == "" {
			return "", "", errors.New("unsupported remote URL")
		}
		host = strings.ToLower(parsed.Hostname())
		path = strings.TrimPrefix(parsed.Path, "/")
	case strings.Contains(value, "@") && strings.Contains(value, ":"):
		separator := strings.Index(value, ":")
		host = strings.ToLower(value[strings.Index(value, "@")+1 : separator])
		path = value[separator+1:]
	default:
		return "", "", errors.New("unsupported remote URL")
	}
	repository, err = normalizeTaskRepository(path, "local origin")
	return host, repository, err
}
