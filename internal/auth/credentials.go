package auth

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

type Source string

const (
	SourceGitHubToken Source = "GITHUB_TOKEN"
	SourceGHToken     Source = "GH_TOKEN"
	SourceGHCLI       Source = "gh"
)

var ErrCredentialsNotFound = errors.New("GITHUB_TOKEN, GH_TOKEN, or gh auth token is required")

type Inputs struct {
	GitHubToken string
	GHToken     string
}

type Credentials struct {
	token  string
	source Source
}

func (c Credentials) Token() string   { return c.token }
func (c Credentials) Source() Source  { return c.source }
func (c Credentials) Available() bool { return c.token != "" }

func Discover(inputs Inputs) (Credentials, error) {
	return DiscoverContext(context.Background(), inputs)
}

func DiscoverContext(ctx context.Context, inputs Inputs) (Credentials, error) {
	credentials, err := discover(inputs, func() (string, error) {
		output, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
		return string(output), err
	})
	if ctx.Err() != nil && !credentials.Available() {
		return Credentials{}, ctx.Err()
	}
	return credentials, err
}

func discover(inputs Inputs, ghToken func() (string, error)) (Credentials, error) {
	if token := strings.TrimSpace(inputs.GitHubToken); token != "" {
		return Credentials{token: token, source: SourceGitHubToken}, nil
	}
	if token := strings.TrimSpace(inputs.GHToken); token != "" {
		return Credentials{token: token, source: SourceGHToken}, nil
	}
	if ghToken != nil {
		if output, err := ghToken(); err == nil {
			if token := strings.TrimSpace(output); token != "" {
				return Credentials{token: token, source: SourceGHCLI}, nil
			}
		}
	}
	return Credentials{}, ErrCredentialsNotFound
}
