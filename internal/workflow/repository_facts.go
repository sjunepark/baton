package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/snapshot"
)

func (workflow ObservationWorkflow) AcquireRecommendationFactsContext(ctx context.Context, input RepositoryInput) (snapshot.Acquisition, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	facts, _, err := workflow.acquireRecommendationFacts(ctx, input)
	return facts, err
}

func (workflow ObservationWorkflow) acquireRecommendationFacts(ctx context.Context, input RepositoryInput) (snapshot.Acquisition, config.Config, error) {
	repositoryContext, err := workflow.resolve(ctx, repository.Options{
		WorkingDir: input.WorkingDir, Repository: input.Repository, ConfigPath: input.ConfigPath,
		EnvironmentRepo: input.EnvironmentRepo, GitHubAPIURL: input.GitHubAPIURL,
	})
	if err != nil {
		return snapshot.Acquisition{}, config.Config{}, classifyRepositoryError(err)
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		if apperror.As(err) != nil {
			return snapshot.Acquisition{}, config.Config{}, err
		}
		return snapshot.Acquisition{}, config.Config{}, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
	}

	facts := snapshot.Acquisition{Repository: repositoryContext.Repository, Completeness: snapshot.Complete}
	issues, err := client.ListOpenIssuesContext(ctx, repositoryContext.Repository)
	if err != nil {
		facts.AddWarning(acquisitionWarning("issues", err))
	} else {
		managedIssues, warnings := acquireManagedIssueFacts(ctx, client, repositoryContext.Repository, issues, repositoryContext.Config)
		facts.Issues = managedIssues
		for _, warning := range warnings {
			facts.AddWarning(warning)
		}
	}

	prs := []gh.PullRequest{}
	var acquiredDelivery *delivery.Snapshot
	openPullRequests, err := client.ListOpenPullRequestsContext(ctx, repositoryContext.Repository, "")
	if err != nil {
		facts.AddWarning(acquisitionWarning("pullRequests", err))
	} else {
		prs = append(prs, openPullRequests...)
	}
	if repositoryContext.Config.Delivery == nil {
		if repositoryContext.Config.Repository.BaseBranch != repositoryContext.Config.Repository.StagingBranch {
			facts.AddWarning(snapshot.Warning{Code: "invalid", Scope: "deliveryLedger", Message: "repository policy has no reviewed delivery-ledger locator"})
		}
	} else if repositoryContext.Config.Delivery.Authority != config.DeliveryAuthoritySealed {
		facts.AddWarning(snapshot.Warning{Code: "invalid", Scope: "deliveryLedger", Message: "delivery ledger is in shadow mode; recommendations require reviewed sealed authority"})
	} else if locator, locatorErr := deliveryLocatorFromConfig(repositoryContext.Config); locatorErr != nil {
		facts.AddWarning(snapshot.Warning{Code: "invalid", Scope: "deliveryLedger", Message: locatorErr.Error()})
	} else if store, storeErr := acquireDeliveryStore(ctx, client, repositoryContext.Repository, locator); storeErr != nil {
		facts.AddWarning(acquisitionWarning("deliveryLedger", storeErr))
	} else {
		facts.MergedWorkPullRequests = deliveryQueuePullRequests(store.Snapshot)
		value := store.Snapshot
		acquiredDelivery = &value
	}
	var ownershipWarnings []snapshot.Warning
	prs, ownershipWarnings = managedPullRequests(prs, repositoryContext.Config)
	for _, warning := range ownershipWarnings {
		facts.AddWarning(warning)
	}

	branchNames := map[string]struct{}{
		repositoryContext.Config.Repository.BaseBranch:    {},
		repositoryContext.Config.Repository.StagingBranch: {},
	}
	branches, rules, err := workflow.acquireBranchFacts(ctx, client, repositoryContext.Repository, repositoryContext.Config.Repository.StagingBranch, branchNames)
	if err != nil {
		return snapshot.Acquisition{}, config.Config{}, err
	}
	facts.Branches = branches
	for _, branch := range branches {
		for _, warning := range branch.Warnings {
			facts.AddWarning(warning)
		}
	}
	if acquiredDelivery != nil {
		baseSHA, stagingSHA := "", ""
		for _, branch := range branches {
			switch branch.Branch.Ref {
			case repositoryContext.Config.Repository.BaseBranch:
				baseSHA = branch.Branch.SHA
			case repositoryContext.Config.Repository.StagingBranch:
				stagingSHA = branch.Branch.SHA
			}
		}
		integration, integrationErr := acquireBaseIntegrationFacts(ctx, client, repositoryContext.Repository, *acquiredDelivery, baseSHA, stagingSHA)
		if integrationErr != nil {
			facts.AddWarning(acquisitionWarning("baseIntegration", integrationErr))
		} else {
			facts.BaseIntegration = &integration
			if integration.State == delivery.BaseIntegrationUnknown || integration.State == delivery.BaseIntegrationDiverged {
				facts.AddWarning(snapshot.Warning{Code: "invalid", Scope: "baseIntegration", Message: integration.Reason})
			}
		}
	}

	facts.PullRequests = workflow.acquirePullRequestFacts(ctx, client, repositoryContext.Repository, prs, rules)
	for _, pr := range facts.PullRequests {
		for _, warning := range pr.Warnings {
			facts.AddWarning(warning)
		}
	}
	return facts, repositoryContext.Config, nil
}

