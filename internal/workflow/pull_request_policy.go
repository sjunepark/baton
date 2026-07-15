package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
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
	WorkflowName    string
	RunID           int64
}

type PullRequestPolicyGitHub interface {
	deliveryStoreReader
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
	FetchCommitListingContext(context.Context, string, int) (gh.CommitListing, error)
	GetCheckRollupContext(context.Context, string, int, string) (gh.CheckRollup, error)
	ListOpenPullRequestsBoundedContext(context.Context, string, string) (gh.PullRequestListing, error)
	ListNewestIssueCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error)
	ListNewestPullRequestCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error)
	ListIssueCommentsAfterContext(context.Context, string, int, time.Time) (gh.IssueCommentListing, error)
	CreateIssueCommentReturningContext(context.Context, string, int, string) (gh.IssueComment, error)
	UpdateIssueCommentReturningContext(context.Context, string, int64, string) (gh.IssueComment, error)
	RerequestCheckRunContext(context.Context, string, int64) error
	CompareCommitsContext(context.Context, string, string, string) (gh.CommitComparison, error)
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
	flow := policy.ClassifyPullRequestFlow(policyInput.PullRequest, cfg)
	if flow != policy.PRFlowWork && flow != policy.PRFlowPromotion {
		return policy.ComputePullRequestPolicy(policyInput), nil
	}
	if strings.TrimSpace(event.HeadSHA) == "" {
		return policy.PRPolicyDecision{}, apperror.New(apperror.Policy, "Baton managed pull request revision is incomplete", "Let GitHub deliver an event with the pull request head revision, then retry.")
	}
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
	latest, err := client.GetPullRequestContext(ctx, repo, event.Number)
	if err != nil {
		return policy.PRPolicyDecision{}, classifyGitHubError(err)
	}
	if !pullRequestRevisionMatchesEvent(event, latest) {
		return policy.PRPolicyDecision{}, apperror.New(apperror.GitHub, "pull request changed before policy acquisition", "Let the newest pull request event evaluate the current managed revision.")
	}
	policyInput.PullRequest = policyPullRequestFromGitHub(latest)
	flow = policy.ClassifyPullRequestFlow(policyInput.PullRequest, cfg)
	if flow != policy.PRFlowWork && flow != policy.PRFlowPromotion {
		return policy.PRPolicyDecision{}, apperror.New(apperror.GitHub, "pull request ownership changed before policy acquisition", "Let the newest pull request event evaluate the current branch and repository identities.")
	}
	workEvidenceIssues := []delivery.ManagedIssueReference{}
	if flow == policy.PRFlowWork {
		issueNumbers := pullRequestReferenceIssueNumbersFromText(latest.Title, latest.Body, cfg.PRPolicy.RequiredReferenceKeyword)
		for _, issueNumber := range issueNumbers {
			issue, err := client.GetIssueContext(ctx, repo, issueNumber)
			if err != nil {
				return policy.PRPolicyDecision{}, classifyGitHubError(err)
			}
			ownership := policy.IssueOwnershipDecision{
				SchemaVersion: 1, Kind: "managedIssueOwnership", Source: policy.IssueOwnershipInvalid,
				Diagnostics: []string{}, Errors: []string{"reference resolves to a pull request, not an issue"},
			}
			if !issue.PullRequest {
				comments, err := client.ListIssueCommentsContext(ctx, repo, issueNumber)
				if err != nil {
					return policy.PRPolicyDecision{}, classifyGitHubError(err)
				}
				ownership = policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
					IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels,
					Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy,
				})
				if ownership.Managed {
					record := ownership.Record
					if record == nil {
						value := policy.NewManagedIssueRecord(issue.NodeID, issue.Number)
						record = &value
					}
					workEvidenceIssues = append(workEvidenceIssues, delivery.ManagedIssueReference{Number: issue.Number, NodeID: issue.NodeID, OwnershipDigest: record.Digest})
				}
			}
			policyInput.ReferencedIssues = append(policyInput.ReferencedIssues, policy.ReferencedIssue{Number: issue.Number, Ownership: ownership})
		}
	}
	listing, err := client.FetchCommitListingContext(ctx, repo, event.Number)
	if err != nil {
		return policy.PRPolicyDecision{}, classifyGitHubError(err)
	}
	confirmed, err := client.GetPullRequestContext(ctx, repo, event.Number)
	if err != nil {
		return policy.PRPolicyDecision{}, classifyGitHubError(err)
	}
	if !sameManagedPolicyPullRequest(latest, confirmed) {
		return policy.PRPolicyDecision{}, apperror.New(apperror.GitHub, "pull request changed during policy acquisition", "Let the newest pull request event evaluate the current head revision.")
	}
	policyInput.PullRequest = policyPullRequestFromGitHub(confirmed)
	if flow == policy.PRFlowPromotion {
		facts, err := sealPromotionPolicy(ctx, client, repo, cfg, event, confirmed, delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID})
		if err != nil {
			return policy.PRPolicyDecision{}, err
		}
		policyInput.PromotionFacts = &facts
	}
	policyInput.CommitMessages = listing.Messages
	policyInput.CommitListingReachedCap = listing.GitHubCapReached
	decision := policy.ComputePullRequestPolicy(policyInput)
	if flow == policy.PRFlowWork && cfg.Delivery != nil && strings.EqualFold(confirmed.State, "open") {
		if err := persistManagedWorkPolicyEvidence(ctx, client, repo, cfg, confirmed, decision, workEvidenceIssues, delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}); err != nil {
			return policy.PRPolicyDecision{}, err
		}
	}
	return decision, nil
}

