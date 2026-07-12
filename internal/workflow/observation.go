package workflow

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/snapshot"
)

type RepositoryInput struct {
	WorkingDir      string
	Repository      string
	ConfigPath      string
	EnvironmentRepo string
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
}

type ObservationGitHub interface {
	ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error)
	ListOpenPullRequestsContext(context.Context, string, string) ([]gh.PullRequest, error)
	ListClosedPullRequestsContext(context.Context, string, string) ([]gh.PullRequest, error)
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetCheckRollupContext(context.Context, string, int, string) (gh.CheckRollup, error)
	GetBranchHealthContext(context.Context, string, string) (*gh.BranchHealth, error)
	GetBranchContext(context.Context, string, string) (gh.Branch, error)
	GetClassicBranchRulesContext(context.Context, string, string) (gh.BranchRules, error)
	GetEffectiveBranchRulesContext(context.Context, string, string) (gh.BranchRules, error)
	GetReviewThreadsContext(context.Context, string, int) (gh.ReviewThreadResult, error)
	ListPullRequestReviewsContext(context.Context, string, int) ([]gh.PullRequestReview, error)
	GetRequestedReviewersContext(context.Context, string, int) ([]gh.ReviewRequest, error)
}

type ObservationWorkflow struct {
	resolve         func(context.Context, repository.Options) (repository.Context, error)
	newClient       func(context.Context, RepositoryInput) (ObservationGitHub, error)
	enrichmentLimit int
	now             func() time.Time
}

type PullRequestsResult struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	Repo          string            `json:"repo"`
	Count         int               `json:"count"`
	Counts        PullRequestCounts `json:"counts"`
	PullRequests  []queue.PullState `json:"pullRequests"`
	Help          []string          `json:"help,omitempty"`
}

type PullRequestCounts struct {
	Success int `json:"success"`
	Failure int `json:"failure"`
	Pending int `json:"pending"`
	Unknown int `json:"unknown"`
}

func NewObservationWorkflow() ObservationWorkflow {
	return ObservationWorkflow{
		resolve: repository.ResolveContext,
		newClient: func(ctx context.Context, input RepositoryInput) (ObservationGitHub, error) {
			credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
			if err != nil {
				return nil, apperror.Wrap(apperror.Auth, err.Error(), err, "")
			}
			return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
		},
		enrichmentLimit: 4,
		now:             time.Now,
	}
}

func (workflow ObservationWorkflow) Queue(input RepositoryInput) (queue.Snapshot, error) {
	return workflow.QueueContext(context.Background(), input)
}

func (workflow ObservationWorkflow) QueueContext(ctx context.Context, input RepositoryInput) (queue.Snapshot, error) {
	repositorySnapshot, err := workflow.SnapshotContext(ctx, input, "")
	if err != nil {
		return queue.Snapshot{}, err
	}
	if repositorySnapshot.Completeness != snapshot.Complete {
		return queue.Snapshot{}, incompleteWarningsError(repositorySnapshot.Warnings)
	}
	return repositorySnapshot.QueueV1(), nil
}

func (workflow ObservationWorkflow) PullRequests(input RepositoryInput) (queue.Snapshot, error) {
	return workflow.PullRequestsContext(context.Background(), input)
}

func (workflow ObservationWorkflow) PullRequestsContext(ctx context.Context, input RepositoryInput) (queue.Snapshot, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	return workflow.acquire(ctx, input, true)
}

func (workflow ObservationWorkflow) PullRequestListing(input RepositoryInput) (PullRequestsResult, error) {
	return workflow.PullRequestListingContext(context.Background(), input)
}

func (workflow ObservationWorkflow) PullRequestListingContext(ctx context.Context, input RepositoryInput) (PullRequestsResult, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	snapshot, err := workflow.acquire(ctx, input, true)
	if err != nil {
		return PullRequestsResult{}, err
	}
	return BuildPullRequestsResult(snapshot.Repo, snapshot.PullRequests), nil
}

func (workflow ObservationWorkflow) Next(input RepositoryInput, requestedAction string) (queue.NextCandidates, error) {
	return workflow.NextContext(context.Background(), input, requestedAction)
}

