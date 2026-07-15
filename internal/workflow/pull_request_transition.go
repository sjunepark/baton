package workflow

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
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
	WorkflowName    string
	RunID           int64
}

type PullRequestTransitionResult struct {
	workitem.TransitionPlan
	Report *operation.Report `json:"report,omitempty"`
}

type PullRequestTransitionGitHub interface {
	deliveryStoreReader
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
	ListNewestIssueCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error)
	ListIssueCommentsAfterContext(context.Context, string, int, time.Time) (gh.IssueCommentListing, error)
	AddIssueLabelsContext(context.Context, string, int, []string) error
	RemoveIssueLabelContext(context.Context, string, int, string) error
	CloseIssueContext(context.Context, string, int) error
	CreateIssueCommentReturningContext(context.Context, string, int, string) (gh.IssueComment, error)
	UpdateIssueCommentReturningContext(context.Context, string, int64, string) (gh.IssueComment, error)
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
	plannerEvent := workitem.PullRequestEvent{
		Repository: event.BaseRepositoryFullName, Action: event.Action, Number: event.Number, Title: event.Title, Body: event.Body,
		BaseRef: event.BaseRef, HeadRef: event.HeadRef, BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA, MergeRevision: event.MergeRevision,
		BaseRepositoryFullName: event.BaseRepositoryFullName, HeadRepositoryFullName: event.HeadRepositoryFullName,
		State: event.State, Merged: event.Merged,
	}
	result := PullRequestTransitionResult{TransitionPlan: workitem.PlanPullRequestTransition(plannerEvent, cfg)}
	if event.Action != "closed" || !event.Merged || (result.Flow != policy.PRFlowWork && result.Flow != policy.PRFlowPromotion) {
		if !input.Apply {
			return result, nil
		}
		report := operation.NewReport(nil)
		result.Report = &report
		return result, nil
	}
	if cfg.Delivery == nil {
		if result.Flow == policy.PRFlowWork {
			result.Warnings = []string{"delivery recording is disabled until repository policy pins a reviewed ledger locator; work transitions are disabled"}
			if input.Apply {
				report := operation.NewReport(nil)
				result.Report = &report
			}
			return result, nil
		}
		return result, apperror.New(apperror.Config, "merged promotion transition requires a reviewed delivery-ledger locator", "Run delivery bootstrap and pin its exact locator before retrying the transition.")
	}
	if cfg.Delivery.Authority != config.DeliveryAuthoritySealed {
		result.Warnings = []string{"delivery ledger is in shadow mode; transitions are disabled until reviewed sealed cutover"}
		if input.Apply {
			report := operation.NewReport(nil)
			result.Report = &report
		}
		return result, nil
	}
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
		repositoryIdentityInput{source: "pull_request.base.repo.full_name", value: event.BaseRepositoryFullName},
	)
	if err != nil {
		return result, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return result, err
	}
	locator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return result, apperror.Wrap(apperror.Config, err.Error(), err, "Run and review delivery bootstrap before retrying the transition.")
	}
	store, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	if result.Flow == policy.PRFlowPromotion {
		return runPromotionTransition(ctx, result, input, cfg, repo, locator, event, plannerEvent, store, client)
	}
	staged, err := delivery.ResolveStagedWork(store.Snapshot, delivery.StagedWorkRevision{
		PullRequest: delivery.ResourceIdentity{Number: event.Number, NodeID: event.NodeID},
		BaseSHA:     event.BaseSHA, HeadSHA: event.HeadSHA, MergeRevision: event.MergeRevision,
	})
	if err != nil {
		return result, apperror.Wrap(apperror.Policy, "merged work delivery record is incomplete", err, "Let Delivery Recorder commit the exact staged-work record, then retry.")
	}
	plannerEvent.DeliveryRecordDigest = staged.Record.Digest
	for _, reference := range staged.Record.Issues {
		plannerEvent.IssueReferences = append(plannerEvent.IssueReferences, workitem.DeliveryIssueReference{Number: reference.Number, NodeID: reference.NodeID, OwnershipDigest: reference.OwnershipDigest})
	}
	result.TransitionPlan = workitem.PlanPullRequestTransition(plannerEvent, cfg)
	result.Repository = repo
	if !input.Apply {
		return result, nil
	}
	if strings.TrimSpace(input.WorkflowName) != "Delivery Recorder" || input.RunID <= 0 {
		return result, apperror.New(apperror.Usage, "merged work transition requires the trusted Delivery Recorder workflow", "Use the generated workflow so recording and transition share one serialized job.")
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
		if issue.PullRequest || issue.NodeID != planned.IssueNodeID {
			results[index].Status = operation.StatusRefused
			results[index].Error = &operation.Failure{Category: "github", Message: "delivery issue identity is stale or resolves to a pull request"}
			report := operation.NewReport(results)
			result.Report = &report
			refused := apperror.New(apperror.GitHub, "work-item transition delivery identity is stale", "Repair or explicitly rebootstrap the delivery record.")
			return result, apperror.WithReport(refused, report)
		}
		comments, err := client.ListIssueCommentsContext(ctx, repo, planned.IssueNumber)
		if err != nil {
			classified := classifyGitHubError(err)
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "managed-issue ownership preflight failed", classified)
			report := operation.NewReport(results)
			result.Report = &report
			return result, apperror.WithReport(classified, report)
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
			IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels,
			Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy,
		})
		if !ownership.Managed || ownership.Source != policy.IssueOwnershipRecord || ownership.Record == nil || ownership.Record.Digest != planned.OwnershipDigest {
			results[index].Status = operation.StatusRefused
			results[index].Error = &operation.Failure{Category: "policy", Message: "referenced issue is not durably managed by Baton"}
			report := operation.NewReport(results)
			result.Report = &report
			refused := apperror.New(apperror.Policy, "work-item transition references an unmanaged issue", "Repair or backfill managed-issue ownership before retrying.")
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

func runPromotionTransition(
	ctx context.Context,
	result PullRequestTransitionResult,
	input PullRequestTransitionInput,
	cfg config.Config,
	repo string,
	locator delivery.DeliveryStoreLocator,
	event gh.PullRequestEvent,
	plannerEvent workitem.PullRequestEvent,
	store acquiredDeliveryStore,
	client PullRequestTransitionGitHub,
) (PullRequestTransitionResult, error) {
	if event.MergedAt.IsZero() {
		return result, apperror.New(apperror.Usage, "merged promotion event has no merge time", "Retry with the complete pull_request.closed webhook payload.")
	}
	if input.Apply && (strings.TrimSpace(input.WorkflowName) != "Work Item Transition" || input.RunID <= 0) {
		return result, apperror.New(apperror.Usage, "promotion transition requires the trusted Work Item Transition workflow", "Use the generated workflow so transition shares the delivery concurrency group.")
	}
	writer := delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}
	if !input.Apply && writer.RunID <= 0 {
		writer = delivery.WriterProvenance{Workflow: "Work Item Transition", RunID: 1}
	}
	consumptionInput := delivery.PromotionConsumptionInput{
		Revision: delivery.PromotionRevision{
			RepositoryNodeID: locator.Repository.NodeID,
			PullRequest:      delivery.ResourceIdentity{Number: event.Number, NodeID: event.NodeID},
			BaseSHA:          event.BaseSHA,
			HeadSHA:          event.HeadSHA,
		},
		MergeRevision: event.MergeRevision,
		Method:        delivery.PromotionUnknown,
		Writer:        writer,
		RecordedAt:    event.MergedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	consumption, err := delivery.PlanPromotionConsumption(store.Snapshot, consumptionInput)
	if err != nil {
		return result, apperror.Wrap(apperror.Policy, "merged promotion delivery plan is incomplete", err, "Repair the exact sealed plan or stale delivery checkpoint, then retry this event.")
	}
	plannerEvent.Promotion = promotionTransitionFacts(consumption)
	result.TransitionPlan = workitem.PlanPullRequestTransition(plannerEvent, cfg)
	result.Repository = repo
	if !input.Apply {
		return result, nil
	}
	if !consumption.Applicable {
		report := operation.NewReport(nil)
		result.Report = &report
		return result, nil
	}
	results := transitionOperationResults(repo, result.Operations)
	fail := func(index int, message string, cause error) (PullRequestTransitionResult, error) {
		classified := classifyGitHubError(cause)
		if index >= 0 && index < len(results) {
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", message, classified)
		}
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classified, report)
	}
	refuse := func(index int, message, remediation string) (PullRequestTransitionResult, error) {
		if index >= 0 && index < len(results) {
			results[index].Status = operation.StatusRefused
			results[index].Error = &operation.Failure{Category: "stale", Message: message}
		}
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(apperror.New(apperror.Policy, message, remediation), report)
	}

	latest, err := client.GetPullRequestContext(ctx, repo, event.Number)
	if err != nil {
		return fail(0, "promotion pull request preflight failed", err)
	}
	if !pullRequestEventStillCurrent(event, latest) {
		return refuse(0, "promotion pull request changed after the event was captured", "Retry only the newest exact merged-promotion event.")
	}
	for _, reference := range consumption.ExpectedIssues {
		operationIndex := transitionIssueOperationIndex(result.Operations, reference.Number)
		issue, err := client.GetIssueContext(ctx, repo, reference.Number)
		if err != nil {
			return fail(operationIndex, "promotion issue preflight failed", err)
		}
		if issue.PullRequest || issue.NodeID != reference.NodeID {
			return refuse(operationIndex, "promotion delivery issue identity is stale", "Repair or explicitly rebootstrap the delivery record.")
		}
		comments, err := client.ListIssueCommentsContext(ctx, repo, reference.Number)
		if err != nil {
			return fail(operationIndex, "promotion issue ownership preflight failed", err)
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
			IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels,
			Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy,
		})
		if !ownership.Managed || ownership.Source != policy.IssueOwnershipRecord || ownership.Record == nil || ownership.Record.Digest != reference.OwnershipDigest {
			return refuse(operationIndex, "promotion delivery issue is not durably managed by Baton", "Repair or backfill managed-issue ownership before retrying.")
		}
		for index := range result.Operations {
			planned := result.Operations[index]
			if planned.IssueNumber != reference.Number {
				continue
			}
			switch planned.Action {
			case workitem.TransitionActionCloseIssue:
				if strings.EqualFold(issue.State, "closed") {
					results[index].Status = operation.StatusUnchanged
				}
			case workitem.TransitionActionRemoveLabel:
				if !containsLabel(issue.Labels, planned.Label) {
					results[index].Status = operation.StatusUnchanged
				}
			}
		}
	}

	// Reacquire the bounded store after every external preflight and before the
	// first write. This makes the seal, plan digest, and cursor one atomic stale
	// precondition from the command's perspective.
	fresh, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return fail(transitionRecordOperationIndex(result.Operations), "promotion delivery checkpoint preflight failed", err)
	}
	replanned, err := delivery.PlanPromotionConsumption(fresh.Snapshot, consumptionInput)
	if err != nil {
		return fail(transitionRecordOperationIndex(result.Operations), "promotion delivery checkpoint became stale", err)
	}
	if !replanned.Applicable {
		plannerEvent.Promotion = promotionTransitionFacts(replanned)
		result.TransitionPlan = workitem.PlanPullRequestTransition(plannerEvent, cfg)
		result.Repository = repo
		report := operation.NewReport(nil)
		result.Report = &report
		return result, nil
	}
	if replanned.Record == nil || consumption.Record == nil || replanned.Record.Digest != consumption.Record.Digest || replanned.Precondition != consumption.Precondition {
		return refuse(transitionRecordOperationIndex(result.Operations), "promotion delivery plan changed during preflight", "Retry the exact merged-promotion event against the current delivery checkpoint.")
	}
	consumption = replanned
	store = fresh

	for index, planned := range result.Operations {
		if planned.Action != workitem.TransitionActionCloseIssue && planned.Action != workitem.TransitionActionRemoveLabel {
			continue
		}
		if results[index].Status == operation.StatusUnchanged {
			continue
		}
		var applyErr error
		switch planned.Action {
		case workitem.TransitionActionCloseIssue:
			applyErr = client.CloseIssueContext(ctx, repo, planned.IssueNumber)
		case workitem.TransitionActionRemoveLabel:
			applyErr = client.RemoveIssueLabelContext(ctx, repo, planned.IssueNumber, planned.Label)
		}
		if applyErr != nil {
			return fail(index, "promotion issue transition failed", applyErr)
		}
		results[index].Status = operation.StatusApplied
	}

	recordIndex := transitionRecordOperationIndex(result.Operations)
	cursorIndex := transitionCursorOperationIndex(result.Operations)
	if consumption.Record == nil {
		return fail(recordIndex, "promotion base-integration plan is incomplete", fmt.Errorf("missing base-integration record"))
	}
	body, err := delivery.RenderBaseIntegrationRecord(*consumption.Record)
	if err != nil {
		return fail(recordIndex, "promotion base-integration record could not be encoded", err)
	}
	comment, err := findTrustedBaseIntegrationRetry(ctx, client, repo, store.LedgerIssue.Number, store.CheckpointComment.UpdatedAt, consumption.Record.RetryID)
	if err != nil {
		return fail(recordIndex, "promotion base-integration retry lookup failed", err)
	}
	recordStatus := operation.StatusUnchanged
	if comment == nil {
		created, createErr := client.CreateIssueCommentReturningContext(ctx, repo, store.LedgerIssue.Number, body)
		if createErr != nil {
			comment, err = findTrustedBaseIntegrationRetry(ctx, client, repo, store.LedgerIssue.Number, store.CheckpointComment.UpdatedAt, consumption.Record.RetryID)
			if err != nil {
				return fail(recordIndex, "promotion base-integration append was ambiguous", err)
			}
			if comment == nil {
				return fail(recordIndex, "promotion base-integration append failed", createErr)
			}
		} else {
			comment = &created
		}
		recordStatus = operation.StatusApplied
	}
	if err := validateTrustedDeliveryWrite(*comment, repo, store.LedgerIssue.Number); err != nil {
		return fail(recordIndex, "promotion base-integration append was not trusted", err)
	}
	parsed, err := delivery.ParseBaseIntegrationComment(storedDeliveryComment(*comment))
	if err != nil {
		return fail(recordIndex, "promotion base-integration append was not exact", err)
	}
	results[recordIndex].Status = recordStatus
	checkpointNow, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
	if err != nil {
		return fail(cursorIndex, "promotion cursor checkpoint preflight failed", err)
	}
	current, err := delivery.ParseCheckpointIndex(consumption.Locator, storedDeliveryComment(checkpointNow))
	if err != nil {
		return fail(cursorIndex, "promotion cursor checkpoint preflight failed", err)
	}
	if parsed.Digest != consumption.Record.Digest {
		consumption, err = delivery.AdoptBaseIntegrationRetry(consumption, current, parsed)
		if err != nil {
			return fail(recordIndex, "promotion base-integration append was not exact", err)
		}
	}
	result.BaseIntegrationDigest = parsed.Digest
	result.Operations[recordIndex].RecordDigest = parsed.Digest
	results[recordIndex].Resource = string(parsed.Kind) + ":" + parsed.Digest
	commit, err := delivery.FinalizePromotionConsumption(consumption, current, delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID})
	if err != nil {
		return fail(cursorIndex, "promotion cursor finalization failed", err)
	}
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, store.CheckpointComment.ID, commit.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
		if readErr != nil || readback.Body != commit.CheckpointBody {
			return fail(cursorIndex, "promotion cursor checkpoint update failed", err)
		}
		updated = readback
	}
	if trustErr := validateTrustedDeliveryWrite(updated, repo, store.LedgerIssue.Number); trustErr != nil || updated.ID != store.CheckpointComment.ID || updated.NodeID != store.CheckpointComment.NodeID || updated.Body != commit.CheckpointBody {
		if trustErr == nil {
			trustErr = fmt.Errorf("promotion checkpoint update returned stale content or identity")
		}
		return fail(cursorIndex, "promotion cursor checkpoint update was not trusted", trustErr)
	}
	results[cursorIndex].Status = operation.StatusApplied
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func promotionTransitionFacts(plan delivery.PromotionConsumptionPlan) *workitem.PromotionTransition {
	facts := &workitem.PromotionTransition{Committed: !plan.Applicable, PlanDigest: plan.PlanDigest}
	if plan.Existing != nil {
		facts.CursorDigest = plan.Existing.Record.CommittedCursorDigest
		facts.BaseIntegrationDigest = plan.Existing.Record.Digest
		return facts
	}
	if plan.Record != nil && plan.Checkpoint != nil {
		facts.CursorDigest = plan.Checkpoint.Checkpoint.Cursor.Digest
		facts.BaseIntegrationDigest = plan.Record.Digest
	}
	for _, reference := range plan.ExpectedIssues {
		facts.IssueReferences = append(facts.IssueReferences, workitem.DeliveryIssueReference{
			Number: reference.Number, NodeID: reference.NodeID, OwnershipDigest: reference.OwnershipDigest,
		})
	}
	return facts
}

