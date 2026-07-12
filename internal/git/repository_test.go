package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubRepositoryFromRemote(t *testing.T) {
	tests := map[string]string{
		"git@github.com:example/project.git":             "example/project",
		"ssh://git@github.com/example/project.git":       "example/project",
		"https://github.com/example/project.git":         "example/project",
		"http://github.com/example/project":              "example/project",
		"https://github.example.com/example/project.git": "example/project",
	}
	for remote, want := range tests {
		t.Run(remote, func(t *testing.T) {
			got, err := GitHubRepositoryFromRemote(remote)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("repository = %q, want %q", got, want)
			}
		})
	}
}

func TestGitHubRepositoryFromRemoteRejectsMalformedPath(t *testing.T) {
	for _, remote := range []string{"https://github.com/example/project/extra.git", "example/project"} {
		if _, err := GitHubRepositoryFromRemote(remote); err == nil {
			t.Fatalf("expected malformed-path error for %q", remote)
		}
	}
}

func TestNormalizeRepositoryNameRejectsPathSensitiveCharacters(t *testing.T) {
	for _, repository := range []string{"owner/repo?ref=x", "owner/repo#fragment", "owner name/repo", "owner/repo/extra", "../repo"} {
		if _, err := NormalizeRepositoryName(repository); err == nil {
			t.Fatalf("expected invalid repository error for %q", repository)
		}
	}
}

func TestRedactRemoteURLRemovesCredentials(t *testing.T) {
	got := RedactRemoteURL("https://user:secret@github.com/example/project.git?token=other#fragment")
	if got != "https://github.com/example/project.git" {
		t.Fatalf("redacted remote = %q", got)
	}
	if got := RedactRemoteURL("token@github.com:example/project.git"); got != "github.com:example/project.git" {
		t.Fatalf("redacted SCP remote = %q", got)
	}
}

func TestRepositoryRootDoesNotMaskOtherExit128Failures(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("invalid gitfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := RepositoryRoot(root)
	if err == nil || errors.Is(err, ErrNotRepository) {
		t.Fatalf("error = %v, want non-sentinel git failure", err)
	}
}
