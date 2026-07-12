package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	gitadapter "github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/policy"
)

type PullRequestPolicyInput struct {
	FixturePath     string
	EventPath       string
	ConfigPath      string
	Repository      string
	EnvironmentRepo string
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
}

type PullRequestPolicyGitHub interface {
	FetchIssueLabelsContext(context.Context, string, []int) ([]gh.ReferencedIssue, error)
	FetchCommitListingContext(context.Context, string, int) (gh.CommitListing, error)
}

type PullRequestPolicyWorkflow struct {
	newClient func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error)
}

func NewPullRequestPolicyWorkflow() PullRequestPolicyWorkflow {
	return PullRequestPolicyWorkflow{newClient: func(ctx context.Context, input PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow PullRequestPolicyWorkflow) Run(input PullRequestPolicyInput) (policy.PRPolicyDecision, error) {
	return workflow.RunContext(context.Background(), input)
}

func (workflow PullRequestPolicyWorkflow) RunContext(ctx context.Context, input PullRequestPolicyInput) (policy.PRPolicyDecision, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	if (input.FixturePath == "") == (input.EventPath == "") {
		return policy.PRPolicyDecision{}, apperror.New(apperror.Usage, "pull request policy requires exactly one fixture or event", "")
	}
	cfg, err := loadWorkflowConfig(input.ConfigPath)
	if err != nil {
		return policy.PRPolicyDecision{}, err
	}
	policyInput := policy.PRPolicyInput{Policy: cfg}
	if input.FixturePath != "" {
		content, err := os.ReadFile(input.FixturePath)
		if err != nil {
			return policy.PRPolicyDecision{}, apperror.Wrap(apperror.Usage, "PR policy fixture could not be read", err, "")
		}
		if err := json.Unmarshal(content, &policyInput); err != nil {
			return policy.PRPolicyDecision{}, apperror.Wrap(apperror.Usage, "PR policy fixture could not be parsed", err, "")
		}
		policyInput.Policy = cfg
		return policy.ComputePullRequestPolicy(policyInput), nil
	}
	content, err := os.ReadFile(input.EventPath)
	if err != nil {
		return policy.PRPolicyDecision{}, apperror.Wrap(apperror.Usage, "pull request event could not be read", err, "")
	}
	event, err := gh.ParsePullRequestEvent(content)
	if err != nil {
		return policy.PRPolicyDecision{}, apperror.Wrap(apperror.Usage, "pull request event could not be parsed", err, "")
	}
	policyInput.PullRequest = policyPullRequest(event)
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
		repositoryIdentityInput{source: "pull_request.base.repo.full_name", value: event.BaseRepositoryFullName},
	)
	if err != nil {
		return policy.PRPolicyDecision{}, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return policy.PRPolicyDecision{}, err
	}
	issueNumbers := pullRequestIssueNumbers(event, cfg.PRPolicy.RequiredReferenceKeyword)
	if len(issueNumbers) > 0 {
		issues, err := client.FetchIssueLabelsContext(ctx, repo, issueNumbers)
		if err != nil {
			return policy.PRPolicyDecision{}, classifyGitHubError(err)
		}
		policyInput.ReferencedIssues = make([]policy.ReferencedIssue, 0, len(issues))
		for _, issue := range issues {
			policyInput.ReferencedIssues = append(policyInput.ReferencedIssues, policy.ReferencedIssue{Number: issue.Number, Labels: issue.Labels})
		}
	}
	listing, err := client.FetchCommitListingContext(ctx, repo, event.Number)
	if err != nil {
		return policy.PRPolicyDecision{}, classifyGitHubError(err)
	}
	policyInput.CommitMessages = listing.Messages
	policyInput.CommitListingReachedCap = listing.GitHubCapReached
	return policy.ComputePullRequestPolicy(policyInput), nil
}

func loadWorkflowConfig(path string) (config.Config, error) {
	var cfg config.Config
	var err error
	if path == "" {
		root := "."
		if resolved, resolveErr := gitadapter.RepositoryRoot(root); resolveErr == nil {
			root = resolved
		}
		cfg, err = config.LoadForRepo(root)
	} else {
		cfg, err = config.Load(path)
	}
	if err != nil {
		hint := "Check the Baton config path and contents, then retry."
		if errors.Is(err, config.ErrConfigNotFound) {
			hint = ""
		}
		return config.Config{}, apperror.Wrap(apperror.Config, err.Error(), err, hint)
	}
	return cfg, nil
}

func policyPullRequest(event gh.PullRequestEvent) policy.PullRequest {
	return policy.PullRequest{
		Number: event.Number, Title: event.Title, Body: event.Body,
		HeadRef: event.HeadRef, BaseRef: event.BaseRef,
		HeadRepositoryFullName: event.HeadRepositoryFullName,
		BaseRepositoryFullName: event.BaseRepositoryFullName,
	}
}

func pullRequestIssueNumbers(event gh.PullRequestEvent, referenceKeyword string) []int {
	values := append(
		policy.ExtractReferenceIssueNumbersForPolicy(event.Title, referenceKeyword),
		policy.ExtractReferenceIssueNumbersForPolicy(event.Body, referenceKeyword)...,
	)
	values = append(values, policy.ExtractClosingIssueNumbers(event.Title)...)
	values = append(values, policy.ExtractClosingIssueNumbers(event.Body)...)
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, number := range values {
		if _, exists := seen[number]; exists {
			continue
		}
		seen[number] = struct{}{}
		result = append(result, number)
	}
	return result
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