func (workflow ObservationWorkflow) acquireBranchFacts(ctx context.Context, client ObservationGitHub, repo, stagingBranch string, names map[string]struct{}) ([]snapshot.BranchFacts, map[string]gh.BranchRules, error) {
	ordered := make([]string, 0, len(names))
	for name := range names {
		if strings.TrimSpace(name) != "" && name != stagingBranch {
			ordered = append(ordered, name)
		}
	}
	sort.Strings(ordered)
	if stagingBranch != "" {
		ordered = append([]string{stagingBranch}, ordered...)
	}
	result := make([]snapshot.BranchFacts, 0, len(ordered))
	rulesByBranch := make(map[string]gh.BranchRules, len(ordered))
	for _, name := range ordered {
		facts := snapshot.BranchFacts{Completeness: snapshot.Complete}
		branch, err := client.GetBranchContext(ctx, repo, name)
		if err != nil {
			if name == stagingBranch && gh.IsNotFound(err) {
				return nil, nil, apperror.New(
					apperror.Config,
					"staging branch \""+stagingBranch+"\" was not found in "+repo,
					"Run `baton ensure-branch --json`, then `baton ensure-branch --apply` after reviewing the plan.",
				)
			}
			facts.AddWarning(acquisitionWarning("branch:"+name, err))
		} else {
			facts.Branch = branch
			if branch.SHA == "" {
				facts.AddWarning(snapshot.Warning{Code: "unknown", Scope: "branch:" + name + ":revision", Message: "branch revision is unavailable"})
			}
			checks, checkErr := client.GetCheckRollupContext(ctx, repo, 0, branch.SHA)
			if checkErr != nil {
				facts.AddWarning(acquisitionWarning("branch:"+name+":checks", checkErr))
			} else {
				facts.Checks = checks
				if !checks.Complete {
					facts.AddWarning(snapshot.Warning{Code: "truncated", Scope: "branch:" + name + ":checks", Message: strings.Join(checks.Warnings, "; ")})
				}
			}
		}
		if err != nil {
			result = append(result, facts)
			continue
		}
		classicRules := gh.BranchRules{Branch: name}
		if branch.Protected {
			classicRules, err = client.GetClassicBranchRulesContext(ctx, repo, name)
			if err != nil && !gh.IsNotFound(err) {
				facts.AddWarning(acquisitionWarning("branch:"+name+":classic-rules", err))
			}
		}
		rules, err := client.GetEffectiveBranchRulesContext(ctx, repo, name)
		if err != nil {
			facts.AddWarning(acquisitionWarning("branch:"+name+":rules", err))
		} else {
			rules = mergeBranchRules(rules, classicRules)
			rules.RequiredChecks = snapshot.MergeRequiredChecks(rules.RequiredChecks, facts.Branch.LegacyRequiredChecks)
			facts.Rules = rules
			rulesByBranch[name] = rules
		}
		result = append(result, facts)
	}
	return result, rulesByBranch, nil
}