func persistManagedWorkPolicyEvidence(ctx context.Context, client PullRequestPolicyGitHub, repo string, cfg config.Config, pr gh.PullRequest, decision policy.PRPolicyDecision, issues []delivery.ManagedIssueReference, writer delivery.WriterProvenance) error {
	if writer.Workflow != "PR Policy" || writer.RunID <= 0 {
		return apperror.New(apperror.Usage, "managed-work policy evidence requires the trusted PR Policy workflow", "Use the generated PR Policy workflow so evidence is authored by trusted default-branch code.")
	}
	locator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return apperror.Wrap(apperror.Config, "managed-work policy evidence requires a pinned delivery repository", err, "Review and pin the delivery locator before enabling delivery policy evidence.")
	}
	repository, err := client.GetRepositoryIdentityContext(ctx, repo)
	if err != nil {
		return classifyGitHubError(err)
	}
	if !strings.EqualFold(repository.Host, locator.Repository.Host) || repository.FullName != locator.Repository.FullName || repository.NodeID != locator.Repository.NodeID {
		return apperror.New(apperror.Config, "managed-work policy evidence repository does not match the pinned delivery locator", "Repair the delivery repository identity before merging managed work.")
	}
	allowed := len(decision.Errors) == 0
	if !allowed {
		issues = nil
	}
	evidence, err := policy.NewManagedWorkPolicyEvidence(policy.ManagedWorkPolicyEvidence{
		Repository: locator.Repository, PullRequest: delivery.ResourceIdentity{Number: pr.Number, NodeID: pr.NodeID},
		BaseBranch: pr.BaseRef, WorkBranch: pr.HeadRef, BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA,
		ProseDigest: policy.ManagedWorkProseDigest(pr.Title, pr.Body), PolicySchemaVersion: decision.SchemaVersion,
		Allowed: allowed, Issues: issues, Writer: writer,
	})
	if err != nil {
		return apperror.Wrap(apperror.Policy, "managed-work policy evidence is invalid", err, "Re-run policy against the current pull-request and managed-issue facts.")
	}
	body, err := policy.RenderManagedWorkPolicyEvidence(evidence)
	if err != nil {
		return apperror.Wrap(apperror.Policy, "managed-work policy evidence could not be encoded", err, "")
	}
	listing, err := client.ListNewestPullRequestCommentsContext(ctx, repo, pr.Number)
	if err != nil {
		return classifyGitHubError(err)
	}
	match, err := policy.FindTrustedManagedWorkPolicyEvidence(managedWorkEvidenceComments(listing.Comments))
	if err != nil {
		return apperror.Wrap(apperror.Policy, "managed-work policy evidence is ambiguous", err, "Repair the trusted evidence comments on the pull request, then retry policy.")
	}
	if match == nil || match.Evidence.Digest != evidence.Digest {
		written, createErr := client.CreateIssueCommentReturningContext(ctx, repo, pr.Number, body)
		if createErr != nil {
			retryListing, readErr := client.ListNewestPullRequestCommentsContext(ctx, repo, pr.Number)
			if readErr != nil {
				return classifyGitHubError(createErr)
			}
			retryMatch, matchErr := policy.FindTrustedManagedWorkPolicyEvidence(managedWorkEvidenceComments(retryListing.Comments))
			if matchErr != nil || retryMatch == nil || retryMatch.Evidence.Digest != evidence.Digest {
				return classifyGitHubError(createErr)
			}
			written, err = client.GetIssueCommentContext(ctx, repo, retryMatch.Comment.ID)
			if err != nil {
				return classifyGitHubError(createErr)
			}
		}
		if err := validateTrustedDeliveryWrite(written, repo, pr.Number); err != nil || strings.TrimSpace(written.Body) != body {
			if err == nil {
				err = errors.New("managed-work policy evidence write returned stale content")
			}
			return apperror.Wrap(apperror.GitHub, "managed-work policy evidence write was not trusted", err, "Retry the PR Policy workflow against the current pull-request revision.")
		}
	}
	confirmed, err := client.GetPullRequestContext(ctx, repo, pr.Number)
	if err != nil {
		return classifyGitHubError(err)
	}
	if !strings.EqualFold(confirmed.State, "open") || !sameManagedPolicyPullRequest(pr, confirmed) {
		return apperror.New(apperror.GitHub, "pull request changed after policy evidence was written", "Let the newest pull-request event evaluate and persist evidence for the current open revision.")
	}
	return nil
}

