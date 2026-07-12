package auth

import (
	"context"
	"errors"
	"testing"
)

func TestDiscoverCredentialPrecedenceAndSource(t *testing.T) {
	credentials, err := discover(Inputs{GitHubToken: " github ", GHToken: "gh"}, func() (string, error) {
		t.Fatal("gh fallback should not run")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token() != "github" || credentials.Source() != SourceGitHubToken {
		t.Fatalf("credentials = source %q token %q", credentials.Source(), credentials.Token())
	}
}

func TestDiscoverContextPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	credentials, err := DiscoverContext(ctx, Inputs{})
	if !errors.Is(err, context.Canceled) || credentials.Available() {
		t.Fatalf("credentials=%+v error=%v", credentials, err)
	}
}

func TestDiscoverFallsBackToGHCLI(t *testing.T) {
	credentials, err := discover(Inputs{}, func() (string, error) { return " cli-token\n", nil })
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token() != "cli-token" || credentials.Source() != SourceGHCLI {
		t.Fatalf("credentials = source %q token %q", credentials.Source(), credentials.Token())
	}
}

func TestDiscoverMissingCredentialsDoesNotExposeCommandFailure(t *testing.T) {
	credentials, err := discover(Inputs{}, func() (string, error) { return "secret", errors.New("command failed with secret") })
	if !errors.Is(err, ErrCredentialsNotFound) || credentials.Available() {
		t.Fatalf("credentials=%+v error=%v", credentials, err)
	}
	if err.Error() != ErrCredentialsNotFound.Error() {
		t.Fatalf("error = %q", err)
	}
}