func mergeBranchRules(ruleset, classic gh.BranchRules) gh.BranchRules {
	result := ruleset
	result.RequiredChecks = snapshot.MergeRequiredChecks(ruleset.RequiredChecks, classic.RequiredChecks)
	result.StrictRequiredChecks = ruleset.StrictRequiredChecks || classic.StrictRequiredChecks
	result.DismissStaleReviews = ruleset.DismissStaleReviews || classic.DismissStaleReviews
	result.RequireLastPushApproval = ruleset.RequireLastPushApproval || classic.RequireLastPushApproval
	result.RequiredLinearHistory = ruleset.RequiredLinearHistory || classic.RequiredLinearHistory
	if classic.RequiredApprovingReviewCount > result.RequiredApprovingReviewCount {
		result.RequiredApprovingReviewCount = classic.RequiredApprovingReviewCount
	}
	return result
}

func (workflow ObservationWorkflow) acquirePullRequestFacts(ctx context.Context, client ObservationGitHub, repo string, prs []gh.PullRequest, rules map[string]gh.BranchRules) []snapshot.PullRequestFacts {
	result := make([]snapshot.PullRequestFacts, len(prs))
	if len(prs) == 0 {
		return result
	}
	limit := workflow.enrichmentLimit
	if limit <= 0 {
		limit = 4
	}
	if limit > len(prs) {
		limit = len(prs)
	}
	jobs := make(chan int, len(prs))
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
				result[index] = acquireOnePullRequest(ctx, client, repo, prs[index], rules[prs[index].BaseRef])
			}
		}()
	}
	wait.Wait()
	return result
}

func acquireOnePullRequest(ctx context.Context, client ObservationGitHub, repo string, listed gh.PullRequest, rules gh.BranchRules) snapshot.PullRequestFacts {
	scope := fmt.Sprintf("pullRequest:%d", listed.Number)
	facts := snapshot.PullRequestFacts{PullRequest: listed, Rules: rules, Completeness: snapshot.Complete}
	if err := ctx.Err(); err != nil {
		facts.AddWarning(acquisitionWarning(scope, err))
		return facts
	}
	current, err := client.GetPullRequestContext(ctx, repo, listed.Number)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":detail", err))
		return facts
	}
	facts.PullRequest = current
	if current.HeadSHA == "" || current.BaseSHA == "" || current.HeadRef == "" || current.BaseRef == "" {
		facts.AddWarning(snapshot.Warning{Code: "unknown", Scope: scope + ":revision", Message: "pull request revision identity is incomplete"})
	}
	if (listed.HeadSHA != "" && listed.HeadSHA != current.HeadSHA) || listed.BaseRef != current.BaseRef || (listed.BaseSHA != "" && listed.BaseSHA != current.BaseSHA) {
		facts.AddWarning(snapshot.Warning{Code: "stale", Scope: scope, Message: "pull request revision or target changed during acquisition"})
	}
	if current.Mergeable == "unknown" || current.MergeState == "" || current.MergeState == "unknown" {
		facts.AddWarning(snapshot.Warning{Code: "unknown", Scope: scope + ":mergeability", Message: "GitHub mergeability is still being computed"})
	}

	checks, err := client.GetCheckRollupContext(ctx, repo, current.Number, current.HeadSHA)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":checks", err))
	} else {
		facts.Checks = checks
		facts.RequiredChecks = snapshot.RequiredCheckStates(checks.Checks, rules.RequiredChecks)
		if !checks.Complete {
			facts.AddWarning(snapshot.Warning{Code: "truncated", Scope: scope + ":checks", Message: strings.Join(checks.Warnings, "; ")})
		}
	}
	threads, err := client.GetReviewThreadsContext(ctx, repo, current.Number)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":reviewThreads", err))
	} else {
		facts.ReviewThreads = threads
		if !threads.Complete {
			facts.AddWarning(snapshot.Warning{Code: "truncated", Scope: scope + ":reviewThreads", Message: strings.Join(threads.Warnings, "; ")})
		}
	}
	reviews, err := client.ListPullRequestReviewsContext(ctx, repo, current.Number)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":reviews", err))
	} else {
		facts.Reviews = reviews
	}
	requests, err := client.GetRequestedReviewersContext(ctx, repo, current.Number)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":reviewRequests", err))
	} else {
		facts.ReviewRequests = requests
	}
	facts.Review = snapshot.EvaluateReviews(current.HeadSHA, rules, facts.Reviews, facts.ReviewRequests)
	if rules.RequireLastPushApproval {
		facts.AddWarning(snapshot.Warning{Code: "unknown", Scope: scope + ":reviews", Message: "last-push approval identity is not available"})
	}

	verified, err := client.GetPullRequestContext(ctx, repo, current.Number)
	if err != nil {
		facts.AddWarning(acquisitionWarning(scope+":revisionVerification", err))
	} else if verified.HeadSHA != current.HeadSHA || verified.BaseSHA != current.BaseSHA {
		facts.AddWarning(snapshot.Warning{Code: "stale", Scope: scope, Message: "pull request revision changed during acquisition"})
	}
	return facts
}

