package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
)

type pullRequestPolicyGitHub struct {
	repo             string
	issueNumbers     []int
	prNumber         int
	promotionBaseSHA string
	promotionHeadSHA string
	stagingBranch    string
	workBranchPrefix string
	promotionHistory gh.PromotionHistory
}

func (f *pullRequestPolicyGitHub) FetchIssueLabelsContext(_ context.Context, repo string, issueNumbers []int) ([]gh.ReferencedIssue, error) {
	f.repo, f.issueNumbers = repo, append([]int(nil), issueNumbers...)
	return []gh.ReferencedIssue{{Number: 7, Labels: []string{"agent:ready-trivial"}}}, nil
}

func (f *pullRequestPolicyGitHub) FetchCommitListingContext(_ context.Context, repo string, prNumber int) (gh.CommitListing, error) {
	f.repo, f.prNumber = repo, prNumber
	return gh.CommitListing{Messages: []string{"Implement policy"}, Count: 250, GitHubCapReached: true}, nil
}

func (f *pullRequestPolicyGitHub) FetchPromotionHistoryContext(_ context.Context, repo, baseSHA, headSHA, stagingBranch, workBranchPrefix string) (gh.PromotionHistory, error) {
	f.repo = repo
	f.promotionBaseSHA = baseSHA
	f.promotionHeadSHA = headSHA
	f.stagingBranch = stagingBranch
	f.workBranchPrefix = workBranchPrefix
	return f.promotionHistory, nil
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

func TestPullRequestPolicyWorkflowUsesConfiguredReferenceKeyword(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.PRPolicy.RequiredReferenceKeyword = "Tracks"
	configContent, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Tracks #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &pullRequestPolicyGitHub{}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	if _, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"}); err != nil {
		t.Fatal(err)
	}
	if len(client.issueNumbers) != 1 || client.issueNumbers[0] != 7 {
		t.Fatalf("issues = %v", client.issueNumbers)
	}
}

func TestPullRequestPolicyWorkflowDerivesExpectedPromotionIssues(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Promote","body":"Closes #7","base":{"ref":"main","sha":"base-sha","repo":{"full_name":"event/repo"}},"head":{"ref":"agent","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &pullRequestPolicyGitHub{promotionHistory: gh.PromotionHistory{
		Complete: true,
		WorkPullRequests: []gh.PromotionWorkPullRequest{
			{Number: 20, Title: "First", Body: "Refs #7"},
			{Number: 21, Title: "Second", Body: "Refs #8, #7"},
		},
	}}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if client.promotionBaseSHA != "base-sha" || client.promotionHeadSHA != "head-sha" || client.stagingBranch != "agent" || client.workBranchPrefix != "agent-work/" {
		t.Fatalf("promotion query = base %q head %q staging %q prefix %q", client.promotionBaseSHA, client.promotionHeadSHA, client.stagingBranch, client.workBranchPrefix)
	}
	if decision.PromotionFacts == nil || !decision.PromotionFacts.Complete || len(decision.PromotionFacts.ExpectedIssues) != 2 || decision.PromotionFacts.ExpectedIssues[0] != 7 || decision.PromotionFacts.ExpectedIssues[1] != 8 {
		t.Fatalf("promotion facts = %+v", decision.PromotionFacts)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "missing: #8") {
		t.Fatalf("decision errors = %v", decision.Errors)
	}
}

func TestPullRequestPolicyWorkflowDegradesPromotionWhenIncludedWorkPRHasNoReference(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Promote","body":"","base":{"ref":"main","sha":"base-sha","repo":{"full_name":"event/repo"}},"head":{"ref":"agent","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &pullRequestPolicyGitHub{promotionHistory: gh.PromotionHistory{
		Complete:         true,
		WorkPullRequests: []gh.PromotionWorkPullRequest{{Number: 20, Title: "Missing reference"}},
	}}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.PromotionFacts == nil || decision.PromotionFacts.Complete || !strings.Contains(strings.Join(decision.Errors, "\n"), "evidence is incomplete") {
		t.Fatalf("decision = %+v", decision)
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

func TestPullRequestPolicyWorkflowSkipsGitHubForDirectBasePR(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Maintenance","body":"","base":{"ref":"main","repo":{"full_name":"event/repo"}},"head":{"ref":"maintenance","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		t.Fatal("direct base PR must not construct a GitHub client")
		return nil, nil
	}}
	decision, err := workflow.Run(PullRequestPolicyInput{
		EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Flow != policy.PRFlowDirectBase || len(decision.Errors) != 0 {
		t.Fatalf("decision = %+v", decision)
	}
}
