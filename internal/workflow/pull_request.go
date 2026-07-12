package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/workitem"
)

type PullRequestInput struct {
	WorkingDir      string
	Number          int
	Repository      string
	EnvironmentRepo string
	ConfigPath      string
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
	BodyLimit       int
	Full            bool
}

type PullRequestDashboard struct {
	SchemaVersion     int                         `json:"schemaVersion"`
	Kind              string                      `json:"kind"`
	Repo              string                      `json:"repo"`
	PullRequest       queue.PullRequest           `json:"pullRequest"`
	ReferencedIssues  []int                       `json:"referencedIssues"`
	IssueReadiness    []PullRequestIssueReadiness `json:"issueReadiness,omitempty"`
	Checks            PullRequestCheckSummary     `json:"checks"`
	ReviewThreads     PullRequestReviewSummary    `json:"reviewThreads"`
	LikelyNextCommand string                      `json:"likelyNextCommand"`
	Warnings          []string                    `json:"warnings,omitempty"`
	Help              []string                    `json:"help,omitempty"`
}

type PullRequestIssueReadiness struct {
	Number  int            `json:"number"`
	Found   bool           `json:"found"`
	State   workitem.State `json:"state"`
	Ready   bool           `json:"ready"`
	Action  string         `json:"action,omitempty"`
	Labels  []string       `json:"labels,omitempty"`
	Reasons []string       `json:"reasons"`
}

type PullRequestCheckSummary struct {
	State   string          `json:"state"`
	Count   int             `json:"count"`
	Summary gh.CheckSummary `json:"summary"`
}

type PullRequestReviewSummary struct {
	Count   int              `json:"count"`
	Summary gh.ThreadSummary `json:"summary"`
}

type PullRequestGitHub interface {
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetCheckRollupContext(context.Context, string, int, string) (gh.CheckRollup, error)
	GetReviewThreadsContext(context.Context, string, int) (gh.ReviewThreadResult, error)
	ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error)
}

type PullRequestWorkflow struct {
	resolve       func(context.Context, repository.Options) (repository.Context, error)
	resolveTarget func(context.Context, repository.Options, string) (repository.Target, error)
	newClient     func(context.Context, PullRequestInput) (PullRequestGitHub, error)
}

func NewPullRequestWorkflow() PullRequestWorkflow {
	return PullRequestWorkflow{
		resolve: repository.ResolveContext, resolveTarget: repository.ResolveTargetContext,
		newClient: func(ctx context.Context, input PullRequestInput) (PullRequestGitHub, error) {
			credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
			if err != nil {
				return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
			}
			return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
		},
	}
}

func (workflow PullRequestWorkflow) Dashboard(input PullRequestInput) (PullRequestDashboard, error) {
	return workflow.DashboardContext(context.Background(), input)
}

func (workflow PullRequestWorkflow) DashboardContext(ctx context.Context, input PullRequestInput) (PullRequestDashboard, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	session, err := workflow.acquire(ctx, input, true)
	if err != nil {
		return PullRequestDashboard{}, err
	}
	transportPR, err := session.client.GetPullRequestContext(ctx, session.repo, input.Number)
	if err != nil {
		return PullRequestDashboard{}, classifyGitHubError(err)
	}
	pr := queuePullRequests([]gh.PullRequest{transportPR})[0]
	result := buildPullRequestDashboard(session.repo, pr, *session.config)
	checksAvailable := false
	checks, err := session.client.GetCheckRollupContext(ctx, session.repo, pr.Number, pr.HeadSHA)
	if err == nil && checks.Complete {
		checksAvailable = true
		result.PullRequest.CheckState = checks.State
		result.Checks = PullRequestCheckSummary{State: checks.State, Count: checks.Count, Summary: checks.Summary}
	} else if err == nil {
		result.Warnings = append(result.Warnings, "check summary incomplete")
	} else {
		result.Warnings = append(result.Warnings, "check summary unavailable")
	}
	reviewsAvailable := false
	threads, err := session.client.GetReviewThreadsContext(ctx, session.repo, pr.Number)
	if err == nil && threads.Complete {
		reviewsAvailable = true
		result.ReviewThreads = PullRequestReviewSummary{Count: threads.Count, Summary: threads.Summary}
	} else if err == nil {
		result.Warnings = append(result.Warnings, "review thread summary incomplete")
	} else {
		result.Warnings = append(result.Warnings, "review thread summary unavailable")
	}
	workflow.addIssueReadiness(ctx, &result, session)
	result.LikelyNextCommand = PullRequestLikelyNextCommand(pr.Number, result.Checks.Summary, result.ReviewThreads.Summary, checksAvailable, reviewsAvailable)
	result.Help = PullRequestDashboardHelp(pr.Number, result.Checks.Summary, result.ReviewThreads.Summary)
	return result, nil
}