func managedWorkEvidenceComments(comments []gh.IssueComment) []policy.ManagedWorkEvidenceComment {
	result := make([]policy.ManagedWorkEvidenceComment, 0, len(comments))
	for _, comment := range comments {
		result = append(result, policy.ManagedWorkEvidenceComment{
			ID: comment.ID, NodeID: comment.NodeID, IssueURL: comment.IssueURL, Body: comment.Body,
			AuthorLogin: comment.Author.Login, AuthorType: comment.Author.Type, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt,
		})
	}
	return result
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

func policyPullRequestFromGitHub(pr gh.PullRequest) policy.PullRequest {
	return policy.PullRequest{
		Number: pr.Number, Title: pr.Title, Body: pr.Body,
		HeadRef: pr.HeadRef, BaseRef: pr.BaseRef,
		HeadRepositoryFullName: pr.HeadRepositoryFullName,
		BaseRepositoryFullName: pr.BaseRepositoryFullName,
	}
}

func pullRequestRevisionMatchesEvent(event gh.PullRequestEvent, current gh.PullRequest) bool {
	return current.Number == event.Number && current.NodeID == event.NodeID && current.BaseRef == event.BaseRef && current.HeadRef == event.HeadRef &&
		current.BaseSHA == event.BaseSHA && current.HeadSHA == event.HeadSHA && strings.EqualFold(current.BaseRepositoryFullName, event.BaseRepositoryFullName) &&
		strings.EqualFold(current.HeadRepositoryFullName, event.HeadRepositoryFullName)
}

func sameManagedPolicyPullRequest(first, second gh.PullRequest) bool {
	return first.Number == second.Number && first.NodeID == second.NodeID && first.Title == second.Title && first.Body == second.Body &&
		first.BaseRef == second.BaseRef && first.HeadRef == second.HeadRef && first.BaseSHA == second.BaseSHA && first.HeadSHA == second.HeadSHA &&
		strings.EqualFold(first.BaseRepositoryFullName, second.BaseRepositoryFullName) && strings.EqualFold(first.HeadRepositoryFullName, second.HeadRepositoryFullName)
}

func pullRequestReferenceIssueNumbers(event gh.PullRequestEvent, referenceKeyword string) []int {
	return pullRequestReferenceIssueNumbersFromText(event.Title, event.Body, referenceKeyword)
}

func pullRequestReferenceIssueNumbersFromText(title, body, referenceKeyword string) []int {
	values := append(
		policy.ExtractReferenceIssueNumbersForPolicy(title, referenceKeyword),
		policy.ExtractReferenceIssueNumbersForPolicy(body, referenceKeyword)...,
	)
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