func (workflow ObservationWorkflow) NextContext(ctx context.Context, input RepositoryInput, requestedAction string) (queue.NextCandidates, error) {
	repositorySnapshot, err := workflow.SnapshotContext(ctx, input, requestedAction)
	if err != nil {
		return queue.NextCandidates{}, err
	}
	if repositorySnapshot.Completeness != snapshot.Complete {
		return queue.NextCandidates{}, incompleteWarningsError(repositorySnapshot.Warnings)
	}
	return repositorySnapshot.NextV2(), nil
}

func (workflow ObservationWorkflow) Snapshot(input RepositoryInput, requestedAction string) (snapshot.RepositorySnapshot, error) {
	return workflow.SnapshotContext(context.Background(), input, requestedAction)
}

func (workflow ObservationWorkflow) SnapshotContext(ctx context.Context, input RepositoryInput, requestedAction string) (snapshot.RepositorySnapshot, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	startedAt := workflow.currentTime()
	facts, cfg, err := workflow.acquireRecommendationFacts(ctx, input)
	if err != nil {
		return snapshot.RepositorySnapshot{}, err
	}
	completedAt := workflow.currentTime()
	queueProjection := queueSnapshotFromRecommendationFacts(facts, cfg)
	result, err := snapshot.Build(snapshot.BuildInput{
		Facts: facts, Queue: queueProjection, StartedAt: startedAt, CompletedAt: completedAt, RequestedAction: requestedAction,
	})
	if err != nil {
		return snapshot.RepositorySnapshot{}, apperror.Wrap(apperror.GitHub, "repository snapshot could not be built", err, "")
	}
	return result, nil
}

func (workflow ObservationWorkflow) currentTime() time.Time {
	if workflow.now == nil {
		return time.Now().UTC()
	}
	return workflow.now().UTC()
}

func (workflow ObservationWorkflow) acquire(ctx context.Context, input RepositoryInput, includeChecks bool) (queue.Snapshot, error) {
	repositoryContext, err := workflow.resolve(ctx, repository.Options{
		WorkingDir: input.WorkingDir, Repository: input.Repository, ConfigPath: input.ConfigPath,
		EnvironmentRepo: input.EnvironmentRepo, GitHubAPIURL: input.GitHubAPIURL,
	})
	if err != nil {
		return queue.Snapshot{}, classifyRepositoryError(err)
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		if apperror.As(err) != nil {
			return queue.Snapshot{}, err
		}
		return queue.Snapshot{}, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
	}

	issues, err := client.ListOpenIssuesContext(ctx, repositoryContext.Repository)
	if err != nil {
		return queue.Snapshot{}, classifyGitHubError(err)
	}
	prs, err := client.ListOpenPullRequestsContext(ctx, repositoryContext.Repository, repositoryContext.Config.Repository.StagingBranch)
	if err != nil {
		return queue.Snapshot{}, classifyGitHubError(err)
	}
	if repositoryContext.Config.Repository.BaseBranch != "" && repositoryContext.Config.Repository.BaseBranch != repositoryContext.Config.Repository.StagingBranch {
		promotionPRs, err := client.ListOpenPullRequestsContext(ctx, repositoryContext.Repository, repositoryContext.Config.Repository.BaseBranch)
		if err != nil {
			return queue.Snapshot{}, classifyGitHubError(err)
		}
		for _, pr := range promotionPRs {
			if pr.HeadRef == repositoryContext.Config.Repository.StagingBranch {
				prs = append(prs, pr)
			}
		}
	}
	if includeChecks {
		if err := workflow.enrichChecks(ctx, client, repositoryContext.Repository, prs); err != nil {
			return queue.Snapshot{}, classifyGitHubError(err)
		}
	}
	branchHealth, err := client.GetBranchHealthContext(ctx, repositoryContext.Repository, repositoryContext.Config.Repository.StagingBranch)
	if err != nil {
		if gh.IsNotFound(err) {
			return queue.Snapshot{}, apperror.New(
				apperror.Config,
				"staging branch \""+repositoryContext.Config.Repository.StagingBranch+"\" was not found in "+repositoryContext.Repository,
				"Run `baton ensure-branch --json`, then `baton ensure-branch --apply` after reviewing the plan.",
			)
		}
		return queue.Snapshot{}, classifyGitHubError(err)
	}
	return queue.BuildSnapshotWithBranchHealth(
		repositoryContext.Repository,
		repositoryContext.Config,
		queueIssues(issues),
		queuePullRequests(prs),
		queueBranchHealth(branchHealth),
	), nil
}