func (workflow PullRequestWorkflow) Checks(input PullRequestInput) (gh.CheckRollup, error) {
	return workflow.ChecksContext(context.Background(), input)
}

func (workflow PullRequestWorkflow) ChecksContext(ctx context.Context, input PullRequestInput) (gh.CheckRollup, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	session, err := workflow.acquire(ctx, input, false)
	if err != nil {
		return gh.CheckRollup{}, err
	}
	pr, err := session.client.GetPullRequestContext(ctx, session.repo, input.Number)
	if err != nil {
		return gh.CheckRollup{}, classifyGitHubError(err)
	}
	result, err := session.client.GetCheckRollupContext(ctx, session.repo, pr.Number, pr.HeadSHA)
	if err != nil {
		return gh.CheckRollup{}, classifyGitHubError(err)
	}
	return result, nil
}

func (workflow PullRequestWorkflow) ReviewThreads(input PullRequestInput) (gh.ReviewThreadResult, error) {
	return workflow.ReviewThreadsContext(context.Background(), input)
}

func (workflow PullRequestWorkflow) ReviewThreadsContext(ctx context.Context, input PullRequestInput) (gh.ReviewThreadResult, error) {
	if input.BodyLimit < 0 {
		return gh.ReviewThreadResult{}, apperror.New(apperror.Usage, "review-threads --body-limit must be non-negative", "")
	}
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	session, err := workflow.acquire(ctx, input, false)
	if err != nil {
		return gh.ReviewThreadResult{}, err
	}
	result, err := session.client.GetReviewThreadsContext(ctx, session.repo, input.Number)
	if err != nil {
		return gh.ReviewThreadResult{}, classifyGitHubError(err)
	}
	return truncateReviewThreadBodies(result, input.BodyLimit, input.Full), nil
}

type pullRequestSession struct {
	repo   string
	client PullRequestGitHub
	config *config.Config
}

func (workflow PullRequestWorkflow) acquire(ctx context.Context, input PullRequestInput, useConfig bool) (pullRequestSession, error) {
	options := repository.Options{
		WorkingDir: input.WorkingDir, Repository: input.Repository, ConfigPath: input.ConfigPath,
		EnvironmentRepo: input.EnvironmentRepo, GitHubAPIURL: input.GitHubAPIURL,
	}
	var session pullRequestSession
	repositoryContext, contextErr := workflow.resolve(ctx, options)
	if contextErr == nil {
		session.repo = repositoryContext.Repository
		if useConfig || input.ConfigPath != "" {
			cfg := repositoryContext.Config
			session.config = &cfg
		}
	} else {
		if input.ConfigPath != "" {
			return pullRequestSession{}, classifyRepositoryError(contextErr)
		}
		var resolveErr *repository.ResolveError
		var localGitErr *repository.LocalGitError
		if errors.As(contextErr, &resolveErr) || errors.As(contextErr, &localGitErr) {
			return pullRequestSession{}, classifyRepositoryError(contextErr)
		}
		target, err := workflow.resolveTarget(ctx, options, "origin")
		if err != nil {
			return pullRequestSession{}, classifyRepositoryError(err)
		}
		session.repo = target.Repository
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return pullRequestSession{}, err
	}
	session.client = client
	return session, nil
}

func buildPullRequestDashboard(repo string, pr queue.PullRequest, cfg config.Config) PullRequestDashboard {
	referencedIssues := pullRequestReferencedIssues(pr, cfg.PRPolicy.RequiredReferenceKeyword)
	return PullRequestDashboard{
		SchemaVersion: 1, Kind: "pullRequest", Repo: repo, PullRequest: pr,
		ReferencedIssues: referencedIssues,
		Checks:           PullRequestCheckSummary{State: firstNonBlank(pr.CheckState, "unknown")},
		ReviewThreads:    PullRequestReviewSummary{},
		Help:             PullRequestDashboardHelp(pr.Number, gh.CheckSummary{}, gh.ThreadSummary{}),
	}
}

func (workflow PullRequestWorkflow) addIssueReadiness(ctx context.Context, result *PullRequestDashboard, session pullRequestSession) {
	if len(result.ReferencedIssues) == 0 {
		return
	}
	if session.config == nil {
		result.Warnings = append(result.Warnings, "issue readiness unavailable: Baton configuration could not be loaded")
		return
	}
	issues, err := session.client.ListOpenIssuesContext(ctx, result.Repo)
	if err != nil {
		result.Warnings = append(result.Warnings, "issue readiness unavailable: GitHub request failed")
		return
	}
	linkedWorkPR, mergedWorkPR := 0, 0
	if policy.ClassifyPullRequestFlow(policy.PullRequest{
		BaseRef: result.PullRequest.BaseRef,
		HeadRef: result.PullRequest.HeadRef,
	}, *session.config) == policy.PRFlowWork {
		switch {
		case result.PullRequest.Merged:
			mergedWorkPR = result.PullRequest.Number
		case result.PullRequest.State == "" || strings.EqualFold(result.PullRequest.State, "open"):
			linkedWorkPR = result.PullRequest.Number
		}
	}
	result.IssueReadiness = buildIssueReadiness(result.ReferencedIssues, queueIssues(issues), *session.config, linkedWorkPR, mergedWorkPR)
}

