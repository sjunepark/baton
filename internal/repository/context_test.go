package repository

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestResolveBindsNestedCheckoutConfigRemoteAndBranches(t *testing.T) {
	root := newRepository(t, "git@github.com:example/project.git")
	nested := filepath.Join(root, "nested", "work")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	context, err := Resolve(Options{WorkingDir: nested, Repository: "example/project"})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if context.Root != wantRoot {
		t.Fatalf("root = %q, want %q", context.Root, wantRoot)
	}
	if context.Repository != "example/project" || context.Remote != "origin" {
		t.Fatalf("context identity = %+v", context)
	}
	if context.ConfigPath != filepath.Join(wantRoot, ".github", "baton.yml") {
		t.Fatalf("config path = %q", context.ConfigPath)
	}
	if context.Config.Repository.StagingBranch != "agent" {
		t.Fatalf("staging branch = %q", context.Config.Repository.StagingBranch)
	}
}

func TestResolveUsesConfiguredRemote(t *testing.T) {
	root := newRepository(t, "git@github.com:wrong/project.git")
	runGit(t, root, "remote", "add", "upstream", "ssh://git@github.com/example/project.git")
	cfg := config.DefaultConfig()
	cfg.Repository.DefaultRemote = "upstream"
	writeConfig(t, root, cfg)

	context, err := Resolve(Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if context.Repository != "example/project" || context.Remote != "upstream" {
		t.Fatalf("context = %+v", context)
	}
}

func TestResolveAcceptsMatchingGitHubEnterpriseHost(t *testing.T) {
	root := newRepository(t, "https://github.example.com/example/project.git")

	context, err := Resolve(Options{WorkingDir: root, GitHubAPIURL: "https://github.example.com/api/v3"})
	if err != nil {
		t.Fatal(err)
	}
	if context.Repository != "example/project" {
		t.Fatalf("repository = %q", context.Repository)
	}
}

func TestResolveRejectsRemoteFromDifferentHost(t *testing.T) {
	root := newRepository(t, "https://gitlab.com/example/project.git")

	_, err := Resolve(Options{WorkingDir: root, Repository: "example/project"})
	var resolveErr *ResolveError
	if !errors.As(err, &resolveErr) || resolveErr.Code != ErrorRemoteHost {
		t.Fatalf("error = %v, want remote-host error", err)
	}
}

func TestResolveRequiresConfiguredRemoteInGitCheckout(t *testing.T) {
	root := newRepository(t, "https://github.com/example/project.git")
	cfg := config.DefaultConfig()
	cfg.Repository.DefaultRemote = "missing"
	writeConfig(t, root, cfg)

	if _, err := Resolve(Options{WorkingDir: root, Repository: "example/project"}); err == nil {
		t.Fatal("expected configured-remote error")
	}
}

func TestResolveRejectsRepositoryMismatch(t *testing.T) {
	root := newRepository(t, "https://github.com/example/local.git")

	_, err := Resolve(Options{WorkingDir: root, Repository: "example/other"})
	var resolveErr *ResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error = %v, want ResolveError", err)
	}
	if resolveErr.Code != ErrorRepositoryMismatch || resolveErr.Right != "example/local" {
		t.Fatalf("resolve error = %+v", resolveErr)
	}
}

func TestResolveRejectsExplicitEnvironmentMismatch(t *testing.T) {
	root := newRepository(t, "https://github.com/example/project.git")

	_, err := Resolve(Options{WorkingDir: root, Repository: "example/project", EnvironmentRepo: "example/other"})
	var resolveErr *ResolveError
	if !errors.As(err, &resolveErr) || resolveErr.Code != ErrorRepositoryMismatch {
		t.Fatalf("error = %v, want repository mismatch", err)
	}
}

func TestResolveMismatchDoesNotExposeRemoteCredentials(t *testing.T) {
	root := newRepository(t, "https://user:secret@github.com/example/local.git")

	_, err := Resolve(Options{WorkingDir: root, Repository: "example/other"})
	if err == nil {
		t.Fatal("expected repository mismatch")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "user") {
		t.Fatalf("error exposes remote credentials: %v", err)
	}
}

func TestResolveAcceptsExplicitRepositoryOutsideGitCheckout(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, config.DefaultConfig())

	context, err := Resolve(Options{WorkingDir: root, Repository: "example/project"})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if context.Root != wantRoot || context.Repository != "example/project" || context.RemoteURL != "" {
		t.Fatalf("context = %+v", context)
	}
}

func TestResolveUsesExplicitRepositoryWhenRemoteIsNotHosted(t *testing.T) {
	root := newRepository(t, "/srv/git/example/project.git")

	context, err := Resolve(Options{WorkingDir: root, Repository: "example/project"})
	if err != nil {
		t.Fatal(err)
	}
	if context.Repository != "example/project" || context.RemoteURL != "/srv/git/example/project.git" {
		t.Fatalf("context = %+v", context)
	}
}

func TestResolveExplicitRelativeConfigUsesInvocationDirectory(t *testing.T) {
	root := newRepository(t, "https://github.com/example/project.git")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Repository.StagingBranch = "staging"
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "custom.yml"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	context, err := Resolve(Options{WorkingDir: nested, Repository: "example/project", ConfigPath: "custom.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if context.Config.Repository.StagingBranch != "staging" {
		t.Fatalf("staging branch = %q", context.Config.Repository.StagingBranch)
	}
}

func TestResolveTargetDoesNotRequireBatonConfig(t *testing.T) {
	root := newRepository(t, "https://github.com/example/project.git")
	if err := os.Remove(filepath.Join(root, ".github", "baton.yml")); err != nil {
		t.Fatal(err)
	}
	target, err := ResolveTarget(Options{WorkingDir: root, Repository: "example/project"}, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if target.Repository != "example/project" || target.Remote != "origin" || target.Root == "" {
		t.Fatalf("target = %+v", target)
	}
}

func newRepository(t *testing.T, remoteURL string) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", remoteURL)
	writeConfig(t, root, config.DefaultConfig())
	return root
}

func writeConfig(t *testing.T, root string, cfg config.Config) {
	t.Helper()
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "baton.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
}