type baseIntegrationRetryReader interface {
	ListIssueCommentsAfterContext(context.Context, string, int, time.Time) (gh.IssueCommentListing, error)
	GetIssueCommentContext(context.Context, string, int64) (gh.IssueComment, error)
}

func findTrustedBaseIntegrationRetry(ctx context.Context, client baseIntegrationRetryReader, repo string, issueNumber int, after time.Time, retryID string) (*gh.IssueComment, error) {
	listing, err := client.ListIssueCommentsAfterContext(ctx, repo, issueNumber, after)
	if err != nil {
		return nil, err
	}
	if !listing.Complete {
		return nil, fmt.Errorf("base-integration retry lookup did not reach the checkpoint boundary")
	}
	stored := make([]delivery.StoredComment, 0, len(listing.Comments))
	for _, comment := range listing.Comments {
		stored = append(stored, storedDeliveryComment(comment))
	}
	match, err := delivery.FindBaseIntegrationRetry(stored, retryID)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}
	comment, err := client.GetIssueCommentContext(ctx, repo, match.Comment.DatabaseID)
	if err != nil {
		return nil, err
	}
	if err := validateTrustedDeliveryWrite(comment, repo, issueNumber); err != nil {
		return nil, err
	}
	record, err := delivery.ParseBaseIntegrationComment(storedDeliveryComment(comment))
	if err != nil {
		return nil, err
	}
	if comment.NodeID != match.Comment.NodeID || record.Digest != match.Record.Digest || record.RetryID != retryID {
		return nil, fmt.Errorf("base-integration retry readback changed identity or content")
	}
	return &comment, nil
}

