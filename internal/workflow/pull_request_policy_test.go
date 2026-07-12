package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
)

type pullRequestPolicyGitHub struct {
	repo         string
	issueNumbers []int
	prNumber     int
}

func (f *pullRequestPolicyGitHub) FetchIssueLabelsContext(_ context.Context, repo string, issueNumbers []int) ([]gh.ReferencedIssue, error) {
	f.repo, f.issueNumbers = repo, append([]int(nil), issueNumbers...)
	return []gh.ReferencedIssue{{Number: 7, Labels: []string{"agent:ready-trivial"}}}, nil
}

func (f *pullRequestPolicyGitHub) FetchCommitListingContext(_ context.Context, repo string, prNumber int) (gh.CommitListing, error) {
	f.repo, f.prNumber = repo, prNumber
	return gh.CommitListing{Messages: []string{"Implement policy"}, Count: 250, GitHubCapReached: true}, nil
}

func TestPullRequestPolicyWorkflowAcquiresEventFacts(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "baton.yml")
	configContent, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &pullRequestPolicyGitHub{}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if client.repo != "event/repo" || client.prNumber != 12 || len(client.issueNumbers) != 1 || client.issueNumbers[0] != 7 {
		t.Fatalf("repo=%q pr=%d issues=%v", client.repo, client.prNumber, client.issueNumbers)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "commit listing reached") {
		t.Fatalf("decision errors = %v", decision.Errors)
	}
}

func TestPullRequestPolicyWorkflowRejectsRepositoryMismatchBeforeClient(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"work","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		t.Fatal("repository mismatch must not construct a GitHub client")
		return nil, nil
	}}
	_, err := workflow.Run(PullRequestPolicyInput{
		EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), Repository: "flag/repo",
	})
	if err == nil {
		t.Fatal("expected repository mismatch")
	}
}
