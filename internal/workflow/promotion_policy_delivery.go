package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

func sealPromotionPolicy(ctx context.Context, client PullRequestPolicyGitHub, repo string, cfg config.Config, event gh.PullRequestEvent, latest gh.PullRequest, writer delivery.WriterProvenance) (policy.PromotionFacts, error) {
	if cfg.Delivery == nil {
		return policy.PromotionFacts{}, apperror.New(apperror.Config, "managed promotion requires a reviewed delivery-ledger locator", "Run delivery bootstrap and pin its exact locator before evaluating promotions.")
	}
	if cfg.Delivery.Authority != config.DeliveryAuthoritySealed {
		return policy.PromotionFacts{}, apperror.New(apperror.Config, "managed promotion requires sealed delivery authority", "Complete and review delivery bootstrap, then change delivery.authority from shadow to sealed through normal repository review.")
	}
	if writer.Workflow != "PR Policy" || writer.RunID <= 0 {
		return policy.PromotionFacts{}, apperror.New(apperror.Usage, "promotion sealing requires the trusted PR Policy workflow", "Use the generated workflow so sealing shares the repository delivery concurrency group.")
	}
	locator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return policy.PromotionFacts{}, apperror.Wrap(apperror.Config, err.Error(), err, "Run and review delivery bootstrap before promotion.")
	}
	store, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return policy.PromotionFacts{}, classifyGitHubError(err)
	}
	integration, err := acquireBaseIntegrationFacts(ctx, client, repo, store.Snapshot, latest.BaseSHA, latest.HeadSHA)
	if err != nil {
		return policy.PromotionFacts{}, classifyGitHubError(err)
	}
	if integration.State != delivery.BaseIntegrated {
		return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion requires integrated base and staging revisions", "Complete the recommended reviewed base-to-staging synchronization, or repair the reported integration evidence, then update the promotion.")
	}
	open, err := client.ListOpenPullRequestsBoundedContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return policy.PromotionFacts{}, classifyGitHubError(err)
	}
	managed := managedOpenPromotions(open, cfg)
	invalidationTargets := append([]gh.PullRequest(nil), managed...)
	if active, found := activePromotionForInvalidation(store.Snapshot); found {
		invalidationTargets = append(invalidationTargets, active)
	}
	if !open.Complete {
		if err := rerequestConcurrentPromotionChecks(ctx, client, repo, invalidationTargets, event.Number); err != nil {
			return policy.PromotionFacts{}, classifyGitHubError(err)
		}
		return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion sealing requires a complete bounded open-promotion listing", "Repair the bounded listing, then update the promotion.")
	}
	if len(managed) != 1 || managed[0].Number != event.Number {
		if err := rerequestConcurrentPromotionChecks(ctx, client, repo, invalidationTargets, event.Number); err != nil {
			return policy.PromotionFacts{}, classifyGitHubError(err)
		}
		return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion sealing requires exactly one completely listed open managed promotion", "Close duplicate promotion PRs or repair the bounded listing, then update the remaining promotion.")
	}
	revision := delivery.PromotionRevision{
		RepositoryNodeID: locator.Repository.NodeID,
		PullRequest:      delivery.ResourceIdentity{Number: latest.Number, NodeID: latest.NodeID},
		BaseSHA:          latest.BaseSHA,
		HeadSHA:          latest.HeadSHA,
	}
	plan, err := delivery.PlanPromotionSeal(store.Snapshot, delivery.PromotionSealInput{Revision: revision, Writer: writer, SealedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return policy.PromotionFacts{}, apperror.Wrap(apperror.Policy, "delivery-ledger promotion plan is incomplete", err, "Run delivery-record --dry-run and repair every reported coverage or record gap.")
	}
	ownershipIssues, err := promotionWorkIssueReferences(plan.Work)
	if err != nil {
		return policy.PromotionFacts{}, apperror.Wrap(apperror.Policy, "delivery-ledger promotion ownership is inconsistent", err, "Repair or explicitly rebootstrap the conflicting staged-work records.")
	}
	if err := verifyPromotionOwnership(ctx, client, repo, cfg, ownershipIssues); err != nil {
		return policy.PromotionFacts{}, err
	}
	if !plan.Applicable {
		resolved, err := delivery.ResolveSealedPromotion(store.Snapshot, revision)
		if err != nil {
			return policy.PromotionFacts{}, apperror.Wrap(apperror.Policy, "active promotion seal is invalid", err, "Repair the delivery ledger before retrying policy.")
		}
		current, err := client.GetPullRequestContext(ctx, repo, latest.Number)
		if err != nil {
			return policy.PromotionFacts{}, classifyGitHubError(err)
		}
		if !sameManagedPolicyPullRequest(latest, current) {
			return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion revision changed after seal verification", "Update the promotion and rerun policy for its exact current base and head revisions.")
		}
		integration, err = acquireBaseIntegrationFacts(ctx, client, repo, store.Snapshot, current.BaseSHA, current.HeadSHA)
		if err != nil {
			return policy.PromotionFacts{}, classifyGitHubError(err)
		}
		if integration.State != delivery.BaseIntegrated {
			return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion integration changed after seal verification", "Complete the recommended reviewed base-to-staging synchronization, then update the promotion.")
		}
		return promotionFactsFromResolved(resolved, integration), nil
	}
	if plan.Record == nil {
		return policy.PromotionFacts{}, apperror.New(apperror.Policy, "promotion seal plan has no immutable record", "Repair the delivery ledger before retrying policy.")
	}
	results := []operation.Result{
		{ID: "promotion-plan-record", Resource: fmt.Sprintf("%s#%d", repo, locator.Issue.Number), Action: "append_record", Status: operation.StatusNotAttempted},
		{ID: "promotion-plan-checkpoint", Resource: fmt.Sprintf("%s#comment-%d", repo, locator.Checkpoint.DatabaseID), Action: "update_checkpoint", Status: operation.StatusNotAttempted},
		{ID: "promotion-plan-readback", Resource: fmt.Sprintf("%s#comment-%d", repo, locator.Checkpoint.DatabaseID), Action: "verify_seal", Status: operation.StatusNotAttempted},
	}
	fail := func(index int, message string, cause error) (policy.PromotionFacts, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, cause)
		report := operation.NewReport(results)
		return policy.PromotionFacts{}, apperror.WithReport(classifyGitHubError(cause), report)
	}
	body, err := delivery.RenderPromotionPlanRecord(*plan.Record)
	if err != nil {
		return policy.PromotionFacts{}, apperror.Wrap(apperror.Policy, "promotion plan could not be encoded", err, "Repair the delivery ledger before retrying policy.")
	}
	comment, err := findTrustedPromotionPlanRetry(ctx, client, repo, locator.Issue.Number, store.CheckpointComment.UpdatedAt, plan.Record.RetryID)
	if err != nil {
		return fail(0, "promotion plan retry lookup failed", err)
	}
	if comment == nil {
		created, createErr := client.CreateIssueCommentReturningContext(ctx, repo, locator.Issue.Number, body)
		if createErr != nil {
			comment, err = findTrustedPromotionPlanRetry(ctx, client, repo, locator.Issue.Number, store.CheckpointComment.UpdatedAt, plan.Record.RetryID)
			if err != nil {
				return fail(0, "promotion plan append was ambiguous", err)
			}
			if comment == nil {
				return fail(0, "promotion plan append failed", createErr)
			}
		} else {
			comment = &created
		}
		if err := validateTrustedDeliveryWrite(*comment, repo, locator.Issue.Number); err != nil {
			return fail(0, "promotion plan append was not trusted", err)
		}
		results[0].Status = operation.StatusApplied
	} else {
		results[0].Status = operation.StatusUnchanged
	}
	parsed, err := delivery.ParsePromotionPlanComment(storedDeliveryComment(*comment))
	if err != nil {
		return fail(0, "promotion plan append was not exact", err)
	}
	if parsed.Digest != plan.Record.Digest {
		plan, err = delivery.AdoptPromotionPlanRetry(plan, store.Snapshot.Checkpoint, parsed)
		if err != nil {
			return fail(0, "promotion plan append was not exact", err)
		}
	}
	fresh, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return fail(1, "promotion checkpoint preflight failed", err)
	}
	commit, err := delivery.FinalizePromotionSeal(plan, fresh.Snapshot.Checkpoint, delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID})
	if err != nil {
		return fail(1, "promotion seal plan became stale", err)
	}
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, locator.Checkpoint.DatabaseID, commit.CheckpointBody)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, locator.Checkpoint.DatabaseID)
		if readErr != nil || readback.Body != commit.CheckpointBody {
			return fail(1, "promotion checkpoint update failed", err)
		}
		updated = readback
	}
	if trustErr := validateTrustedDeliveryWrite(updated, repo, locator.Issue.Number); trustErr != nil || updated.ID != locator.Checkpoint.DatabaseID || updated.NodeID != locator.Checkpoint.NodeID || updated.Body != commit.CheckpointBody {
		if trustErr == nil {
			trustErr = fmt.Errorf("promotion checkpoint update returned stale content or identity")
		}
		return fail(1, "promotion checkpoint update was not trusted", trustErr)
	}
	results[1].Status = operation.StatusApplied
	sealed, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return fail(2, "promotion checkpoint readback failed", err)
	}
	resolved, err := delivery.ResolveSealedPromotion(sealed.Snapshot, revision)
	if err != nil {
		return fail(2, "promotion seal readback is invalid", err)
	}
	current, err := client.GetPullRequestContext(ctx, repo, latest.Number)
	if err != nil {
		return fail(2, "promotion revision verification failed", err)
	}
	if !sameManagedPolicyPullRequest(latest, current) {
		return fail(2, "promotion revision changed after sealing", fmt.Errorf("stale promotion base or head revision"))
	}
	integration, err = acquireBaseIntegrationFacts(ctx, client, repo, sealed.Snapshot, current.BaseSHA, current.HeadSHA)
	if err != nil {
		return fail(2, "promotion integration verification failed", err)
	}
	if integration.State != delivery.BaseIntegrated {
		return fail(2, "promotion integration changed after sealing", fmt.Errorf("base integration is %s", integration.State))
	}
	results[2].Status = operation.StatusUnchanged
	return promotionFactsFromResolved(resolved, integration), nil
}