func acquisitionWarning(scope string, err error) snapshot.Warning {
	warning := snapshot.Warning{Code: "unavailable", Scope: scope, Message: "GitHub fact acquisition failed"}
	if errors.Is(err, context.Canceled) {
		warning.Code, warning.Message = "canceled", "fact acquisition was canceled"
		return warning
	}
	if errors.Is(err, context.DeadlineExceeded) {
		warning.Code, warning.Message, warning.Retryable = "timeout", "fact acquisition exceeded its deadline", true
		return warning
	}
	applicationError := apperror.WrapUpstream(err)
	warning.Message = applicationError.Message
	warning.Retryable = applicationError.Retryable
	warning.HTTPStatus = applicationError.HTTPStatus
	warning.RequestID = applicationError.RequestID
	if applicationError.Details["rateLimited"] == "true" {
		warning.Code = "rate_limited"
	}
	return warning
}

func incompleteWarningsError(warnings []snapshot.Warning) error {
	codes := make([]string, 0, len(warnings))
	scopes := make([]string, 0, len(warnings))
	retryable := false
	httpStatus := 0
	requestID := ""
	for _, warning := range warnings {
		codes = append(codes, warning.Code)
		scopes = append(scopes, warning.Scope)
		retryable = retryable || warning.Retryable
		if httpStatus == 0 {
			httpStatus = warning.HTTPStatus
		}
		if requestID == "" {
			requestID = warning.RequestID
		}
	}
	return &apperror.Error{
		Category: apperror.GitHub, Message: "GitHub fact acquisition is incomplete",
		Hint: "Retry the command after GitHub facts are available and stable.", Retryable: retryable, HTTPStatus: httpStatus, RequestID: requestID,
		Details: map[string]string{
			"completeness":  "degraded",
			"warningCodes":  strings.Join(codes, ","),
			"warningScopes": strings.Join(scopes, ","),
		},
	}
}

func queueSnapshotFromRecommendationFacts(facts snapshot.Acquisition, cfg config.Config) queue.Snapshot {
	prs := make([]gh.PullRequest, 0, len(facts.PullRequests))
	for _, prFacts := range facts.PullRequests {
		pr := prFacts.PullRequest
		pr.CheckState = prFacts.Checks.State
		prs = append(prs, pr)
	}
	var branchHealth *queue.BranchHealth
	for _, branch := range facts.Branches {
		if branch.Branch.Ref == cfg.Repository.StagingBranch {
			branchHealth = &queue.BranchHealth{Ref: branch.Branch.Ref, SHA: branch.Branch.SHA, CheckState: branch.Checks.State}
			break
		}
	}
	result := queue.BuildSnapshotWithLifecycle(facts.Repository, cfg, queueIssues(facts.Issues), queuePullRequests(prs, cfg), facts.MergedWorkPullRequests, branchHealth)
	result.BaseIntegration = facts.BaseIntegration
	return result
}