func buildIssueReadiness(referenced []int, issues []queue.Issue, cfg config.Config, linkedWorkPR, mergedWorkPR int) []PullRequestIssueReadiness {
	issuesByNumber := map[int]queue.Issue{}
	for _, issue := range issues {
		issuesByNumber[issue.Number] = issue
	}
	readiness := make([]PullRequestIssueReadiness, 0, len(referenced))
	for _, number := range referenced {
		issue, ok := issuesByNumber[number]
		if !ok {
			readiness = append(readiness, PullRequestIssueReadiness{Number: number, State: workitem.StatePromotedOrClosed, Reasons: []string{"referenced issue is closed"}})
			continue
		}
		readiness = append(readiness, classifyIssueReadiness(issue, cfg, linkedWorkPR, mergedWorkPR))
	}
	return readiness
}

func classifyIssueReadiness(issue queue.Issue, cfg config.Config, linkedWorkPR, mergedWorkPR int) PullRequestIssueReadiness {
	linked, merged := []int(nil), []int(nil)
	if linkedWorkPR != 0 {
		linked = []int{linkedWorkPR}
	}
	if mergedWorkPR != 0 {
		merged = []int{mergedWorkPR}
	}
	readiness := workitem.ClassifyIssue(workitem.IssueFacts{Open: true, Labels: issue.Labels, LinkedWorkPRs: linked, MergedWorkPRs: merged}, cfg.IssuePolicy)
	return PullRequestIssueReadiness{Number: issue.Number, Found: true, State: readiness.State, Ready: readiness.Eligible, Action: readiness.Action, Labels: issue.Labels, Reasons: readiness.Reasons}
}

func pullRequestReferencedIssues(pr queue.PullRequest, keyword string) []int {
	values := append(policy.ExtractReferenceIssueNumbersForPolicy(pr.Title, keyword), policy.ExtractReferenceIssueNumbersForPolicy(pr.Body, keyword)...)
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func PullRequestLikelyNextCommand(prNumber int, checks gh.CheckSummary, threads gh.ThreadSummary, checksAvailable, reviewAvailable bool) string {
	switch {
	case threads.HumanUnresolved > 0:
		return fmt.Sprintf("baton review-threads %d --format toon", prNumber)
	case checks.Failed > 0 || checks.Pending > 0:
		return fmt.Sprintf("baton checks %d --format toon", prNumber)
	case threads.BotUnresolved > 0 || threads.UnknownUnresolved > 0 || !reviewAvailable:
		return fmt.Sprintf("baton review-threads %d --format toon", prNumber)
	case !checksAvailable:
		return fmt.Sprintf("baton checks %d --format toon", prNumber)
	default:
		return "baton next --format toon"
	}
}

func PullRequestDashboardHelp(prNumber int, checks gh.CheckSummary, threads gh.ThreadSummary) []string {
	help := []string{
		fmt.Sprintf("Run `baton checks %d --format toon` for check details.", prNumber),
		fmt.Sprintf("Run `baton review-threads %d --format toon` for review comments.", prNumber),
	}
	if checks.Failed > 0 {
		help = append(help, "Inspect failed check URLs before editing.")
	}
	if threads.HumanUnresolved > 0 {
		help = append(help, "Stop and ask the user if unresolved human comments require product judgment.")
	}
	return help
}

func truncateReviewThreadBodies(result gh.ReviewThreadResult, limit int, full bool) gh.ReviewThreadResult {
	threads := append([]gh.ReviewThread(nil), result.Threads...)
	result.Threads = threads
	for threadIndex := range result.Threads {
		comments := make([]gh.ReviewComment, len(result.Threads[threadIndex].Comments))
		for commentIndex, comment := range result.Threads[threadIndex].Comments {
			comments[commentIndex] = truncateReviewComment(comment, result.PRNumber, limit, full)
		}
		result.Threads[threadIndex].Comments = comments
	}
	return result
}

func truncateReviewComment(comment gh.ReviewComment, prNumber, limit int, full bool) gh.ReviewComment {
	bodyRunes := []rune(comment.Body)
	comment.BodyChars = len(bodyRunes)
	if full || len(bodyRunes) <= limit {
		comment.BodyTruncated = false
		return comment
	}
	preview := string(bodyRunes[:limit])
	comment.Body, comment.BodyPreview, comment.BodyTruncated = preview, preview, true
	comment.FullCommand = fmt.Sprintf("baton review-threads %d --full --json", prNumber)
	return comment
}