func promotionWorkIssueReferences(work []delivery.PromotionWork) ([]delivery.ManagedIssueReference, error) {
	byNumber := map[int]delivery.ManagedIssueReference{}
	for _, item := range work {
		for _, reference := range item.Issues {
			if prior, duplicate := byNumber[reference.Number]; duplicate && prior != reference {
				return nil, fmt.Errorf("managed issue #%d has contradictory delivery identities", reference.Number)
			}
			byNumber[reference.Number] = reference
		}
	}
	result := make([]delivery.ManagedIssueReference, 0, len(byNumber))
	for _, reference := range byNumber {
		result = append(result, reference)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result, nil
}

func activePromotionForInvalidation(snapshot delivery.Snapshot) (gh.PullRequest, bool) {
	if snapshot.Checkpoint.ActivePlan == nil {
		return gh.PullRequest{}, false
	}
	for _, plan := range snapshot.PromotionPlans {
		if plan.Digest == snapshot.Checkpoint.ActivePlan.Digest {
			return gh.PullRequest{Number: plan.PullRequest.Number, NodeID: plan.PullRequest.NodeID, HeadSHA: plan.HeadSHA}, true
		}
	}
	return gh.PullRequest{}, false
}

func rerequestConcurrentPromotionChecks(ctx context.Context, client PullRequestPolicyGitHub, repo string, promotions []gh.PullRequest, current int) error {
	seen := map[int]struct{}{}
	failures := []error{}
	for _, promotion := range promotions {
		if promotion.Number == current {
			continue
		}
		if _, duplicate := seen[promotion.Number]; duplicate {
			continue
		}
		seen[promotion.Number] = struct{}{}
		rollup, err := client.GetCheckRollupContext(ctx, repo, promotion.Number, promotion.HeadSHA)
		if err != nil {
			failures = append(failures, fmt.Errorf("acquire concurrent promotion #%d checks: %w", promotion.Number, err))
			continue
		}
		if !rollup.Complete {
			failures = append(failures, fmt.Errorf("concurrent promotion #%d policy check acquisition is incomplete", promotion.Number))
			continue
		}
		matches := []gh.CheckState{}
		for _, check := range rollup.Checks {
			if check.Name == deliveryPolicyCheckName && check.ID > 0 {
				matches = append(matches, check)
			}
		}
		if len(matches) != 1 {
			failures = append(failures, fmt.Errorf("concurrent promotion #%d policy check identity is ambiguous or missing", promotion.Number))
			continue
		}
		if matches[0].Status == "completed" && matches[0].Conclusion == "success" {
			if err := client.RerequestCheckRunContext(ctx, repo, matches[0].ID); err != nil {
				failures = append(failures, fmt.Errorf("invalidate concurrent promotion #%d policy check: %w", promotion.Number, err))
			}
		}
	}
	return errors.Join(failures...)
}

func managedOpenPromotions(listing gh.PullRequestListing, cfg config.Config) []gh.PullRequest {
	result := []gh.PullRequest{}
	for _, pr := range listing.PullRequests {
		if classifyTransportPullRequest(pr, cfg) == policy.PRFlowPromotion {
			result = append(result, pr)
		}
	}
	return result
}

func verifyPromotionOwnership(ctx context.Context, client PullRequestPolicyGitHub, repo string, cfg config.Config, expected []delivery.ManagedIssueReference) error {
	for _, reference := range expected {
		issue, err := client.GetIssueContext(ctx, repo, reference.Number)
		if err != nil {
			return classifyGitHubError(err)
		}
		if issue.PullRequest || issue.NodeID != reference.NodeID {
			return apperror.New(apperror.Policy, fmt.Sprintf("delivery record for issue #%d has stale resource identity", reference.Number), "Repair or explicitly rebootstrap the delivery record.")
		}
		comments, err := client.ListIssueCommentsContext(ctx, repo, reference.Number)
		if err != nil {
			return classifyGitHubError(err)
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels, Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy})
		if !ownership.Managed || ownership.Source != policy.IssueOwnershipRecord || ownership.Record == nil || ownership.Record.Digest != reference.OwnershipDigest {
			return apperror.New(apperror.Policy, fmt.Sprintf("delivery record for issue #%d does not match durable managed ownership", reference.Number), "Repair the ownership record or explicitly rebootstrap delivery state.")
		}
	}
	return nil
}

