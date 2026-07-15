package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

const deliveryPolicyCheckName = "Check PR policy"

type DeliveryRecordInput struct {
	EventPath, ConfigPath, Repository, EnvironmentRepo string
	Apply                                              bool
	GitHubAPIURL, GitHubToken, GHToken                 string
	WorkflowName                                       string
	RunID                                              int64
}

type DeliveryOwnershipBackfill struct {
	Issue  delivery.ResourceIdentity `json:"issue"`
	Digest string                    `json:"digest"`
	Body   string                    `json:"body"`
}

type DeliveryPromotionRecheck struct {
	PullRequest        delivery.ResourceIdentity `json:"pullRequest"`
	HeadSHA            string                    `json:"headSha"`
	CheckRunID         int64                     `json:"checkRunId,omitempty"`
	ObservedStatus     string                    `json:"observedStatus,omitempty"`
	ObservedConclusion string                    `json:"observedConclusion,omitempty"`
	OperationID        string                    `json:"operationId,omitempty"`
}

type DeliveryRecordOperation struct {
	ID       string `json:"id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type DeliveryRecordResult struct {
	SchemaVersion   int                            `json:"schemaVersion"`
	Kind            string                         `json:"kind"`
	Repository      string                         `json:"repository"`
	Complete        bool                           `json:"complete"`
	Applicable      bool                           `json:"applicable"`
	Candidates      []delivery.ResourceIdentity    `json:"candidates"`
	Ownership       []DeliveryOwnershipBackfill    `json:"ownershipBackfill"`
	Append          *delivery.StagedWorkAppendPlan `json:"append,omitempty"`
	Synchronization *delivery.SynchronizationPlan  `json:"synchronization,omitempty"`
	Coverage        *delivery.CoverageAdvancePlan  `json:"coverage,omitempty"`
	Rechecks        []DeliveryPromotionRecheck     `json:"promotionRechecks"`
	Operations      []DeliveryRecordOperation      `json:"operations"`
	Warnings        []string                       `json:"warnings"`
	Report          *operation.Report              `json:"report,omitempty"`
}

type DeliveryRecordGitHub interface {
	deliveryStoreReader
	GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error)
	GetBranchContext(context.Context, string, string) (gh.Branch, error)
	ListClosedPullRequestsBoundedContext(context.Context, string, string) (gh.PullRequestListing, error)
	ListClosedPullRequestsUpdatedSinceContext(context.Context, string, string, time.Time) (gh.PullRequestListing, error)
	ListOpenPullRequestsBoundedContext(context.Context, string, string) (gh.PullRequestListing, error)
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
	AddIssueLabelsContext(context.Context, string, int, []string) error
	CreateIssueCommentReturningContext(context.Context, string, int, string) (gh.IssueComment, error)
	UpdateIssueCommentReturningContext(context.Context, string, int64, string) (gh.IssueComment, error)
	ListNewestIssueCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error)
	ListNewestPullRequestCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error)
	ListIssueCommentsAfterContext(context.Context, string, int, time.Time) (gh.IssueCommentListing, error)
	CompareCommitsContext(context.Context, string, string, string) (gh.CommitComparison, error)
	GetCheckRollupContext(context.Context, string, int, string) (gh.CheckRollup, error)
	RerequestCheckRunContext(context.Context, string, int64) error
}

type DeliveryRecordWorkflow struct {
	newClient func(context.Context, DeliveryRecordInput) (DeliveryRecordGitHub, error)
}

func NewDeliveryRecordWorkflow() DeliveryRecordWorkflow {
	return DeliveryRecordWorkflow{newClient: func(ctx context.Context, input DeliveryRecordInput) (DeliveryRecordGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow DeliveryRecordWorkflow) RunContext(ctx context.Context, input DeliveryRecordInput) (DeliveryRecordResult, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cfg, err := loadWorkflowConfig(input.ConfigPath)
	if err != nil {
		return DeliveryRecordResult{}, err
	}
	if cfg.Delivery == nil {
		result := DeliveryRecordResult{
			SchemaVersion: 3, Kind: "deliveryRecordPlan", Repository: firstNonBlank(input.Repository, input.EnvironmentRepo),
			Complete: true, Applicable: false, Candidates: []delivery.ResourceIdentity{}, Ownership: []DeliveryOwnershipBackfill{},
			Rechecks: []DeliveryPromotionRecheck{}, Operations: []DeliveryRecordOperation{},
			Warnings: []string{"delivery recording is disabled until repository policy pins a reviewed ledger locator"},
		}
		if input.Apply {
			report := operation.NewReport(nil)
			result.Report = &report
		}
		return result, nil
	}
	locator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return DeliveryRecordResult{}, apperror.Wrap(apperror.Config, err.Error(), err, "Run and review delivery bootstrap before enabling recording.")
	}
	event, hasEvent, err := readOptionalPullRequestEvent(input.EventPath)
	if err != nil {
		return DeliveryRecordResult{}, err
	}
	repoInputs := []repositoryIdentityInput{{source: "--repo", value: input.Repository}, {source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo}, {source: "delivery.repository.full_name", value: locator.Repository.FullName}}
	if hasEvent {
		repoInputs = append(repoInputs, repositoryIdentityInput{source: "pull_request.base.repo.full_name", value: event.BaseRepositoryFullName})
	}
	repo, err := resolveRepositoryIdentities(repoInputs...)
	if err != nil {
		return DeliveryRecordResult{}, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return DeliveryRecordResult{}, err
	}
	if input.Apply && (input.RunID <= 0 || strings.TrimSpace(input.WorkflowName) != "Delivery Recorder") {
		return DeliveryRecordResult{}, apperror.New(apperror.Usage, "delivery apply requires the trusted Delivery Recorder workflow", "Use the generated workflow so writes share its delivery concurrency group.")
	}
	store, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return DeliveryRecordResult{}, classifyGitHubError(err)
	}
	if store.Snapshot.Checkpoint.PendingRechecks != nil {
		pending := planPendingPromotionRechecks(repo, store)
		if !input.Apply {
			return pending, nil
		}
		applied, applyErr := applyPendingPromotionRechecks(ctx, client, repo, store, pending)
		if applyErr != nil {
			return applied, applyErr
		}
		next, nextErr := workflow.RunContext(ctx, input)
		combined := mergeDeliveryRecordResults(applied, next)
		if nextErr != nil && combined.Report != nil {
			return combined, apperror.WithReport(nextErr, *combined.Report)
		}
		return combined, nextErr
	}
	if hasEvent && isBaseToStagingSynchronization(event, cfg) {
		return runDeliverySynchronization(ctx, client, repo, cfg, store, event, input)
	}
	candidates, observedStagingSHA, complete, warnings, err := deliveryRecordCandidates(ctx, client, repo, cfg, store.Snapshot, event, hasEvent)
	if err != nil {
		return DeliveryRecordResult{}, classifyGitHubError(err)
	}
	result := DeliveryRecordResult{SchemaVersion: 3, Kind: "deliveryRecordPlan", Repository: repo, Complete: complete, Applicable: complete, Candidates: []delivery.ResourceIdentity{}, Ownership: []DeliveryOwnershipBackfill{}, Rechecks: []DeliveryPromotionRecheck{}, Operations: []DeliveryRecordOperation{}, Warnings: warnings}
	for _, candidate := range candidates {
		result.Candidates = append(result.Candidates, delivery.ResourceIdentity{Number: candidate.Number, NodeID: candidate.NodeID})
	}
	if len(candidates) == 0 {
		if !result.Complete {
			if input.Apply {
				return result, apperror.New(apperror.Policy, "delivery reconciliation is incomplete", "Resolve the reported acquisition or ancestry ambiguity before retrying.")
			}
			return result, nil
		}
		result, err = planDeliveryRechecks(ctx, client, repo, cfg, store.Snapshot.Checkpoint.Coverage.StagingSHA, "coverage-repair", result)
		if err != nil {
			return result, err
		}
		if input.Apply && !result.Applicable {
			return result, apperror.New(apperror.Policy, "delivery reconciliation plan is incomplete", "Wait for complete promotion checks or resolve the reported ambiguity, then retry.")
		}
		if input.Apply {
			covered, coverageErr := advanceDeliveryCoverage(ctx, client, repo, cfg, locator, observedStagingSHA, result, input)
			if coverageErr != nil {
				rechecked, _ := applyStandaloneDeliveryRechecks(ctx, client, repo, covered)
				if rechecked.Report != nil {
					return rechecked, apperror.WithReport(coverageErr, *rechecked.Report)
				}
				return covered, coverageErr
			}
			return applyStandaloneDeliveryRechecks(ctx, client, repo, covered)
		}
		if strings.TrimSpace(observedStagingSHA) != "" {
			writer := delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}
			if writer.Workflow == "" {
				writer.Workflow = "delivery-recorder"
			}
			if writer.RunID <= 0 {
				writer.RunID = 1
				result.Warnings = append(result.Warnings, "dry-run uses placeholder writer run ID 1 outside GitHub Actions")
			}
			coverage, planErr := delivery.PlanCoverageAdvance(store.Snapshot, delivery.CoverageAdvanceInput{StagingSHA: observedStagingSHA, Writer: writer, ObservedAt: time.Now().UTC().Format(time.RFC3339)})
			if planErr != nil {
				return result, apperror.Wrap(apperror.Policy, "delivery coverage could not be planned", planErr, "Repair the pinned checkpoint before retrying.")
			}
			result.Coverage = &coverage
			if coverage.Applicable {
				result.Operations = append([]DeliveryRecordOperation{{ID: "staging-coverage", Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "advance_coverage"}}, result.Operations...)
			}
		}
		return result, nil
	}
	if len(candidates) > 1 {
		result.Warnings = append(result.Warnings, "the plan shows the oldest coverage gap; apply reacquires and commits each candidate in order")
	}
	var evidenceEvent *gh.PullRequestEvent
	if hasEvent && event.Number == candidates[0].Number {
		evidenceEvent = &event
	}
	result, err = planDeliveryRecord(ctx, client, repo, cfg, store, candidates[0], evidenceEvent, input, result)
	if err != nil {
		return result, err
	}
	if !input.Apply {
		return result, nil
	}
	if !result.Applicable {
		return result, apperror.New(apperror.Policy, "delivery record plan is incomplete", "Resolve the reported ambiguity or acquisition cap, then retry.")
	}
	result = withoutDeliveryRechecks(result)
	applied, err := applyDeliveryRecord(ctx, client, repo, cfg, store, candidates[0], result)
	if err != nil {
		if deliveryReportHasCheckpointCommit(applied.Report) {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, applied, err)
		}
		return applied, err
	}
	combined := applied
	for {
		store, err = acquireDeliveryStore(ctx, client, repo, locator)
		if err != nil {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, classifyGitHubError(err))
		}
		remaining, remainingStagingSHA, remainingComplete, remainingWarnings, err := deliveryRecordCandidates(ctx, client, repo, cfg, store.Snapshot, gh.PullRequestEvent{}, false)
		if err != nil {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, classifyGitHubError(err))
		}
		combined.Complete = combined.Complete && remainingComplete
		combined.Warnings = append(combined.Warnings, remainingWarnings...)
		if !remainingComplete {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, apperror.New(apperror.Policy, "delivery reconciliation became incomplete", "Resolve the reported acquisition cap, then retry."))
		}
		if len(remaining) == 0 {
			covered, coverageErr := advanceDeliveryCoverage(ctx, client, repo, cfg, locator, remainingStagingSHA, combined, input)
			if coverageErr != nil {
				return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, covered, coverageErr)
			}
			return finalizeDeliveryBatchRechecks(ctx, client, repo, cfg, candidates[0].MergeRevision, covered)
		}
		next := DeliveryRecordResult{SchemaVersion: 3, Kind: "deliveryRecordPlan", Repository: repo, Complete: true, Applicable: true, Candidates: []delivery.ResourceIdentity{{Number: remaining[0].Number, NodeID: remaining[0].NodeID}}, Ownership: []DeliveryOwnershipBackfill{}, Rechecks: []DeliveryPromotionRecheck{}, Operations: []DeliveryRecordOperation{}, Warnings: []string{}}
		var nextEvidenceEvent *gh.PullRequestEvent
		if hasEvent && event.Number == remaining[0].Number {
			nextEvidenceEvent = &event
		}
		next, err = planDeliveryRecord(ctx, client, repo, cfg, store, remaining[0], nextEvidenceEvent, input, next)
		if err != nil {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, err)
		}
		if !next.Applicable {
			return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, apperror.New(apperror.Policy, "delivery reconciliation plan is incomplete", "Resolve the reported ambiguity, then retry."))
		}
		next = withoutDeliveryRechecks(next)
		next, err = applyDeliveryRecord(ctx, client, repo, cfg, store, remaining[0], next)
		combined = mergeDeliveryRecordResults(combined, next)
		if err != nil {
			if deliveryReportHasCheckpointCommit(combined.Report) {
				return finalizeDeliveryAfterCommittedError(ctx, client, repo, cfg, candidates[0].MergeRevision, combined, err)
			}
			return combined, apperror.WithReport(err, *combined.Report)
		}
	}
}

func withoutDeliveryRechecks(result DeliveryRecordResult) DeliveryRecordResult {
	result.Rechecks = []DeliveryPromotionRecheck{}
	operations := result.Operations[:0]
	for _, planned := range result.Operations {
		if planned.Action != "rerequest_check" {
			operations = append(operations, planned)
		}
	}
	result.Operations = operations
	return result
}

func deliveryReportHasCheckpointCommit(report *operation.Report) bool {
	if report == nil {
		return false
	}
	for _, item := range report.Operations {
		if item.Action == "update_checkpoint" && item.Status == operation.StatusApplied {
			return true
		}
	}
	return false
}

func finalizeDeliveryBatchRechecks(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, mergeRevision string, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	rechecks, complete, warnings, err := planPromotionRechecks(ctx, client, repo, cfg, mergeRevision)
	if err != nil {
		return failDeliveryRecheckDiscovery(result, "promotion recheck discovery failed after delivery commit", err)
	}
	result.Warnings = append(result.Warnings, warnings...)
	if !complete {
		return failDeliveryRecheckDiscovery(result, "promotion recheck discovery was incomplete after delivery commit", fmt.Errorf("promotion recheck discovery was incomplete"))
	}
	for index := range rechecks {
		rechecks[index].OperationID = fmt.Sprintf("delivery-batch-promotion-%d-policy-recheck", rechecks[index].PullRequest.Number)
		result.Operations = append(result.Operations, DeliveryRecordOperation{ID: rechecks[index].OperationID, Resource: fmt.Sprintf("%s#%d@%s", repo, rechecks[index].PullRequest.Number, rechecks[index].HeadSHA), Action: "rerequest_check"})
	}
	result.Rechecks = rechecks
	return applyStandaloneDeliveryRechecks(ctx, client, repo, result)
}

func failDeliveryRecheckDiscovery(result DeliveryRecordResult, message string, err error) (DeliveryRecordResult, error) {
	failurePlan := DeliveryRecordOperation{ID: "delivery-batch-promotion-discovery", Resource: result.Repository, Action: "discover_promotion_rechecks"}
	result.Operations = append(result.Operations, failurePlan)
	results := append([]operation.Result{}, reportOrEmpty(result.Report).Operations...)
	results = append(results, operation.Result{ID: failurePlan.ID, Resource: failurePlan.Resource, Action: failurePlan.Action, Status: operation.StatusFailed, Error: operationFailure("github", message, err)})
	report := operation.NewReport(results)
	result.Report = &report
	return result, apperror.WithReport(apperror.New(apperror.Policy, message, "Retry the Delivery Recorder workflow."), report)
}

func finalizeDeliveryAfterCommittedError(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, mergeRevision string, result DeliveryRecordResult, primary error) (DeliveryRecordResult, error) {
	if result.Report == nil || (result.Report.Status != operation.ReportFailed && result.Report.Status != operation.ReportPartial && result.Report.Status != operation.ReportRefused) {
		failurePlan := DeliveryRecordOperation{ID: "delivery-batch-reconciliation", Resource: repo, Action: "reconcile_delivery"}
		result.Operations = append(result.Operations, failurePlan)
		results := append([]operation.Result{}, reportOrEmpty(result.Report).Operations...)
		results = append(results, operation.Result{ID: failurePlan.ID, Resource: failurePlan.Resource, Action: failurePlan.Action, Status: operation.StatusFailed, Error: operationFailure("github", "delivery reconciliation failed after a checkpoint commit", primary)})
		report := operation.NewReport(results)
		result.Report = &report
	}
	finalized, cleanupErr := finalizeDeliveryBatchRechecks(ctx, client, repo, cfg, mergeRevision, result)
	if cleanupErr != nil {
		finalized.Warnings = append(finalized.Warnings, "post-commit promotion recheck cleanup did not complete")
	}
	if finalized.Report != nil {
		return finalized, apperror.WithReport(primary, *finalized.Report)
	}
	return finalized, primary
}

func mergeDeliveryRecordResults(base, next DeliveryRecordResult) DeliveryRecordResult {
	seenCandidates := map[int]struct{}{}
	for _, candidate := range base.Candidates {
		seenCandidates[candidate.Number] = struct{}{}
	}
	for _, candidate := range next.Candidates {
		if _, exists := seenCandidates[candidate.Number]; !exists {
			base.Candidates = append(base.Candidates, candidate)
			seenCandidates[candidate.Number] = struct{}{}
		}
	}
	base.Ownership = append(base.Ownership, next.Ownership...)
	if next.Coverage != nil {
		base.Coverage = next.Coverage
	}
	base.Rechecks = append(base.Rechecks, next.Rechecks...)
	base.Operations = append(base.Operations, next.Operations...)
	base.Warnings = append(base.Warnings, next.Warnings...)
	base.Complete = base.Complete && next.Complete
	base.Applicable = base.Applicable && next.Applicable
	results := []operation.Result{}
	if base.Report != nil {
		results = append(results, base.Report.Operations...)
	}
	if next.Report != nil {
		results = append(results, next.Report.Operations...)
	}
	report := operation.NewReport(results)
	base.Report = &report
	return base
}

func planPendingPromotionRechecks(repo string, store acquiredDeliveryStore) DeliveryRecordResult {
	pending := store.Snapshot.Checkpoint.PendingRechecks
	result := DeliveryRecordResult{
		SchemaVersion: 3, Kind: "deliveryRecordPlan", Repository: repo, Complete: true, Applicable: true,
		Candidates: []delivery.ResourceIdentity{}, Ownership: []DeliveryOwnershipBackfill{}, Rechecks: []DeliveryPromotionRecheck{},
		Operations: []DeliveryRecordOperation{}, Warnings: []string{"a committed synchronization recheck batch must be drained before other delivery work"},
	}
	if pending == nil {
		return result
	}
	for _, target := range pending.Targets {
		operationID := fmt.Sprintf("sync-sequence-%d-promotion-%d-policy-recheck", pending.Cause.Sequence, target.PullRequest.Number)
		result.Rechecks = append(result.Rechecks, DeliveryPromotionRecheck{PullRequest: target.PullRequest, HeadSHA: target.HeadSHA, OperationID: operationID})
		result.Operations = append(result.Operations, DeliveryRecordOperation{ID: operationID, Resource: fmt.Sprintf("%s#%d@%s", repo, target.PullRequest.Number, target.HeadSHA), Action: "rerequest_check"})
	}
	result.Operations = append(result.Operations, DeliveryRecordOperation{
		ID:       fmt.Sprintf("sync-sequence-%d-promotion-rechecks-clear", pending.Cause.Sequence),
		Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID),
		Action:   "clear_promotion_rechecks",
	})
	return result
}

func applyPendingPromotionRechecks(ctx context.Context, client DeliveryRecordGitHub, repo string, store acquiredDeliveryStore, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	results := make([]operation.Result, len(result.Operations))
	for index, planned := range result.Operations {
		results[index] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	}
	fail := func(index int, message string, cause error) (DeliveryRecordResult, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, cause)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(cause), report)
	}
	for index, recheck := range result.Rechecks {
		status, err := reconcilePendingPromotionRecheck(ctx, client, repo, recheck)
		if err != nil {
			return fail(index, "pending promotion policy recheck failed", err)
		}
		results[index].Status = status
	}
	clearIndex := len(results) - 1
	fresh, err := acquireDeliveryStore(ctx, client, repo, store.Snapshot.Locator)
	if err != nil {
		return fail(clearIndex, "pending promotion recheck checkpoint readback failed", err)
	}
	if !samePendingPromotionRechecks(store.Snapshot.Checkpoint.PendingRechecks, fresh.Snapshot.Checkpoint.PendingRechecks) {
		return fail(clearIndex, "pending promotion recheck batch changed before clear", fmt.Errorf("checkpoint pending batch changed"))
	}
	clearPlan, err := delivery.PlanPromotionRecheckClear(fresh.Snapshot)
	if err != nil || !clearPlan.Applicable || clearPlan.Checkpoint == nil {
		if err == nil {
			err = fmt.Errorf("pending promotion recheck batch is missing")
		}
		return fail(clearIndex, "pending promotion recheck clear planning failed", err)
	}
	cleared, err := client.UpdateIssueCommentReturningContext(ctx, repo, fresh.CheckpointComment.ID, clearPlan.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, fresh.CheckpointComment.ID)
		if readErr != nil || readback.Body != clearPlan.CheckpointBody {
			return fail(clearIndex, "pending promotion recheck clear failed", err)
		}
		cleared = readback
	}
	if trustErr := validateTrustedDeliveryWrite(cleared, repo, fresh.LedgerIssue.Number); trustErr != nil || cleared.ID != fresh.CheckpointComment.ID || cleared.NodeID != fresh.CheckpointComment.NodeID || cleared.Body != clearPlan.CheckpointBody {
		if trustErr == nil {
			trustErr = fmt.Errorf("pending promotion recheck clear returned stale content or identity")
		}
		return fail(clearIndex, "pending promotion recheck clear was not trusted", trustErr)
	}
	results[clearIndex].Status = operation.StatusApplied
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func samePendingPromotionRechecks(left, right *delivery.PendingPromotionRechecks) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Cause != right.Cause || len(left.Targets) != len(right.Targets) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	return true
}

func advanceDeliveryCoverage(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, locator delivery.DeliveryStoreLocator, observedStagingSHA string, result DeliveryRecordResult, input DeliveryRecordInput) (DeliveryRecordResult, error) {
	if strings.TrimSpace(observedStagingSHA) == "" {
		if result.Report == nil {
			report := operation.NewReport(nil)
			result.Report = &report
		}
		return result, nil
	}
	store, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return result, apperror.WithReport(classifyGitHubError(err), reportOrEmpty(result.Report))
	}
	branch, err := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return result, apperror.WithReport(classifyGitHubError(err), reportOrEmpty(result.Report))
	}
	if branch.SHA != observedStagingSHA {
		return result, apperror.WithReport(apperror.New(apperror.GitHub, "staging changed after delivery reconciliation", "Retry against the new staging head."), reportOrEmpty(result.Report))
	}
	comparison, err := client.CompareCommitsContext(ctx, repo, store.Snapshot.Checkpoint.Coverage.StagingSHA, observedStagingSHA)
	if err != nil {
		return result, apperror.WithReport(classifyGitHubError(err), reportOrEmpty(result.Report))
	}
	if comparison.Status != "ahead" && comparison.Status != "identical" {
		return result, apperror.WithReport(apperror.New(apperror.Policy, "staging diverges from committed delivery coverage", "Repair or explicitly rebootstrap the coverage boundary before retrying."), reportOrEmpty(result.Report))
	}
	plan, err := delivery.PlanCoverageAdvance(store.Snapshot, delivery.CoverageAdvanceInput{
		StagingSHA: observedStagingSHA,
		Writer:     delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID},
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return result, apperror.WithReport(apperror.Wrap(apperror.Policy, "delivery coverage could not be advanced", err, "Repair the pinned checkpoint before retrying."), reportOrEmpty(result.Report))
	}
	result.Coverage = &plan
	if !plan.Applicable || plan.Checkpoint == nil {
		if result.Report == nil {
			report := operation.NewReport(nil)
			result.Report = &report
		}
		return result, nil
	}
	planned := DeliveryRecordOperation{ID: "staging-coverage", Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "advance_coverage"}
	if result.Report != nil && len(result.Report.Operations) > 0 {
		result.Operations = append(result.Operations, planned)
	} else {
		result.Operations = append([]DeliveryRecordOperation{planned}, result.Operations...)
	}
	results := append([]operation.Result{}, reportOrEmpty(result.Report).Operations...)
	operationResult := operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	checkpointNow, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
	if err == nil {
		err = validateTrustedDeliveryWrite(checkpointNow, repo, store.LedgerIssue.Number)
	}
	if err == nil {
		var current delivery.DeliveryCheckpoint
		current, err = delivery.ParseCheckpointIndex(locator, storedDeliveryComment(checkpointNow))
		if err == nil && current.Digest != plan.Precondition.Digest {
			err = fmt.Errorf("delivery coverage checkpoint changed after planning")
		}
	}
	if err != nil {
		operationResult.Status = operation.StatusFailed
		operationResult.Error = operationFailure("github", "delivery coverage preflight failed", err)
		results = append(results, operationResult)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, store.CheckpointComment.ID, plan.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
		if readErr == nil && readback.Body == plan.CheckpointBody && readback.ID == store.CheckpointComment.ID && readback.NodeID == store.CheckpointComment.NodeID && validateTrustedDeliveryWrite(readback, repo, store.LedgerIssue.Number) == nil {
			updated, err = readback, nil
		}
	}
	if err == nil {
		err = validateTrustedDeliveryWrite(updated, repo, store.LedgerIssue.Number)
	}
	if err == nil && (updated.Body != plan.CheckpointBody || updated.ID != store.CheckpointComment.ID || updated.NodeID != store.CheckpointComment.NodeID) {
		err = fmt.Errorf("delivery coverage update returned stale content or identity")
	}
	if err != nil {
		operationResult.Status = operation.StatusFailed
		operationResult.Error = operationFailure("github", "delivery coverage update failed", err)
		results = append(results, operationResult)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	operationResult.Status = operation.StatusApplied
	results = append(results, operationResult)
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func reportOrEmpty(report *operation.Report) operation.Report {
	if report == nil {
		return operation.NewReport(nil)
	}
	return *report
}

func readOptionalPullRequestEvent(path string) (gh.PullRequestEvent, bool, error) {
	if strings.TrimSpace(path) == "" {
		return gh.PullRequestEvent{}, false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return gh.PullRequestEvent{}, false, apperror.Wrap(apperror.Usage, "pull request event could not be read", err, "")
	}
	event, err := gh.ParsePullRequestEvent(content)
	if err != nil {
		return gh.PullRequestEvent{}, false, apperror.Wrap(apperror.Usage, "pull request event could not be parsed", err, "")
	}
	return event, true, nil
}

func isBaseToStagingSynchronization(event gh.PullRequestEvent, cfg config.Config) bool {
	return event.Action == "closed" && event.Merged && event.HeadRef == cfg.Repository.BaseBranch && event.BaseRef == cfg.Repository.StagingBranch &&
		strings.TrimSpace(event.HeadRepositoryFullName) != "" && strings.EqualFold(event.HeadRepositoryFullName, event.BaseRepositoryFullName)
}

func runDeliverySynchronization(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, event gh.PullRequestEvent, input DeliveryRecordInput) (DeliveryRecordResult, error) {
	result := DeliveryRecordResult{
		SchemaVersion: 3, Kind: "deliveryRecordPlan", Repository: repo, Complete: true, Applicable: true,
		Candidates: []delivery.ResourceIdentity{{Number: event.Number, NodeID: event.NodeID}}, Ownership: []DeliveryOwnershipBackfill{},
		Rechecks: []DeliveryPromotionRecheck{}, Operations: []DeliveryRecordOperation{}, Warnings: []string{},
	}
	latest, err := client.GetPullRequestContext(ctx, repo, event.Number)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	if !deliveryPullRequestMatchesEvent(latest, event) || latest.HeadRef != cfg.Repository.BaseBranch || latest.BaseRef != cfg.Repository.StagingBranch || !latest.Merged {
		return result, apperror.New(apperror.GitHub, "base-to-staging synchronization event is stale", "Retry the exact merged synchronization event.")
	}
	if !validSynchronizationRevisions(event) {
		return result, apperror.New(apperror.Policy, "base-to-staging synchronization revisions are incomplete", "Use a normal pull request whose merged event includes exact base, head, and merge revisions.")
	}
	if coverage := store.Snapshot.Checkpoint.Coverage.StagingSHA; coverage != event.BaseSHA && coverage != event.MergeRevision {
		result.Complete, result.Applicable = false, false
		result.Warnings = append(result.Warnings, "delivery coverage must be repaired before synchronization is recorded")
		if input.Apply {
			return result, apperror.New(apperror.Policy, "delivery repair must precede base-to-staging synchronization", "Run the Delivery Recorder reconciliation, then retry this exact synchronization event.")
		}
		return result, nil
	}
	for _, pair := range [][2]string{{event.HeadSHA, event.MergeRevision}, {event.BaseSHA, event.MergeRevision}} {
		comparison, compareErr := client.CompareCommitsContext(ctx, repo, pair[0], pair[1])
		if compareErr != nil {
			return result, classifyGitHubError(compareErr)
		}
		if comparison.Status != "ahead" && comparison.Status != "identical" {
			result.Applicable = false
			result.Warnings = append(result.Warnings, "synchronization did not preserve both base and staging ancestry")
			if input.Apply {
				return result, apperror.New(apperror.Policy, "base-to-staging synchronization must use an ancestry-preserving merge", "Open a new reviewed PR from base to staging and merge it with a merge commit; do not squash or rebase it.")
			}
			return result, nil
		}
	}
	staging, err := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	containsResult, err := client.CompareCommitsContext(ctx, repo, event.MergeRevision, staging.SHA)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	if containsResult.Status != "ahead" && containsResult.Status != "identical" {
		return result, apperror.New(apperror.Policy, "current staging does not contain the synchronization result", "Repair the staging branch without rewriting its acknowledged history, then retry.")
	}
	writer := delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}
	if writer.Workflow == "" {
		writer.Workflow = "delivery-recorder"
	}
	if writer.RunID <= 0 {
		writer.RunID = 1
		result.Warnings = append(result.Warnings, "dry-run uses placeholder writer run ID 1 outside GitHub Actions")
	}
	rechecks, complete, warnings, err := planPromotionRechecks(ctx, client, repo, cfg, event.MergeRevision)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	result.Complete, result.Applicable = result.Complete && complete, result.Applicable && complete
	result.Warnings = append(result.Warnings, warnings...)
	for _, recheck := range rechecks {
		if recheck.ObservedStatus != "completed" {
			result.Complete, result.Applicable = false, false
			result.Warnings = append(result.Warnings, fmt.Sprintf("promotion PR #%d policy check is not completed", recheck.PullRequest.Number))
		}
	}
	if !result.Complete {
		if input.Apply {
			return result, apperror.New(apperror.Policy, "base-to-staging synchronization recheck planning is incomplete", "Retry after the reported promotion check facts can be acquired completely.")
		}
		return result, nil
	}
	targets := make([]delivery.PromotionRecheckTarget, 0, len(rechecks))
	for index := range rechecks {
		rechecks[index].OperationID = fmt.Sprintf("sync-pr-%d-promotion-%d-policy-recheck", event.Number, rechecks[index].PullRequest.Number)
		targets = append(targets, delivery.PromotionRecheckTarget{PullRequest: rechecks[index].PullRequest, HeadSHA: rechecks[index].HeadSHA})
	}
	result.Rechecks = rechecks
	plan, err := delivery.PlanSynchronization(store.Snapshot, delivery.SynchronizationInput{
		PullRequest: delivery.ResourceIdentity{Number: latest.Number, NodeID: latest.NodeID}, PriorStagingSHA: event.BaseSHA,
		BaseSHA: event.HeadSHA, StagingSHA: event.MergeRevision, Writer: writer, RecordedAt: event.MergedAt.UTC().Format(time.RFC3339),
		RecheckTargets: targets,
	})
	if err != nil {
		return result, apperror.Wrap(apperror.Policy, "base-to-staging synchronization could not be planned", err, "Repair the delivery checkpoint, then retry.")
	}
	result.Synchronization = &plan
	if !plan.Applicable {
		result.Applicable = false
		result.Warnings = append(result.Warnings, "equivalent base-to-staging synchronization is already committed")
		if input.Apply {
			report := operation.NewReport(nil)
			result.Report = &report
		}
		return result, nil
	}
	result.Operations = append(result.Operations,
		DeliveryRecordOperation{ID: fmt.Sprintf("sync-pr-%d-integration-record", event.Number), Resource: fmt.Sprintf("%s#%d", repo, store.LedgerIssue.Number), Action: "append_base_integration"},
		DeliveryRecordOperation{ID: fmt.Sprintf("sync-pr-%d-checkpoint", event.Number), Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "update_checkpoint"},
	)
	for _, recheck := range rechecks {
		result.Operations = append(result.Operations, DeliveryRecordOperation{ID: recheck.OperationID, Resource: fmt.Sprintf("%s#%d@%s", repo, recheck.PullRequest.Number, recheck.HeadSHA), Action: "rerequest_check"})
	}
	if len(rechecks) > 0 {
		result.Operations = append(result.Operations, DeliveryRecordOperation{ID: fmt.Sprintf("sync-pr-%d-promotion-rechecks-clear", event.Number), Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "clear_promotion_rechecks"})
	}
	if !input.Apply {
		return result, nil
	}
	return applyDeliverySynchronization(ctx, client, repo, cfg, store, latest, result)
}

func validSynchronizationRevisions(event gh.PullRequestEvent) bool {
	return len(strings.TrimSpace(event.BaseSHA)) == 40 && len(strings.TrimSpace(event.HeadSHA)) == 40 && len(strings.TrimSpace(event.MergeRevision)) == 40 && !event.MergedAt.IsZero()
}

func applyDeliverySynchronization(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, pr gh.PullRequest, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	results := make([]operation.Result, len(result.Operations))
	for index, planned := range result.Operations {
		results[index] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	}
	fail := func(index int, message string, cause error) (DeliveryRecordResult, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, cause)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(cause), report)
	}
	if result.Synchronization == nil || result.Synchronization.Record == nil {
		return fail(0, "synchronization plan is incomplete", fmt.Errorf("missing base-integration record"))
	}
	latest, err := client.GetPullRequestContext(ctx, repo, pr.Number)
	if err != nil {
		return fail(0, "synchronization pull request preflight failed", err)
	}
	record := result.Synchronization.Record
	if latest.NodeID != record.PullRequest.NodeID || latest.HeadSHA != record.BaseSHA || latest.BaseSHA != record.PriorStagingSHA || latest.MergeRevision != record.StagingSHA || latest.HeadRef != cfg.Repository.BaseBranch || latest.BaseRef != cfg.Repository.StagingBranch || !latest.Merged {
		return fail(0, "synchronization pull request changed after planning", fmt.Errorf("stale pull request revision"))
	}
	staging, err := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return fail(0, "synchronization staging preflight failed", err)
	}
	containsResult, err := client.CompareCommitsContext(ctx, repo, record.StagingSHA, staging.SHA)
	if err != nil {
		return fail(0, "synchronization staging preflight failed", err)
	}
	if containsResult.Status != "ahead" && containsResult.Status != "identical" {
		return fail(0, "synchronization staging changed after planning", fmt.Errorf("current staging does not contain the synchronization result"))
	}
	checkpointNow, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
	if err != nil {
		return fail(1, "synchronization checkpoint preflight failed", err)
	}
	current, err := delivery.ParseCheckpointIndex(result.Synchronization.Locator, storedDeliveryComment(checkpointNow))
	if err != nil {
		return fail(1, "synchronization checkpoint preflight failed", err)
	}
	body, err := delivery.RenderBaseIntegrationRecord(*record)
	if err != nil {
		return fail(0, "synchronization record encoding failed", err)
	}
	comment, err := findTrustedBaseIntegrationRetry(ctx, client, repo, store.LedgerIssue.Number, checkpointNow.UpdatedAt, record.RetryID)
	if err != nil {
		return fail(0, "synchronization retry lookup failed", err)
	}
	recordStatus := operation.StatusUnchanged
	if comment == nil {
		created, createErr := client.CreateIssueCommentReturningContext(ctx, repo, store.LedgerIssue.Number, body)
		if createErr != nil {
			comment, err = findTrustedBaseIntegrationRetry(ctx, client, repo, store.LedgerIssue.Number, checkpointNow.UpdatedAt, record.RetryID)
			if err != nil || comment == nil {
				if err == nil {
					err = createErr
				}
				return fail(0, "synchronization record append failed", err)
			}
		} else {
			comment = &created
		}
		recordStatus = operation.StatusApplied
	}
	if err := validateTrustedDeliveryWrite(*comment, repo, store.LedgerIssue.Number); err != nil {
		return fail(0, "synchronization record append was not trusted", err)
	}
	parsed, err := delivery.ParseBaseIntegrationComment(storedDeliveryComment(*comment))
	if err != nil {
		return fail(0, "synchronization record append was not exact", err)
	}
	if parsed.Digest != record.Digest {
		adopted, adoptErr := delivery.AdoptSynchronizationRetry(*result.Synchronization, current, parsed)
		if adoptErr != nil {
			return fail(0, "synchronization record append was not exact", adoptErr)
		}
		result.Synchronization = &adopted
	}
	commit, err := delivery.FinalizeSynchronization(*result.Synchronization, current, delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID})
	if err != nil {
		return fail(1, "synchronization checkpoint finalization failed", err)
	}
	results[0].Status = recordStatus
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, store.CheckpointComment.ID, commit.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
		if readErr != nil || readback.Body != commit.CheckpointBody {
			return fail(1, "synchronization checkpoint update failed", err)
		}
		updated = readback
	}
	if err := validateTrustedDeliveryWrite(updated, repo, store.LedgerIssue.Number); err != nil || updated.ID != store.CheckpointComment.ID || updated.NodeID != store.CheckpointComment.NodeID || updated.Body != commit.CheckpointBody {
		if err == nil {
			err = fmt.Errorf("synchronization checkpoint update returned stale content or identity")
		}
		return fail(1, "synchronization checkpoint update was not trusted", err)
	}
	results[1].Status = operation.StatusApplied
	for index, recheck := range result.Rechecks {
		status, recheckErr := reconcilePendingPromotionRecheck(ctx, client, repo, recheck)
		if recheckErr != nil {
			return fail(index+2, "promotion policy recheck failed", recheckErr)
		}
		results[index+2].Status = status
	}
	if len(result.Rechecks) > 0 {
		clearIndex := len(results) - 1
		fresh, err := acquireDeliveryStore(ctx, client, repo, result.Synchronization.Locator)
		if err != nil {
			return fail(clearIndex, "pending promotion recheck checkpoint readback failed", err)
		}
		clearPlan, err := delivery.PlanPromotionRecheckClear(fresh.Snapshot)
		if err != nil || !clearPlan.Applicable || clearPlan.Checkpoint == nil || !samePendingPromotionRechecks(commit.Checkpoint.PendingRechecks, clearPlan.Pending) {
			if err == nil {
				err = fmt.Errorf("pending promotion recheck batch changed before clear")
			}
			return fail(clearIndex, "pending promotion recheck clear planning failed", err)
		}
		cleared, err := client.UpdateIssueCommentReturningContext(ctx, repo, fresh.CheckpointComment.ID, clearPlan.CheckpointBody)
		if err != nil {
			readback, readErr := client.GetIssueCommentContext(ctx, repo, fresh.CheckpointComment.ID)
			if readErr != nil || readback.Body != clearPlan.CheckpointBody {
				return fail(clearIndex, "pending promotion recheck clear failed", err)
			}
			cleared = readback
		}
		if trustErr := validateTrustedDeliveryWrite(cleared, repo, fresh.LedgerIssue.Number); trustErr != nil || cleared.ID != fresh.CheckpointComment.ID || cleared.NodeID != fresh.CheckpointComment.NodeID || cleared.Body != clearPlan.CheckpointBody {
			if trustErr == nil {
				trustErr = fmt.Errorf("pending promotion recheck clear returned stale content or identity")
			}
			return fail(clearIndex, "pending promotion recheck clear was not trusted", trustErr)
		}
		results[clearIndex].Status = operation.StatusApplied
	}
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func reconcilePendingPromotionRecheck(ctx context.Context, client DeliveryRecordGitHub, repo string, recheck DeliveryPromotionRecheck) (operation.Status, error) {
	latest, err := client.GetPullRequestContext(ctx, repo, recheck.PullRequest.Number)
	if err != nil {
		return operation.StatusFailed, err
	}
	if latest.State == "closed" || latest.NodeID != recheck.PullRequest.NodeID || latest.HeadSHA != recheck.HeadSHA {
		return operation.StatusUnchanged, nil
	}
	rollup, err := client.GetCheckRollupContext(ctx, repo, recheck.PullRequest.Number, recheck.HeadSHA)
	if err != nil {
		return operation.StatusFailed, err
	}
	if !rollup.Complete {
		return operation.StatusFailed, fmt.Errorf("promotion policy check acquisition is incomplete")
	}
	matches := []gh.CheckState{}
	for _, check := range rollup.Checks {
		if check.Name == deliveryPolicyCheckName && check.ID > 0 {
			matches = append(matches, check)
		}
	}
	if len(matches) != 1 {
		return operation.StatusFailed, fmt.Errorf("promotion policy check identity is ambiguous or missing")
	}
	if matches[0].Status != "completed" {
		return operation.StatusUnchanged, nil
	}
	if err := client.RerequestCheckRunContext(ctx, repo, matches[0].ID); err != nil {
		return operation.StatusFailed, err
	}
	return operation.StatusApplied, nil
}

func deliveryRecordCandidates(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, snapshot delivery.Snapshot, event gh.PullRequestEvent, hasEvent bool) ([]gh.PullRequest, string, bool, []string, error) {
	eventRequiresCandidate := false
	warnings := []string{}
	if hasEvent {
		if event.Action != "closed" || !event.Merged {
			return nil, "", true, []string{"pull request event is not a merged close; no delivery record is needed"}, nil
		}
		latest, err := client.GetPullRequestContext(ctx, repo, event.Number)
		if err != nil {
			return nil, "", false, nil, err
		}
		if !deliveryPullRequestMatchesEvent(latest, event) {
			return nil, "", false, nil, fmt.Errorf("delivery record event is stale")
		}
		if !isManagedWorkPullRequest(latest, cfg) {
			warnings = append(warnings, "pull request is not a managed staging work merge; reconciliation still scanned staging")
		} else {
			eventRequiresCandidate = true
		}
	}
	branch, err := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return nil, "", false, nil, err
	}
	coverageToCurrent, err := client.CompareCommitsContext(ctx, repo, snapshot.Checkpoint.Coverage.StagingSHA, branch.SHA)
	if err != nil {
		return nil, "", false, nil, err
	}
	if coverageToCurrent.Status != "ahead" && coverageToCurrent.Status != "identical" {
		return nil, branch.SHA, false, []string{"current staging diverges from the committed delivery coverage"}, nil
	}
	boundary, err := time.Parse(time.RFC3339, snapshot.Checkpoint.Coverage.ObservedAt)
	if err != nil {
		return nil, branch.SHA, false, nil, fmt.Errorf("delivery coverage observation time is invalid: %w", err)
	}
	listing, err := client.ListClosedPullRequestsUpdatedSinceContext(ctx, repo, cfg.Repository.StagingBranch, boundary)
	if err != nil {
		return nil, "", false, nil, err
	}
	if !listing.Complete {
		return nil, branch.SHA, false, []string{"closed staging pull request acquisition was unstable or did not reach the committed coverage time boundary"}, nil
	}
	candidates := []gh.PullRequest{}
	distances := map[int]int{}
	for _, pr := range listing.PullRequests {
		if !pr.Merged || !isManagedWorkPullRequest(pr, cfg) || pr.MergeRevision == "" {
			continue
		}
		if stagedWorkRecordedForPR(snapshot, pr.Number) {
			continue
		}
		fromCoverage, err := client.CompareCommitsContext(ctx, repo, snapshot.Checkpoint.Coverage.StagingSHA, pr.MergeRevision)
		if err != nil {
			return nil, branch.SHA, false, nil, err
		}
		switch fromCoverage.Status {
		case "ahead":
			distances[pr.Number] = fromCoverage.AheadBy
		case "behind", "identical":
			continue
		default:
			return nil, branch.SHA, false, []string{fmt.Sprintf("merged work PR #%d diverges from the committed staging coverage", pr.Number)}, nil
		}
		toCurrent, err := client.CompareCommitsContext(ctx, repo, pr.MergeRevision, branch.SHA)
		if err != nil {
			return nil, branch.SHA, false, nil, err
		}
		if toCurrent.Status != "ahead" && toCurrent.Status != "identical" {
			return nil, branch.SHA, false, []string{fmt.Sprintf("merged work PR #%d is not contained in current staging", pr.Number)}, nil
		}
		candidates = append(candidates, pr)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := distances[candidates[i].Number], distances[candidates[j].Number]
		if left == right {
			return candidates[i].Number < candidates[j].Number
		}
		return left < right
	})
	for index := 1; index < len(candidates); index++ {
		if distances[candidates[index-1].Number] == distances[candidates[index].Number] {
			return nil, branch.SHA, false, append(warnings, fmt.Sprintf("merged work PRs #%d and #%d have the same ancestry distance from committed coverage", candidates[index-1].Number, candidates[index].Number)), nil
		}
		comparison, err := client.CompareCommitsContext(ctx, repo, candidates[index-1].MergeRevision, candidates[index].MergeRevision)
		if err != nil {
			return nil, branch.SHA, false, nil, err
		}
		if comparison.Status != "ahead" {
			return nil, branch.SHA, false, append(warnings, fmt.Sprintf("merged work PR #%d does not extend the prior candidate #%d", candidates[index].Number, candidates[index-1].Number)), nil
		}
	}
	if eventRequiresCandidate && !stagedWorkRecordedForPR(snapshot, event.Number) {
		found := false
		for _, candidate := range candidates {
			if candidate.Number == event.Number {
				found = true
				break
			}
		}
		if !found {
			return nil, branch.SHA, false, []string{fmt.Sprintf("merged event PR #%d is not represented after the committed coverage boundary", event.Number)}, nil
		}
	}
	return candidates, branch.SHA, true, warnings, nil
}

func planDeliveryRecord(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, pr gh.PullRequest, event *gh.PullRequestEvent, input DeliveryRecordInput, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	evidence, err := acquireManagedWorkPolicyEvidence(ctx, client, repo, store.Snapshot.Locator.Repository, pr, event)
	if err != nil {
		result.Applicable = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("merged work PR #%d has no usable immutable policy evidence: %v", pr.Number, err))
		return result, nil
	}
	managed := make([]delivery.ManagedIssueReference, 0, len(evidence.Issues))
	for _, expected := range evidence.Issues {
		issue, err := client.GetIssueContext(ctx, repo, expected.Number)
		if err != nil {
			return result, classifyGitHubError(err)
		}
		if issue.PullRequest || issue.NodeID != expected.NodeID {
			result.Applicable = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("issue #%d identity no longer matches its reviewed policy evidence", expected.Number))
			continue
		}
		comments, err := client.ListIssueCommentsContext(ctx, repo, expected.Number)
		if err != nil {
			return result, classifyGitHubError(err)
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels, Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy})
		record := policy.NewManagedIssueRecord(issue.NodeID, issue.Number)
		switch ownership.Source {
		case policy.IssueOwnershipRecord:
			record = *ownership.Record
		case policy.IssueOwnershipLegacyFingerprint:
			result.Ownership = append(result.Ownership, DeliveryOwnershipBackfill{Issue: delivery.ResourceIdentity{Number: issue.Number, NodeID: issue.NodeID}, Digest: record.Digest, Body: policy.RenderManagedIssueRecord(record)})
		case policy.IssueOwnershipNone, policy.IssueOwnershipUnavailable:
			result.Ownership = append(result.Ownership, DeliveryOwnershipBackfill{Issue: delivery.ResourceIdentity{Number: issue.Number, NodeID: issue.NodeID}, Digest: record.Digest, Body: policy.RenderManagedIssueRecord(record)})
		default:
			result.Applicable = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("issue #%d has contradictory or malformed durable ownership", issue.Number))
			continue
		}
		if record.Digest != expected.OwnershipDigest {
			result.Applicable = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("issue #%d ownership digest changed after policy review", issue.Number))
			continue
		}
		managed = append(managed, expected)
	}
	if !result.Applicable {
		return result, nil
	}
	writer := delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}
	if writer.Workflow == "" {
		writer.Workflow = "delivery-recorder"
	}
	if writer.RunID <= 0 {
		if input.Apply {
			return result, apperror.New(apperror.Usage, "delivery recording requires GITHUB_RUN_ID", "Run apply from the trusted generated workflow.")
		}
		writer.RunID = 1
		result.Warnings = append(result.Warnings, "dry-run uses placeholder writer run ID 1 outside GitHub Actions")
	}
	appendPlan, err := delivery.PlanStagedWorkAppend(store.Snapshot, delivery.StagedWorkAppendInput{
		PullRequest: delivery.ResourceIdentity{Number: pr.Number, NodeID: pr.NodeID}, StagingBranch: cfg.Repository.StagingBranch,
		BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA, MergeRevision: pr.MergeRevision, MergedAt: pr.MergedAt.UTC().Format(time.RFC3339), Issues: managed, Writer: writer,
	})
	if err != nil {
		return result, apperror.Wrap(apperror.Policy, "delivery record could not be planned", err, "Repair the pinned ledger before retrying.")
	}
	result.Append = &appendPlan
	if !appendPlan.Applicable {
		result.Warnings = append(result.Warnings, "equivalent staged-work record is already committed")
		return result, nil
	}
	for _, item := range result.Ownership {
		resource := fmt.Sprintf("%s#%d", repo, item.Issue.Number)
		result.Operations = append(result.Operations,
			DeliveryRecordOperation{ID: fmt.Sprintf("issue-%d-ownership-record", item.Issue.Number), Resource: resource, Action: "create_ownership_record"},
			DeliveryRecordOperation{ID: fmt.Sprintf("issue-%d-ownership-index", item.Issue.Number), Resource: resource, Action: "add_ownership_index"},
		)
	}
	result.Operations = append(result.Operations,
		DeliveryRecordOperation{ID: fmt.Sprintf("work-pr-%d-staged-record", pr.Number), Resource: fmt.Sprintf("%s#%d", repo, store.LedgerIssue.Number), Action: "append_record"},
		DeliveryRecordOperation{ID: fmt.Sprintf("work-pr-%d-checkpoint", pr.Number), Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "update_checkpoint"},
	)
	return planDeliveryRechecks(ctx, client, repo, cfg, pr.MergeRevision, fmt.Sprintf("work-pr-%d", pr.Number), result)
}

func acquireManagedWorkPolicyEvidence(ctx context.Context, client DeliveryRecordGitHub, repo string, repository delivery.RepositoryIdentity, pr gh.PullRequest, event *gh.PullRequestEvent) (policy.ManagedWorkPolicyEvidence, error) {
	listing, err := client.ListNewestPullRequestCommentsContext(ctx, repo, pr.Number)
	if err != nil {
		return policy.ManagedWorkPolicyEvidence{}, err
	}
	match, err := policy.FindTrustedManagedWorkPolicyEvidence(managedWorkEvidenceComments(listing.Comments))
	if err != nil {
		return policy.ManagedWorkPolicyEvidence{}, err
	}
	if match == nil {
		if !listing.Complete {
			return policy.ManagedWorkPolicyEvidence{}, errors.New("bounded policy evidence window is incomplete")
		}
		return policy.ManagedWorkPolicyEvidence{}, errors.New("trusted policy evidence is missing")
	}
	evidence := match.Evidence
	if !evidence.Allowed {
		return policy.ManagedWorkPolicyEvidence{}, errors.New("latest policy evidence rejected the pull request")
	}
	if evidence.Repository != repository || evidence.PullRequest != (delivery.ResourceIdentity{Number: pr.Number, NodeID: pr.NodeID}) ||
		evidence.BaseBranch != pr.BaseRef || evidence.WorkBranch != pr.HeadRef || evidence.BaseSHA != pr.BaseSHA || evidence.HeadSHA != pr.HeadSHA {
		return policy.ManagedWorkPolicyEvidence{}, errors.New("policy evidence does not match the merged pull-request revision")
	}
	if evidence.PolicySchemaVersion != policy.PRPolicySchemaVersion {
		return policy.ManagedWorkPolicyEvidence{}, errors.New("policy evidence uses a stale policy schema")
	}
	writtenAt := match.Comment.UpdatedAt
	if writtenAt.IsZero() {
		writtenAt = match.Comment.CreatedAt
	}
	if writtenAt.IsZero() || pr.MergedAt.IsZero() || writtenAt.After(pr.MergedAt) {
		return policy.ManagedWorkPolicyEvidence{}, errors.New("policy evidence was not durably written before merge")
	}
	if event != nil && evidence.ProseDigest != policy.ManagedWorkProseDigest(event.Title, event.Body) {
		return policy.ManagedWorkPolicyEvidence{}, errors.New("policy evidence does not match the merged event prose")
	}
	return evidence, nil
}

func planDeliveryRechecks(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, mergeRevision, operationPrefix string, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	rechecks, complete, warnings, err := planPromotionRechecks(ctx, client, repo, cfg, mergeRevision)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	result.Rechecks, result.Complete = rechecks, result.Complete && complete
	result.Applicable = result.Applicable && result.Complete
	result.Warnings = append(result.Warnings, warnings...)
	for index := range rechecks {
		rechecks[index].OperationID = fmt.Sprintf("%s-promotion-%d-policy-recheck", operationPrefix, rechecks[index].PullRequest.Number)
		result.Operations = append(result.Operations, DeliveryRecordOperation{ID: rechecks[index].OperationID, Resource: fmt.Sprintf("%s#%d@%s", repo, rechecks[index].PullRequest.Number, rechecks[index].HeadSHA), Action: "rerequest_check"})
	}
	result.Rechecks = rechecks
	return result, nil
}

func planPromotionRechecks(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, mergeRevision string) ([]DeliveryPromotionRecheck, bool, []string, error) {
	listing, err := client.ListOpenPullRequestsBoundedContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return nil, false, nil, err
	}
	if !listing.Complete {
		return nil, false, []string{"open promotion acquisition reached the 100-item cap"}, nil
	}
	result := []DeliveryPromotionRecheck{}
	for _, pr := range listing.PullRequests {
		if policy.ClassifyPullRequestFlow(policy.PullRequest{Number: pr.Number, Title: pr.Title, Body: pr.Body, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, BaseRepositoryFullName: pr.BaseRepositoryFullName, HeadRepositoryFullName: pr.HeadRepositoryFullName}, cfg) != policy.PRFlowPromotion {
			continue
		}
		comparison, err := client.CompareCommitsContext(ctx, repo, mergeRevision, pr.HeadSHA)
		if err != nil {
			return nil, false, nil, err
		}
		if comparison.Status != "ahead" && comparison.Status != "identical" {
			continue
		}
		rollup, err := client.GetCheckRollupContext(ctx, repo, pr.Number, pr.HeadSHA)
		if err != nil {
			return nil, false, nil, err
		}
		if !rollup.Complete {
			return nil, false, []string{fmt.Sprintf("promotion PR #%d policy check acquisition is incomplete", pr.Number)}, nil
		}
		matches := []gh.CheckState{}
		for _, check := range rollup.Checks {
			if check.Name == deliveryPolicyCheckName && check.ID > 0 {
				matches = append(matches, check)
			}
		}
		if len(matches) > 1 {
			return nil, false, []string{fmt.Sprintf("promotion PR #%d has ambiguous policy check identities", pr.Number)}, nil
		}
		recheck := DeliveryPromotionRecheck{PullRequest: delivery.ResourceIdentity{Number: pr.Number, NodeID: pr.NodeID}, HeadSHA: pr.HeadSHA}
		if len(matches) == 1 {
			recheck.CheckRunID, recheck.ObservedStatus, recheck.ObservedConclusion = matches[0].ID, matches[0].Status, matches[0].Conclusion
		}
		result = append(result, recheck)
	}
	return result, true, nil, nil
}

func applyDeliveryRecord(ctx context.Context, client DeliveryRecordGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, pr gh.PullRequest, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	results := make([]operation.Result, len(result.Operations))
	for index, planned := range result.Operations {
		results[index] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	}
	fail := func(index int, message string, err error) (DeliveryRecordResult, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, err)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	index := 0
	for _, backfill := range result.Ownership {
		comment, err := client.CreateIssueCommentReturningContext(ctx, repo, backfill.Issue.Number, backfill.Body)
		if err != nil {
			return fail(index, "managed-issue ownership backfill failed", err)
		}
		if err := validateTrustedDeliveryWrite(comment, repo, backfill.Issue.Number); err != nil {
			return fail(index, "managed-issue ownership backfill was not trusted", err)
		}
		results[index].Status = operation.StatusApplied
		index++
		if err := client.AddIssueLabelsContext(ctx, repo, backfill.Issue.Number, []string{policy.ManagedIssueIndexLabel}); err != nil {
			return fail(index, "managed-issue ownership index failed", err)
		}
		results[index].Status = operation.StatusApplied
		index++
	}
	if result.Append == nil || !result.Append.Applicable || result.Append.Record == nil {
		report := operation.NewReport(results)
		result.Report = &report
		return result, nil
	}
	latestPR, err := client.GetPullRequestContext(ctx, repo, pr.Number)
	if err != nil {
		return fail(index, "staged-work pull request preflight failed", err)
	}
	if !pullRequestMatchesStagedWorkRecord(latestPR, *result.Append.Record, cfg) {
		return fail(index, "staged-work pull request changed after planning", fmt.Errorf("stale pull request revision"))
	}
	checkpointNow, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
	if err != nil {
		return fail(index, "delivery checkpoint preflight failed", err)
	}
	current, err := delivery.ParseCheckpointIndex(result.Append.Locator, storedDeliveryComment(checkpointNow))
	if err != nil {
		return fail(index, "delivery checkpoint preflight failed", err)
	}
	recordBody, err := delivery.RenderStagedWorkRecord(*result.Append.Record)
	if err != nil {
		return fail(index, "staged-work record encoding failed", err)
	}
	comment, err := findTrustedStagedWorkRetry(ctx, client, repo, store.LedgerIssue.Number, checkpointNow.UpdatedAt, result.Append.Record.RetryID)
	if err != nil {
		return fail(index, "staged-work retry lookup failed", err)
	}
	recordStatus := operation.StatusUnchanged
	if comment == nil {
		created, createErr := client.CreateIssueCommentReturningContext(ctx, repo, store.LedgerIssue.Number, recordBody)
		if createErr != nil {
			comment, err = findTrustedStagedWorkRetry(ctx, client, repo, store.LedgerIssue.Number, checkpointNow.UpdatedAt, result.Append.Record.RetryID)
			if err != nil {
				return fail(index, "staged-work record append was ambiguous", err)
			}
			if comment == nil {
				return fail(index, "staged-work record append failed", createErr)
			}
		} else {
			comment = &created
		}
		if err := validateTrustedDeliveryWrite(*comment, repo, store.LedgerIssue.Number); err != nil {
			return fail(index, "staged-work record append was not trusted", err)
		}
		recordStatus = operation.StatusApplied
	}
	parsedRecord, err := delivery.ParseStagedWorkComment(storedDeliveryComment(*comment))
	if err != nil {
		return fail(index, "staged-work record append was not exact", err)
	}
	if parsedRecord.Digest != result.Append.Record.Digest {
		adopted, adoptErr := delivery.AdoptStagedWorkRetry(*result.Append, current, parsedRecord)
		if adoptErr != nil {
			return fail(index, "staged-work record append was not exact", adoptErr)
		}
		result.Append = &adopted
	}
	commit, err := delivery.FinalizeStagedWorkAppend(*result.Append, current, delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID})
	if err != nil {
		return fail(index, "staged-work record finalization failed", err)
	}
	results[index].Status = recordStatus
	index++
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, store.CheckpointComment.ID, commit.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
		if readErr == nil && readback.Body == commit.CheckpointBody {
			results[index].Status = operation.StatusApplied
		} else {
			return fail(index, "delivery checkpoint update failed", err)
		}
	} else if err := validateTrustedDeliveryWrite(updated, repo, store.LedgerIssue.Number); err != nil || updated.Body != commit.CheckpointBody || updated.ID != store.CheckpointComment.ID || updated.NodeID != store.CheckpointComment.NodeID {
		if err == nil {
			err = fmt.Errorf("delivery checkpoint update returned stale content or identity")
		}
		return fail(index, "delivery checkpoint update was not trusted", err)
	} else {
		results[index].Status = operation.StatusApplied
	}
	index++
	for _, recheck := range result.Rechecks {
		status, err := reconcilePromotionRecheck(ctx, client, repo, recheck)
		if err != nil {
			return fail(index, "promotion policy recheck failed", err)
		}
		results[index].Status = status
		index++
	}
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func reconcilePromotionRecheck(ctx context.Context, client DeliveryRecordGitHub, repo string, recheck DeliveryPromotionRecheck) (operation.Status, error) {
	latest, err := client.GetPullRequestContext(ctx, repo, recheck.PullRequest.Number)
	if err != nil {
		return operation.StatusFailed, err
	}
	if latest.NodeID != recheck.PullRequest.NodeID || latest.HeadSHA != recheck.HeadSHA {
		return operation.StatusFailed, fmt.Errorf("promotion revision changed")
	}
	rollup, err := client.GetCheckRollupContext(ctx, repo, recheck.PullRequest.Number, recheck.HeadSHA)
	if err != nil {
		return operation.StatusFailed, err
	}
	if !rollup.Complete {
		return operation.StatusFailed, fmt.Errorf("promotion policy check acquisition is incomplete")
	}
	matches := []gh.CheckState{}
	for _, check := range rollup.Checks {
		if check.Name == deliveryPolicyCheckName && check.ID > 0 {
			matches = append(matches, check)
		}
	}
	if len(matches) != 1 {
		return operation.StatusFailed, fmt.Errorf("promotion policy check identity is ambiguous or missing")
	}
	check := matches[0]
	if recheck.CheckRunID > 0 && check.ID != recheck.CheckRunID {
		return operation.StatusFailed, fmt.Errorf("promotion policy check changed after planning")
	}
	if check.Status != "completed" {
		return operation.StatusFailed, fmt.Errorf("promotion policy check is still %s after the delivery commit", check.Status)
	}
	if check.Conclusion != "success" {
		return operation.StatusUnchanged, nil
	}
	if err := client.RerequestCheckRunContext(ctx, repo, check.ID); err != nil {
		return operation.StatusFailed, err
	}
	return operation.StatusApplied, nil
}

func applyStandaloneDeliveryRechecks(ctx context.Context, client DeliveryRecordGitHub, repo string, result DeliveryRecordResult) (DeliveryRecordResult, error) {
	byID := map[string]operation.Result{}
	if result.Report != nil {
		for _, existing := range result.Report.Operations {
			byID[existing.ID] = existing
		}
	}
	for _, planned := range result.Operations {
		if _, exists := byID[planned.ID]; !exists {
			byID[planned.ID] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
		}
	}
	for _, recheck := range result.Rechecks {
		id := recheck.OperationID
		status, err := reconcilePromotionRecheck(ctx, client, repo, recheck)
		current := byID[id]
		current.Status = status
		if err != nil {
			current.Status = operation.StatusFailed
			current.Error = operationFailure("github", "promotion policy recheck failed", err)
		}
		byID[id] = current
		if err != nil {
			results := orderedDeliveryOperationResults(result.Operations, byID)
			report := operation.NewReport(results)
			result.Report = &report
			return result, apperror.WithReport(classifyGitHubError(err), report)
		}
	}
	report := operation.NewReport(orderedDeliveryOperationResults(result.Operations, byID))
	result.Report = &report
	return result, nil
}

func orderedDeliveryOperationResults(planned []DeliveryRecordOperation, values map[string]operation.Result) []operation.Result {
	results := make([]operation.Result, 0, len(planned))
	for _, item := range planned {
		results = append(results, values[item.ID])
	}
	return results
}

func findTrustedStagedWorkRetry(ctx context.Context, client DeliveryRecordGitHub, repo string, issueNumber int, after time.Time, retryID string) (*gh.IssueComment, error) {
	listing, err := client.ListIssueCommentsAfterContext(ctx, repo, issueNumber, after)
	if err != nil {
		return nil, err
	}
	if !listing.Complete {
		return nil, fmt.Errorf("staged-work retry lookup did not reach the checkpoint boundary")
	}
	stored := make([]delivery.StoredComment, 0, len(listing.Comments))
	for _, value := range listing.Comments {
		stored = append(stored, storedDeliveryComment(value))
	}
	match, err := delivery.FindStagedWorkRetry(stored, retryID)
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
	record, err := delivery.ParseStagedWorkComment(storedDeliveryComment(comment))
	if err != nil {
		return nil, err
	}
	if comment.NodeID != match.Comment.NodeID || record.Digest != match.Record.Digest || record.RetryID != retryID {
		return nil, fmt.Errorf("staged-work retry readback changed identity or content")
	}
	return &comment, nil
}

func deliveryPullRequestMatchesEvent(pr gh.PullRequest, event gh.PullRequestEvent) bool {
	return pr.Number == event.Number && pr.NodeID == event.NodeID && pr.BaseRef == event.BaseRef && pr.HeadRef == event.HeadRef &&
		pr.BaseSHA == event.BaseSHA && pr.HeadSHA == event.HeadSHA && pr.MergeRevision == event.MergeRevision &&
		strings.TrimSpace(pr.BaseRepositoryFullName) != "" && strings.EqualFold(pr.BaseRepositoryFullName, event.BaseRepositoryFullName) &&
		strings.TrimSpace(pr.HeadRepositoryFullName) != "" && strings.EqualFold(pr.HeadRepositoryFullName, event.HeadRepositoryFullName) &&
		pr.Merged && pr.MergedAt.Equal(event.MergedAt)
}

func isManagedWorkPullRequest(pr gh.PullRequest, cfg config.Config) bool {
	return policy.ClassifyPullRequestFlow(policy.PullRequest{Number: pr.Number, Title: pr.Title, Body: pr.Body, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, BaseRepositoryFullName: pr.BaseRepositoryFullName, HeadRepositoryFullName: pr.HeadRepositoryFullName}, cfg) == policy.PRFlowWork
}

func stagedWorkRecordedForPR(snapshot delivery.Snapshot, number int) bool {
	for _, record := range snapshot.StagedWork {
		if record.PullRequest.Number == number {
			return true
		}
	}
	return false
}

func uniqueSortedNumbers(values []int) []int {
	seen := map[int]struct{}{}
	result := []int{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}