func deliveryQueuePullRequests(store delivery.Snapshot) []queue.PullRequest {
	result := make([]queue.PullRequest, 0, len(store.StagedWork))
	for _, record := range store.StagedWork {
		reference, active := deliveryActiveRecordReference(store.Checkpoint, record.Digest)
		if !active || reference.Kind != delivery.RecordStagedWork || reference.Sequence > store.Checkpoint.Coverage.RecordSequence {
			continue
		}
		issues := make([]int, 0, len(record.Issues))
		for _, issue := range record.Issues {
			issues = append(issues, issue.Number)
		}
		result = append(result, queue.PullRequest{
			Number: record.PullRequest.Number, BaseRef: record.StagingBranch, HeadSHA: record.HeadSHA,
			State: "closed", Merged: true, Ownership: policy.PRFlowWork, ReferencedIssues: issues,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result
}

func deliveryActiveRecordReference(checkpoint delivery.DeliveryCheckpoint, digest string) (delivery.RecordReference, bool) {
	for _, reference := range checkpoint.ActiveRecords {
		if reference.Digest == digest {
			return reference, true
		}
	}
	return delivery.RecordReference{}, false
}

type issueOwnershipGitHub interface {
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
}

func acquireManagedIssueFacts(ctx context.Context, client issueOwnershipGitHub, repo string, issues []gh.Issue, cfg config.Config) ([]snapshot.IssueFacts, []snapshot.Warning) {
	result := make([]snapshot.IssueFacts, 0, len(issues))
	warnings := []snapshot.Warning{}
	for _, issue := range issues {
		comments, err := client.ListIssueCommentsContext(ctx, repo, issue.Number)
		if err != nil {
			warnings = append(warnings, acquisitionWarning(fmt.Sprintf("issue:%d:ownership", issue.Number), err))
			continue
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
			IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels,
			Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy,
		})
		switch {
		case ownership.Managed:
			result = append(result, snapshot.IssueFacts{Issue: issue, Ownership: ownership})
		case ownership.Source == policy.IssueOwnershipInvalid:
			warnings = append(warnings, snapshot.Warning{Code: "invalid", Scope: fmt.Sprintf("issue:%d:ownership", issue.Number), Message: strings.Join(ownership.Errors, "; ")})
		}
	}
	return result, warnings
}

func managedPullRequests(prs []gh.PullRequest, cfg config.Config) ([]gh.PullRequest, []snapshot.Warning) {
	result := make([]gh.PullRequest, 0, len(prs))
	warnings := []snapshot.Warning{}
	for _, pr := range prs {
		switch classifyTransportPullRequest(pr, cfg) {
		case policy.PRFlowWork, policy.PRFlowPromotion:
			result = append(result, pr)
		case policy.PRFlowMisroutedWork:
			warnings = append(warnings, snapshot.Warning{Code: "invalid", Scope: fmt.Sprintf("pullRequest:%d:ownership", pr.Number), Message: "managed work branch targets a non-staging branch"})
		case policy.PRFlowIndeterminate:
			warnings = append(warnings, snapshot.Warning{Code: "unknown", Scope: fmt.Sprintf("pullRequest:%d:ownership", pr.Number), Message: "pull request ownership is indeterminate"})
		}
	}
	return result, warnings
}

func classifyTransportPullRequest(pr gh.PullRequest, cfg config.Config) policy.PRFlow {
	return policy.ClassifyPullRequestFlow(policy.PullRequest{
		BaseRef: pr.BaseRef, HeadRef: pr.HeadRef,
		BaseRepositoryFullName: pr.BaseRepositoryFullName, HeadRepositoryFullName: pr.HeadRepositoryFullName,
	}, cfg)
}