func promotionFactsFromResolved(resolved delivery.ResolvedPromotion, integration delivery.BaseIntegrationFacts) policy.PromotionFacts {
	issues := make([]int, 0, len(resolved.ExpectedIssues))
	included, excluded := []int{}, []int{}
	for _, issue := range resolved.ExpectedIssues {
		issues = append(issues, issue.Number)
	}
	for _, work := range resolved.Work {
		if work.Excluded {
			excluded = append(excluded, work.PullRequest.Number)
		} else {
			included = append(included, work.PullRequest.Number)
		}
	}
	return policy.PromotionFacts{
		ExpectedIssues: issues, IncludedWorkPullRequests: uniqueSortedNumbers(included), ExcludedWorkPullRequests: uniqueSortedNumbers(excluded),
		Complete: true, Source: "sealedDeliveryPlan", PlanDigest: resolved.Plan.Digest,
		CursorDigest: resolved.Plan.CursorDigest, CoverageDigest: resolved.Plan.CoverageDigest,
		BaseIntegration: integration,
	}
}

type baseIntegrationComparisonReader interface {
	CompareCommitsContext(context.Context, string, string, string) (gh.CommitComparison, error)
}

func acquireBaseIntegrationFacts(ctx context.Context, client baseIntegrationComparisonReader, repo string, snapshot delivery.Snapshot, baseSHA, stagingSHA string) (delivery.BaseIntegrationFacts, error) {
	observation := delivery.BaseIntegrationObservation{BaseSHA: baseSHA, StagingSHA: stagingSHA, BaseRelation: delivery.RevisionUnknown, StagingRelation: delivery.RevisionUnknown}
	anchorBase, anchorStaging := snapshot.Checkpoint.GenesisBaseSHA, snapshot.Checkpoint.Coverage.StagingSHA
	if snapshot.Checkpoint.BaseIntegration != nil {
		for _, record := range snapshot.BaseIntegrations {
			if record.Digest == snapshot.Checkpoint.BaseIntegration.Digest {
				anchorBase, anchorStaging = record.BaseSHA, record.StagingSHA
				break
			}
		}
	}
	if anchorBase != "" {
		if anchorBase == baseSHA {
			observation.BaseRelation = delivery.RevisionIdentical
		} else {
			comparison, err := client.CompareCommitsContext(ctx, repo, anchorBase, baseSHA)
			if err != nil {
				return delivery.BaseIntegrationFacts{}, err
			}
			observation.BaseRelation = delivery.RelationFromComparison(comparison.Status)
		}
	}
	if anchorStaging != "" {
		if anchorStaging == stagingSHA {
			observation.StagingRelation = delivery.RevisionIdentical
		} else {
			comparison, err := client.CompareCommitsContext(ctx, repo, anchorStaging, stagingSHA)
			if err != nil {
				return delivery.BaseIntegrationFacts{}, err
			}
			observation.StagingRelation = delivery.RelationFromComparison(comparison.Status)
		}
	}
	return delivery.ClassifyBaseIntegration(snapshot, observation)
}

func findTrustedPromotionPlanRetry(ctx context.Context, client PullRequestPolicyGitHub, repo string, issueNumber int, after time.Time, retryID string) (*gh.IssueComment, error) {
	listing, err := client.ListIssueCommentsAfterContext(ctx, repo, issueNumber, after)
	if err != nil {
		return nil, err
	}
	if !listing.Complete {
		return nil, fmt.Errorf("promotion-plan retry lookup did not reach the checkpoint boundary")
	}
	stored := make([]delivery.StoredComment, 0, len(listing.Comments))
	byID := make(map[int64]gh.IssueComment, len(listing.Comments))
	for _, comment := range listing.Comments {
		stored = append(stored, storedDeliveryComment(comment))
		byID[comment.ID] = comment
	}
	match, err := delivery.FindPromotionPlanRetry(stored, retryID)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}
	comment := byID[match.Comment.DatabaseID]
	if comment.NodeID != match.Comment.NodeID || !strings.Contains(comment.Body, delivery.RecordMarkerV1) {
		return nil, fmt.Errorf("promotion-plan retry comment identity changed")
	}
	return &comment, nil
}
