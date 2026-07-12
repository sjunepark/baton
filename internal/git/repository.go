package git

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

var ErrNotRepository = errors.New("not a git repository")

// RepositoryRoot returns the canonical top-level directory containing start.
func RepositoryRoot(start string) (string, error) {
	return RepositoryRootContext(context.Background(), start)
}

func RepositoryRootContext(ctx context.Context, start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	out, err := OutputAtContext(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		if IsExitCode(err, 128) && StderrContains(err, "not a git repository") {
			return "", ErrNotRepository
		}
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSpace(out))
}

// RemoteURL returns the configured URL for remote in root.
func RemoteURL(root, remote string) (string, error) {
	return RemoteURLContext(context.Background(), root, remote)
}

func RemoteURLContext(ctx context.Context, root, remote string) (string, error) {
	out, err := OutputAtContext(ctx, root, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

type HostedRepository struct {
	Host       string
	Repository string
}

// ParseHostedRepositoryRemote extracts host and owner/name from common
// hosted-git remote forms, including GitHub Enterprise hosts.
func ParseHostedRepositoryRemote(remote string) (HostedRepository, error) {
	value := strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	var host, path string
	switch {
	case strings.Contains(value, "://"):
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return HostedRepository{}, errors.New("configured remote is not a supported repository URL")
		}
		host = parsed.Hostname()
		path = strings.TrimPrefix(parsed.Path, "/")
	case strings.Contains(value, "@") && strings.Contains(value, ":"):
		separator := strings.Index(value, ":")
		host = value[strings.Index(value, "@")+1 : separator]
		path = value[separator+1:]
	default:
		return HostedRepository{}, errors.New("configured remote is not a supported repository URL")
	}
	repository, err := NormalizeRepositoryName(path)
	if err != nil {
		return HostedRepository{}, errors.New("configured remote does not identify a GitHub owner/name repository")
	}
	return HostedRepository{Host: strings.ToLower(host), Repository: repository}, nil
}

func GitHubRepositoryFromRemote(remote string) (string, error) {
	parsed, err := ParseHostedRepositoryRemote(remote)
	return parsed.Repository, err
}

// RedactRemoteURL removes credentials, query parameters, and fragments before
// a remote is retained in application context or diagnostics.
func RedactRemoteURL(remote string) string {
	value := strings.TrimSpace(remote)
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return ""
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	if at := strings.Index(value, "@"); at >= 0 {
		return value[at+1:]
	}
	return ""
}

// NormalizeRepositoryName validates the owner/name identity used in GitHub API
// paths. It deliberately rejects URL delimiters, whitespace, and path segments.
func NormalizeRepositoryName(value string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("repository %q must be an owner/name", value)
	}
	for _, part := range parts {
		for _, char := range part {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.') {
				return "", fmt.Errorf("repository %q contains invalid characters", value)
			}
		}
	}
	return value, nil
}