func transitionIssueOperationIndex(operations []workitem.TransitionOperation, issueNumber int) int {
	for index, operation := range operations {
		if operation.IssueNumber == issueNumber {
			return index
		}
	}
	return 0
}

func transitionRecordOperationIndex(operations []workitem.TransitionOperation) int {
	for index, operation := range operations {
		if operation.Action == workitem.TransitionActionAppendBaseIntegration {
			return index
		}
	}
	return 0
}

func transitionCursorOperationIndex(operations []workitem.TransitionOperation) int {
	for index, operation := range operations {
		if operation.Action == workitem.TransitionActionCommitPromotionCursor {
			return index
		}
	}
	return 0
}

func transitionOperationResults(repo string, planned []workitem.TransitionOperation) []operation.Result {
	results := make([]operation.Result, len(planned))
	for index, item := range planned {
		resource := repo
		switch item.Action {
		case workitem.TransitionActionAddLabels, workitem.TransitionActionRemoveLabel:
			resource = fmt.Sprintf("%s#%d:%s", repo, item.IssueNumber, item.Label)
		case workitem.TransitionActionCloseIssue:
			resource = fmt.Sprintf("%s#%d", repo, item.IssueNumber)
		case workitem.TransitionActionAppendBaseIntegration:
			resource = item.RecordKind + ":" + item.RecordDigest
		case workitem.TransitionActionCommitPromotionCursor:
			resource = "promotionCursor:" + item.CursorDigest
		}
		results[index] = operation.Result{ID: item.ID, Resource: resource, Action: item.Action, Status: operation.StatusNotAttempted}
	}
	return results
}

func transitionPreflightFailure(results []operation.Result, repo string, number int, action string, status operation.Status, failure *operation.Failure) operation.Report {
	preflight := operation.Result{ID: "pull-request-state-preflight", Resource: fmt.Sprintf("%s#%d", repo, number), Action: action, Status: status, Error: failure}
	return operation.NewReport(append([]operation.Result{preflight}, results...))
}

func pullRequestEventStillCurrent(event gh.PullRequestEvent, latest gh.PullRequest) bool {
	return latest.Number == event.Number && latest.NodeID == event.NodeID && latest.BaseRef == event.BaseRef && latest.HeadRef == event.HeadRef && latest.BaseSHA == event.BaseSHA &&
		latest.BaseRepositoryFullName == event.BaseRepositoryFullName && latest.HeadRepositoryFullName == event.HeadRepositoryFullName &&
		latest.HeadSHA == event.HeadSHA && latest.MergeRevision == event.MergeRevision && latest.MergedAt.Equal(event.MergedAt) && strings.EqualFold(latest.State, event.State) && latest.Merged == event.Merged
}

func containsLabel(labels []string, wanted string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}
