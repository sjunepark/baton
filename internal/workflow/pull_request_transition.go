package workflow

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/workitem"
)

type PullRequestTransitionInput struct {
	EventPath       string
	ConfigPath      string
	Repository      string
	EnvironmentRepo string
	Apply           bool
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
}

type PullRequestTransitionResult struct {
	workitem.TransitionPlan
	Report *operation.Report `json:"report,omitempty"`
}

type PullRequestTransitionGitHub interface {
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	AddIssueLabelsContext(context.Context, string, int, []string) error
}

type PullRequestTransitionWorkflow struct {
	newClient func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error)
}

func NewPullRequestTransitionWorkflow() PullRequestTransitionWorkflow {
	return PullRequestTransitionWorkflow{newClient: func(ctx context.Context, input PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow PullRequestTransitionWorkflow) RunContext(ctx context.Context, input PullRequestTransitionInput) (PullRequestTransitionResult, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	if strings.TrimSpace(input.EventPath) == "" {
		return PullRequestTransitionResult{}, apperror.New(apperror.Usage, "pr-transition requires --event", "")
	}
	cfg, err := loadWorkflowConfig(input.ConfigPath)
	if err != nil {
		return PullRequestTransitionResult{}, err
	}
	content, err := os.ReadFile(input.EventPath)
	if err != nil {
		return PullRequestTransitionResult{}, apperror.Wrap(apperror.Usage, "pull request event could not be read", err, "")
	}
	event, err := gh.ParsePullRequestEvent(content)
	if err != nil {
		return PullRequestTransitionResult{}, apperror.Wrap(apperror.Usage, "pull request event could not be parsed", err, "")
	}
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
		repositoryIdentityInput{source: "pull_request.base.repo.full_name", value: event.BaseRepositoryFullName},
	)
	if err != nil {
		return PullRequestTransitionResult{}, err
	}
	plannerEvent := workitem.PullRequestEvent{
		Repository: repo, Action: event.Action, Number: event.Number, Title: event.Title, Body: event.Body,
		BaseRef: event.BaseRef, HeadRef: event.HeadRef, BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA,
		State: event.State, Merged: event.Merged,
	}
	result := PullRequestTransitionResult{TransitionPlan: workitem.PlanPullRequestTransition(plannerEvent, cfg)}
	if !input.Apply {
		return result, nil
	}
	if len(result.Operations) == 0 {
		report := operation.NewReport(nil)
		result.Report = &report
		return result, nil
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return result, err
	}
	results := transitionOperationResults(repo, result.Operations)
	latest, err := client.GetPullRequestContext(ctx, repo, event.Number)
	if err != nil {
		classified := classifyGitHubError(err)
		report := transitionPreflightFailure(results, repo, event.Number, "get_pull_request", operation.StatusFailed, operationFailure("github", "pull request preflight failed", classified))
		result.Report = &report
		return result, apperror.WithReport(classified, report)
	}
	if !pullRequestEventStillCurrent(event, latest) {
		report := transitionPreflightFailure(results, repo, event.Number, "verify_pull_request_state", operation.StatusRefused, &operation.Failure{Category: "stale", Message: "pull request changed after the event was captured"})
		result.Report = &report
		stale := apperror.New(apperror.GitHub, "work-item transition plan is stale", "Let the newest pull request event run.")
		return result, apperror.WithReport(stale, report)
	}

	for index, planned := range result.Operations {
		issue, err := client.GetIssueContext(ctx, repo, planned.IssueNumber)
		if err != nil {
			classified := classifyGitHubError(err)
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "issue preflight failed", classified)
			report := operation.NewReport(results)
			result.Report = &report
			return result, apperror.WithReport(classified, report)
		}
		if issue.PullRequest {
			results[index].Status = operation.StatusRefused
			results[index].Error = &operation.Failure{Category: "github", Message: "referenced resource is a pull request, not an issue"}
			report := operation.NewReport(results)
			result.Report = &report
			refused := apperror.New(apperror.GitHub, "work-item transition references a pull request", "Reference GitHub issues with the configured keyword.")
			return result, apperror.WithReport(refused, report)
		}
		if strings.EqualFold(issue.State, "closed") || containsLabel(issue.Labels, planned.Label) {
			results[index].Status = operation.StatusUnchanged
		}
	}
	for index, planned := range result.Operations {
		if results[index].Status == operation.StatusUnchanged {
			continue
		}
		if err := client.AddIssueLabelsContext(ctx, repo, planned.IssueNumber, []string{planned.Label}); err != nil {
			classified := classifyGitHubError(err)
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "work-item transition failed", classified)
			report := operation.NewReport(results)
			result.Report = &report
			return result, apperror.WithReport(classified, report)
		}
		results[index].Status = operation.StatusApplied
	}
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func transitionOperationResults(repo string, planned []workitem.TransitionOperation) []operation.Result {
	results := make([]operation.Result, len(planned))
	for index, item := range planned {
		results[index] = operation.Result{ID: item.ID, Resource: fmt.Sprintf("%s#%d:%s", repo, item.IssueNumber, item.Label), Action: item.Action, Status: operation.StatusNotAttempted}
	}
	return results
}

func transitionPreflightFailure(results []operation.Result, repo string, number int, action string, status operation.Status, failure *operation.Failure) operation.Report {
	preflight := operation.Result{ID: "pull-request-state-preflight", Resource: fmt.Sprintf("%s#%d", repo, number), Action: action, Status: status, Error: failure}
	return operation.NewReport(append([]operation.Result{preflight}, results...))
}

func pullRequestEventStillCurrent(event gh.PullRequestEvent, latest gh.PullRequest) bool {
	return latest.Number == event.Number && latest.Title == event.Title && latest.Body == event.Body && latest.BaseRef == event.BaseRef && latest.HeadRef == event.HeadRef &&
		latest.HeadSHA == event.HeadSHA && strings.EqualFold(latest.State, event.State) && latest.Merged == event.Merged
}

func containsLabel(labels []string, wanted string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}
