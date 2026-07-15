package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTaskRepositoryPrecedenceStopsAtAuthoritativeSource(t *testing.T) {
	t.Parallel()
	broken := filepath.Join(t.TempDir(), "does-not-exist")
	tests := []struct {
		name      string
		options   TaskOptions
		want      string
		wantReads int
	}{
		{name: "explicit ignores environment and broken checkout", options: TaskOptions{Repository: "explicit/repo", EnvironmentRepo: "ambient/repo", WorkingDir: broken}, want: "explicit/repo"},
		{name: "environment ignores broken checkout", options: TaskOptions{EnvironmentRepo: "ambient/repo", WorkingDir: broken}, want: "ambient/repo"},
		{name: "local fallback", options: TaskOptions{}, want: "local/repo", wantReads: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			test.options.ReadRemote = func(context.Context, string) (string, error) {
				reads++
				return "git@github.com:local/repo.git", nil
			}
			got, err := ResolveTaskRepositoryContext(context.Background(), test.options)
			if err != nil || got != test.want || reads != test.wantReads {
				t.Fatalf("ResolveTaskRepositoryContext() = %q, %v, reads %d", got, err, reads)
			}
		})
	}
}

func TestTaskRepositoryRejectsInvalidSourcesSafely(t *testing.T) {
	t.Parallel()
	tests := []TaskOptions{
		{Repository: "https://github.com/owner/repo"},
		{EnvironmentRepo: "owner"},
		{ReadRemote: func(context.Context, string) (string, error) { return "/srv/repo.git", nil }},
		{ReadRemote: func(context.Context, string) (string, error) { return "https://gitlab.com/owner/repo.git", nil }},
	}
	for _, options := range tests {
		_, err := ResolveTaskRepositoryContext(context.Background(), options)
		var resolveErr *TaskResolveError
		if !errors.As(err, &resolveErr) || resolveErr.Code == "" || resolveErr.Hint == "" {
			t.Fatalf("error = %#v", err)
		}
	}
}

func TestTaskRepositoryAcceptsMatchingEnterpriseRemote(t *testing.T) {
	t.Parallel()
	got, err := ResolveTaskRepositoryContext(context.Background(), TaskOptions{
		GitHubAPIURL: "https://github.example.com/api/v3",
		ReadRemote: func(context.Context, string) (string, error) {
			return "https://github.example.com/owner/repo.git", nil
		},
	})
	if err != nil || got != "owner/repo" {
		t.Fatalf("ResolveTaskRepositoryContext() = %q, %v", got, err)
	}
}

func TestTaskRepositoryInfersOriginFromCheckout(t *testing.T) {
	root := t.TempDir()
	runTaskGit(t, root, "init")
	runTaskGit(t, root, "remote", "add", "origin", "https://github.com/owner/repo.git")
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "baton.yml"), []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTaskRepositoryContext(context.Background(), TaskOptions{WorkingDir: nested})
	if err != nil || got != "owner/repo" {
		t.Fatalf("ResolveTaskRepositoryContext() = %q, %v", got, err)
	}
}

func runTaskGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
}