func (workflow ObservationWorkflow) enrichChecks(ctx context.Context, client ObservationGitHub, repo string, prs []gh.PullRequest) error {
	if len(prs) == 0 {
		return nil
	}
	limit := workflow.enrichmentLimit
	if limit <= 0 {
		limit = 4
	}
	if limit > len(prs) {
		limit = len(prs)
	}
	jobs := make(chan int, len(prs))
	errs := make([]error, len(prs))
	for index := range prs {
		jobs <- index
	}
	close(jobs)
	var wait sync.WaitGroup
	wait.Add(limit)
	for range limit {
		go func() {
			defer wait.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					errs[index] = err
					continue
				}
				rollup, err := client.GetCheckRollupContext(ctx, repo, prs[index].Number, prs[index].HeadSHA)
				if err != nil {
					errs[index] = err
					continue
				}
				prs[index].CheckState = rollup.State
			}
		}()
	}
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func classifyRepositoryError(err error) error {
	var resolveErr *repository.ResolveError
	if errors.As(err, &resolveErr) {
		if resolveErr.Code == repository.ErrorMissingRepository || resolveErr.Code == repository.ErrorInvalidRepository {
			return apperror.Wrap(apperror.Usage, resolveErr.Error(), err, "")
		}
		return apperror.Wrap(apperror.Config, resolveErr.Error(), err, "")
	}
	var localGitErr *repository.LocalGitError
	if errors.As(err, &localGitErr) {
		return apperror.Wrap(apperror.LocalGit, "local repository inspection failed", err, "")
	}
	return apperror.Wrap(apperror.Config, err.Error(), err, "")
}

func classifyGitHubError(err error) error {
	return apperror.WrapUpstream(err)
}

func operationFailure(category, message string, err error) *operation.Failure {
	retryable := false
	if applicationError := apperror.As(err); applicationError != nil {
		retryable = applicationError.Retryable
	}
	return &operation.Failure{Category: category, Message: message, Retryable: retryable}
}

func queueIssues(issues []gh.Issue) []queue.Issue {
	result := make([]queue.Issue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, queue.Issue{Number: issue.Number, Title: issue.Title, URL: issue.URL, Body: issue.Body, Labels: issue.Labels})
	}
	return result
}

func queuePullRequests(prs []gh.PullRequest) []queue.PullRequest {
	result := make([]queue.PullRequest, 0, len(prs))
	for _, pr := range prs {
		result = append(result, queue.PullRequest{
			Number: pr.Number, Title: pr.Title, URL: pr.URL, Body: pr.Body,
			BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA, CheckState: pr.CheckState, State: pr.State, Merged: pr.Merged,
		})
	}
	return result
}

func queueBranchHealth(branch *gh.BranchHealth) *queue.BranchHealth {
	if branch == nil {
		return nil
	}
	return &queue.BranchHealth{Ref: branch.Ref, SHA: branch.SHA, CheckState: branch.CheckState}
}

func BuildPullRequestsResult(repo string, pullRequests []queue.PullState) PullRequestsResult {
	counts := PullRequestCounts{}
	for _, pullRequest := range pullRequests {
		switch pullRequest.PullRequest.CheckState {
		case "success":
			counts.Success++
		case "failure":
			counts.Failure++
		case "pending":
			counts.Pending++
		default:
			counts.Unknown++
		}
	}
	help := []string{
		"Run `baton pr <number> --json` for PR details.",
		"Run `baton checks <number> --json` to inspect check status.",
		"Run `baton review-threads <number> --json` before completing review work.",
	}
	if len(pullRequests) == 0 {
		help = []string{"Run `baton queue --json` to inspect eligible issue work."}
	}
	return PullRequestsResult{
		SchemaVersion: 1, Kind: "pullRequests", Repo: repo, Count: len(pullRequests),
		Counts: counts, PullRequests: pullRequests, Help: help,
	}
}
