package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

type DeliveryBootstrapInput struct {
	ConfigPath, Repository, EnvironmentRepo string
	Apply                                   bool
	ReviewedPlanID                          string
	GenesisPromotion                        int
	GenesisStagingSHA                       string
	GitHubAPIURL, GitHubToken, GHToken      string
	WorkflowName                            string
	RunID                                   int64
	Initialize                              bool
	LedgerIssue                             int
	LedgerID                                string
	ObservedAt                              string
}

type DeliveryBootstrapResult struct {
	delivery.BootstrapPlan
	Initialization *DeliveryBootstrapInitialization `json:"-"`
	Rechecks       []DeliveryBootstrapRecheck       `json:"promotionRechecks"`
	Operations     []DeliveryRecordOperation        `json:"operations"`
	Report         *operation.Report                `json:"report,omitempty"`
}

type DeliveryBootstrapRecheck struct {
	AfterStagedPullRequest int                      `json:"afterStagedPullRequest"`
	Recheck                DeliveryPromotionRecheck `json:"recheck"`
}

type DeliveryBootstrapInitialization struct {
	SchemaVersion     int                               `json:"schemaVersion"`
	Kind              string                            `json:"kind"`
	PlanID            string                            `json:"planId"`
	Applicable        bool                              `json:"applicable"`
	Repository        delivery.RepositoryIdentity       `json:"repository"`
	Issue             delivery.ResourceIdentity         `json:"issue"`
	GenesisBoundary   delivery.BootstrapGenesisBoundary `json:"genesisBoundary"`
	GenesisCheckpoint delivery.DeliveryCheckpoint       `json:"genesisCheckpoint"`
	CheckpointBody    string                            `json:"checkpointBody"`
	Locator           *delivery.DeliveryStoreLocator    `json:"locator,omitempty"`
	Rollover          *delivery.RolloverPlan            `json:"rollover,omitempty"`
	Warnings          []string                          `json:"warnings"`
}

func (result DeliveryBootstrapResult) MarshalJSON() ([]byte, error) {
	if result.Initialization == nil {
		type alias DeliveryBootstrapResult
		return json.Marshal(alias(result))
	}
	return json.Marshal(struct {
		*DeliveryBootstrapInitialization
		Rechecks   []DeliveryBootstrapRecheck `json:"promotionRechecks"`
		Operations []DeliveryRecordOperation  `json:"operations"`
		Report     *operation.Report          `json:"report,omitempty"`
	}{DeliveryBootstrapInitialization: result.Initialization, Rechecks: result.Rechecks, Operations: result.Operations, Report: result.Report})
}

type DeliveryBootstrapGitHub interface {
	DeliveryRecordGitHub
	ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error)
}

type DeliveryBootstrapWorkflow struct {
	newClient func(context.Context, DeliveryBootstrapInput) (DeliveryBootstrapGitHub, error)
}

const bootstrapGenesisBoundaryOperationID = "delivery-genesis-boundary"

func NewDeliveryBootstrapWorkflow() DeliveryBootstrapWorkflow {
	return DeliveryBootstrapWorkflow{newClient: func(ctx context.Context, input DeliveryBootstrapInput) (DeliveryBootstrapGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow DeliveryBootstrapWorkflow) RunContext(ctx context.Context, input DeliveryBootstrapInput) (DeliveryBootstrapResult, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	cfg, err := loadWorkflowConfig(input.ConfigPath)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	if input.RunID <= 0 || strings.TrimSpace(input.WorkflowName) != "Delivery Recorder" {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Usage, "delivery bootstrap requires the trusted Delivery Recorder workflow", "Dispatch the generated workflow so planning and apply share one run and delivery concurrency group.")
	}
	if input.Initialize {
		return workflow.runInitialization(ctx, input, cfg)
	}
	if cfg.Delivery == nil || cfg.Delivery.Authority != config.DeliveryAuthorityShadow {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Config, "delivery bootstrap migration requires shadow authority", "Pin the initialized locator with delivery.authority: shadow; change it to sealed only after bootstrap is complete and reviewed.")
	}
	locator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return DeliveryBootstrapResult{}, apperror.Wrap(apperror.Config, err.Error(), err, "Pin the reviewed ledger issue and checkpoint identities in repository policy.")
	}
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
		repositoryIdentityInput{source: "delivery.repository.full_name", value: locator.Repository.FullName},
	)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	store, err := acquireDeliveryStore(ctx, client, repo, locator)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	planInput, operations, rechecks, err := acquireBootstrapPlanInput(ctx, client, repo, cfg, store, input)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	plan, err := delivery.PlanBootstrap(planInput)
	if err != nil {
		return DeliveryBootstrapResult{}, apperror.Wrap(apperror.Policy, "delivery bootstrap could not be planned", err, "Review the reported repository facts and genesis boundary.")
	}
	if plan.GenesisCheckpoint.Digest != store.Snapshot.Checkpoint.Digest {
		operations = append([]DeliveryRecordOperation{{
			ID: bootstrapGenesisBoundaryOperationID, Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "update_genesis_checkpoint",
		}}, operations...)
	}
	plan.PlanID = deliveryBootstrapExecutionDigest(plan, operations, rechecks)
	result := DeliveryBootstrapResult{BootstrapPlan: plan, Rechecks: rechecks, Operations: operations}
	if !input.Apply {
		return result, nil
	}
	if strings.TrimSpace(input.ReviewedPlanID) != plan.PlanID {
		return result, apperror.New(apperror.Policy, "delivery bootstrap plan changed after review", "Run --dry-run again and pass its exact planId.")
	}
	if !plan.Applicable {
		return result, apperror.New(apperror.Policy, "delivery bootstrap plan has unresolved ambiguity", "Resolve every listed ambiguity before apply.")
	}
	return applyDeliveryBootstrap(ctx, client, repo, cfg, store, result)
}

func (workflow DeliveryBootstrapWorkflow) runInitialization(ctx context.Context, input DeliveryBootstrapInput, cfg config.Config) (DeliveryBootstrapResult, error) {
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
	)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	if cfg.Delivery != nil {
		return workflow.runRolloverInitialization(ctx, client, repo, input, cfg)
	}
	repository, err := client.GetRepositoryIdentityContext(ctx, repo)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	issue, err := client.GetIssueContext(ctx, repo, input.LedgerIssue)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	if issue.PullRequest || !issue.Locked || issue.NodeID == "" {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Policy, "delivery ledger issue must be an existing locked issue", "Create and lock the reserved delivery-state issue, then retry.")
	}
	comments, err := client.ListNewestIssueCommentsContext(ctx, repo, issue.Number)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	warnings := []string{}
	applicable := comments.Complete
	if !comments.Complete {
		warnings = append(warnings, "ledger comment discovery reached the newest-100 cap; initialization cannot prove a checkpoint is absent")
	}
	for _, comment := range comments.Comments {
		if strings.Contains(comment.Body, delivery.CheckpointMarkerV1) {
			applicable = false
			warnings = append(warnings, fmt.Sprintf("comment %d already contains a delivery checkpoint marker", comment.ID))
		}
	}
	repositoryIdentity := delivery.RepositoryIdentity{Host: repository.Host, FullName: repository.FullName, NodeID: repository.NodeID}
	issueIdentity := delivery.ResourceIdentity{Number: issue.Number, NodeID: issue.NodeID}
	base, err := client.GetBranchContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	comparison, err := client.CompareCommitsContext(ctx, repo, base.SHA, input.GenesisStagingSHA)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	acknowledgedBaseSHA := comparison.MergeBaseSHA
	if comparison.Status == "ahead" || comparison.Status == "identical" {
		acknowledgedBaseSHA = base.SHA
	}
	checkpoint, err := delivery.NewGenesisCheckpoint(delivery.GenesisCheckpointInput{
		LedgerID: input.LedgerID, Repository: repositoryIdentity, Issue: issueIdentity, StagingSHA: input.GenesisStagingSHA, BaseSHA: acknowledgedBaseSHA,
		Writer: delivery.WriterProvenance{Workflow: input.WorkflowName, RunID: input.RunID}, ObservedAt: input.ObservedAt,
	})
	if err != nil {
		return DeliveryBootstrapResult{}, apperror.Wrap(apperror.Usage, "delivery genesis is invalid", err, "Pass a full 40-character staging revision and stable ledger identity.")
	}
	temporaryLocator := delivery.DeliveryStoreLocator{Repository: repositoryIdentity, Issue: issueIdentity, Checkpoint: delivery.CommentIdentity{DatabaseID: 1, NodeID: "pending-checkpoint"}}
	body, err := delivery.RenderCheckpointIndex(temporaryLocator, checkpoint)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	initialization := &DeliveryBootstrapInitialization{
		SchemaVersion: 1, Kind: "deliveryBootstrapInitialization", Applicable: applicable,
		Repository: repositoryIdentity, Issue: issueIdentity,
		GenesisBoundary:   delivery.BootstrapGenesisBoundary{Explicit: true, StagingSHA: input.GenesisStagingSHA, BaseSHA: acknowledgedBaseSHA, Rationale: "operator-reviewed initialization boundary"},
		GenesisCheckpoint: checkpoint, CheckpointBody: body, Warnings: warnings,
	}
	initialization.PlanID = deliveryBootstrapInitializationDigest(*initialization)
	result := DeliveryBootstrapResult{
		BootstrapPlan: delivery.BootstrapPlan{PlanID: initialization.PlanID, Ambiguities: []delivery.BootstrapAmbiguity{}}, Initialization: initialization,
		Operations: []DeliveryRecordOperation{{ID: "delivery-genesis-checkpoint", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: "create_checkpoint"}},
	}
	if !input.Apply {
		return result, nil
	}
	if input.ReviewedPlanID != initialization.PlanID {
		return result, apperror.New(apperror.Policy, "delivery initialization plan changed after review", "Run --dry-run again and pass its exact planId.")
	}
	if !initialization.Applicable {
		return result, apperror.New(apperror.Policy, "delivery initialization is incomplete", "Resolve the reported checkpoint discovery ambiguity before apply.")
	}
	latestIssue, err := client.GetIssueContext(ctx, repo, issue.Number)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	if latestIssue.NodeID != issue.NodeID || !latestIssue.Locked {
		return result, apperror.New(apperror.GitHub, "delivery initialization plan is stale", "Re-run dry-run against the current ledger issue.")
	}
	latestComments, err := client.ListNewestIssueCommentsContext(ctx, repo, issue.Number)
	if err != nil {
		return result, classifyGitHubError(err)
	}
	if !latestComments.Complete {
		return result, apperror.New(apperror.GitHub, "delivery initialization checkpoint preflight is incomplete", "Re-run after the ledger comment set can be acquired completely.")
	}
	for _, comment := range latestComments.Comments {
		if strings.Contains(comment.Body, delivery.CheckpointMarkerV1) {
			return result, apperror.New(apperror.GitHub, "delivery initialization checkpoint appeared after planning", "Re-run dry-run against the existing checkpoint.")
		}
	}
	created, err := client.CreateIssueCommentReturningContext(ctx, repo, issue.Number, body)
	op := operation.Result{ID: "delivery-genesis-checkpoint", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: "create_checkpoint", Status: operation.StatusApplied}
	if err != nil {
		op.Status = operation.StatusFailed
		op.Error = operationFailure("github", "delivery genesis checkpoint creation failed", err)
		report := operation.NewReport([]operation.Result{op})
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	if err := validateTrustedDeliveryWrite(created, repo, issue.Number); err != nil || created.Body != body {
		if err == nil {
			err = fmt.Errorf("delivery genesis checkpoint returned stale content")
		}
		op.Status = operation.StatusFailed
		op.Error = operationFailure("github", "delivery genesis checkpoint was not trusted", err)
		report := operation.NewReport([]operation.Result{op})
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	initialization.Locator = &delivery.DeliveryStoreLocator{Repository: repositoryIdentity, Issue: issueIdentity, Checkpoint: delivery.CommentIdentity{DatabaseID: created.ID, NodeID: created.NodeID}}
	report := operation.NewReport([]operation.Result{op})
	result.Report = &report
	return result, nil
}

func deliveryBootstrapInitializationDigest(value DeliveryBootstrapInitialization) string {
	value.PlanID = ""
	if value.Rollover == nil {
		value.Locator = nil
	}
	content, _ := json.Marshal(value)
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (workflow DeliveryBootstrapWorkflow) runRolloverInitialization(ctx context.Context, client DeliveryBootstrapGitHub, repo string, input DeliveryBootstrapInput, cfg config.Config) (DeliveryBootstrapResult, error) {
	oldLocator, err := deliveryLocatorFromConfig(cfg)
	if err != nil {
		return DeliveryBootstrapResult{}, apperror.Wrap(apperror.Config, "delivery rollover predecessor is invalid", err, "Repair the pinned delivery locator before retrying.")
	}
	oldStore, err := acquireDeliveryStore(ctx, client, repo, oldLocator)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	if strings.TrimSpace(input.LedgerID) != oldStore.Snapshot.Checkpoint.LedgerID {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Usage, "delivery rollover must preserve the ledger identity", "Pass the ledgerId from the pinned predecessor checkpoint.")
	}
	if strings.TrimSpace(input.GenesisStagingSHA) != oldStore.Snapshot.Checkpoint.Coverage.StagingSHA {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Usage, "delivery rollover staging boundary must match committed coverage", "Pass the exact coverage.stagingSha from the pinned predecessor checkpoint.")
	}
	issue, err := client.GetIssueContext(ctx, repo, input.LedgerIssue)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	if issue.PullRequest || !issue.Locked || issue.NodeID == "" || issue.Number == oldStore.LedgerIssue.Number {
		return DeliveryBootstrapResult{}, apperror.New(apperror.Policy, "delivery successor must be a different existing locked issue", "Create and lock a new reserved delivery-state issue, then retry.")
	}
	rollover, err := delivery.PlanRollover(oldStore.Snapshot, delivery.RolloverInput{
		Issue:  delivery.ResourceIdentity{Number: issue.Number, NodeID: issue.NodeID},
		Writer: delivery.WriterProvenance{Workflow: input.WorkflowName, RunID: input.RunID}, ObservedAt: input.ObservedAt,
	})
	if err != nil {
		return DeliveryBootstrapResult{}, apperror.Wrap(apperror.Policy, "delivery rollover could not be planned", err, "Drain the active delivery window and pending rechecks before rollover.")
	}
	temporaryLocator := delivery.DeliveryStoreLocator{Repository: oldLocator.Repository, Issue: rollover.Checkpoint.Issue, Checkpoint: delivery.CommentIdentity{DatabaseID: 1, NodeID: "pending-checkpoint"}}
	body, err := delivery.RenderCheckpointIndex(temporaryLocator, rollover.Checkpoint)
	if err != nil {
		return DeliveryBootstrapResult{}, err
	}
	comments, err := client.ListNewestIssueCommentsContext(ctx, repo, issue.Number)
	if err != nil {
		return DeliveryBootstrapResult{}, classifyGitHubError(err)
	}
	warnings := []string{}
	applicable := comments.Complete
	if !comments.Complete {
		warnings = append(warnings, "successor checkpoint discovery reached the newest-100 cap")
	}
	var existing *delivery.DeliveryStoreLocator
	checkpointMarkers := 0
	for _, comment := range comments.Comments {
		if !strings.Contains(comment.Body, delivery.CheckpointMarkerV1) {
			continue
		}
		checkpointMarkers++
		locator := delivery.DeliveryStoreLocator{Repository: oldLocator.Repository, Issue: rollover.Checkpoint.Issue, Checkpoint: delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID}}
		parsed, parseErr := delivery.ParseCheckpointIndex(locator, storedDeliveryComment(comment))
		if parseErr == nil && parsed.Digest == rollover.Checkpoint.Digest && validateTrustedDeliveryWrite(comment, repo, issue.Number) == nil {
			candidate := locator
			existing = &candidate
		}
	}
	if checkpointMarkers > 0 && existing == nil {
		applicable = false
		warnings = append(warnings, "successor issue already contains a different delivery checkpoint")
	}
	if checkpointMarkers > 1 {
		applicable = false
		warnings = append(warnings, "successor issue contains multiple delivery checkpoints")
	}
	initialization := &DeliveryBootstrapInitialization{
		SchemaVersion: 2, Kind: "deliveryBootstrapInitialization", Applicable: applicable,
		Repository: oldLocator.Repository, Issue: rollover.Checkpoint.Issue,
		GenesisBoundary:   delivery.BootstrapGenesisBoundary{Explicit: true, StagingSHA: rollover.Checkpoint.Coverage.StagingSHA, BaseSHA: rollover.Checkpoint.GenesisBaseSHA, Rationale: "operator-reviewed drained-ledger rollover"},
		GenesisCheckpoint: rollover.Checkpoint, CheckpointBody: body, Locator: existing, Rollover: &rollover, Warnings: warnings,
	}
	initialization.PlanID = deliveryBootstrapInitializationDigest(*initialization)
	createAction := "create_successor_checkpoint"
	if existing != nil {
		createAction = "adopt_successor_checkpoint"
	}
	result := DeliveryBootstrapResult{
		BootstrapPlan: delivery.BootstrapPlan{PlanID: initialization.PlanID, Ambiguities: []delivery.BootstrapAmbiguity{}}, Initialization: initialization,
		Operations: []DeliveryRecordOperation{
			{ID: "delivery-successor-checkpoint", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: createAction},
			{ID: "delivery-predecessor-freeze", Resource: fmt.Sprintf("%s#comment-%d", repo, oldStore.CheckpointComment.ID), Action: "freeze_predecessor"},
		},
	}
	if !input.Apply {
		return result, nil
	}
	if input.ReviewedPlanID != initialization.PlanID {
		return result, apperror.New(apperror.Policy, "delivery rollover plan changed after review", "Run --dry-run again and pass its exact planId.")
	}
	if !initialization.Applicable {
		return result, apperror.New(apperror.Policy, "delivery rollover is incomplete", "Resolve the reported successor checkpoint ambiguity before apply.")
	}
	return applyDeliveryRollover(ctx, client, repo, oldStore, result)
}

func applyDeliveryRollover(ctx context.Context, client DeliveryBootstrapGitHub, repo string, oldStore acquiredDeliveryStore, result DeliveryBootstrapResult) (DeliveryBootstrapResult, error) {
	initialization := result.Initialization
	results := make([]operation.Result, len(result.Operations))
	for index, planned := range result.Operations {
		results[index] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	}
	fail := func(index int, message string, cause error) (DeliveryBootstrapResult, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, cause)
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(cause), report)
	}
	if err := verifyRolloverPredecessor(ctx, client, repo, oldStore, *initialization.Rollover); err != nil {
		return fail(0, "rollover predecessor preflight failed", err)
	}
	issue, err := client.GetIssueContext(ctx, repo, initialization.Issue.Number)
	if err != nil || issue.PullRequest || !issue.Locked || issue.NodeID != initialization.Issue.NodeID {
		if err == nil {
			err = fmt.Errorf("successor ledger issue changed after planning")
		}
		return fail(0, "successor ledger issue preflight failed", err)
	}
	var successor delivery.DeliveryStoreLocator
	if initialization.Locator != nil {
		successor = *initialization.Locator
		comment, err := client.GetIssueCommentContext(ctx, repo, successor.Checkpoint.DatabaseID)
		if err != nil || comment.Body != initialization.CheckpointBody || validateTrustedDeliveryWrite(comment, repo, successor.Issue.Number) != nil {
			if err == nil {
				err = fmt.Errorf("reviewed successor checkpoint changed before apply")
			}
			return fail(0, "successor checkpoint adoption failed", err)
		}
		results[0].Status = operation.StatusUnchanged
	} else {
		created, err := client.CreateIssueCommentReturningContext(ctx, repo, initialization.Issue.Number, initialization.CheckpointBody)
		if err != nil {
			return fail(0, "successor checkpoint creation failed", err)
		}
		if trustErr := validateTrustedDeliveryWrite(created, repo, initialization.Issue.Number); trustErr != nil || created.Body != initialization.CheckpointBody {
			if trustErr == nil {
				trustErr = fmt.Errorf("successor checkpoint creation returned stale content")
			}
			return fail(0, "successor checkpoint creation was not trusted", trustErr)
		}
		successor = delivery.DeliveryStoreLocator{Repository: initialization.Repository, Issue: initialization.Issue, Checkpoint: delivery.CommentIdentity{DatabaseID: created.ID, NodeID: created.NodeID}}
		results[0].Status = operation.StatusApplied
	}
	if err := verifyRolloverPredecessor(ctx, client, repo, oldStore, *initialization.Rollover); err != nil {
		return fail(1, "rollover predecessor changed before freeze", err)
	}
	frozen, body, err := delivery.FinalizeRollover(oldStore.Snapshot, *initialization.Rollover, successor)
	if err != nil {
		return fail(1, "predecessor freeze planning failed", err)
	}
	updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, oldStore.CheckpointComment.ID, body)
	if err != nil {
		readback, readErr := client.GetIssueCommentContext(ctx, repo, oldStore.CheckpointComment.ID)
		if readErr != nil || readback.Body != body {
			return fail(1, "predecessor checkpoint freeze failed", err)
		}
		updated = readback
	}
	if trustErr := validateTrustedDeliveryWrite(updated, repo, oldStore.LedgerIssue.Number); trustErr != nil || updated.ID != oldStore.CheckpointComment.ID || updated.NodeID != oldStore.CheckpointComment.NodeID || updated.Body != body {
		if trustErr == nil {
			trustErr = fmt.Errorf("predecessor checkpoint freeze returned stale content")
		}
		return fail(1, "predecessor checkpoint freeze was not trusted", trustErr)
	}
	if frozen.Successor == nil || frozen.Successor.Locator != successor {
		return fail(1, "predecessor checkpoint freeze was not exact", fmt.Errorf("successor link is missing"))
	}
	results[1].Status = operation.StatusApplied
	initialization.Locator = &successor
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func verifyRolloverPredecessor(ctx context.Context, client DeliveryBootstrapGitHub, repo string, store acquiredDeliveryStore, plan delivery.RolloverPlan) error {
	comment, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
	if err != nil {
		return err
	}
	if err := validateTrustedDeliveryWrite(comment, repo, store.LedgerIssue.Number); err != nil || comment.ID != store.CheckpointComment.ID || comment.NodeID != store.CheckpointComment.NodeID {
		if err == nil {
			err = fmt.Errorf("predecessor checkpoint identity changed")
		}
		return err
	}
	checkpoint, err := delivery.ParseCheckpointIndex(store.Snapshot.Locator, storedDeliveryComment(comment))
	if err != nil {
		return err
	}
	if checkpoint.Digest != plan.Precondition.Digest || checkpoint.Generation != plan.Precondition.Generation {
		return fmt.Errorf("predecessor checkpoint changed after planning")
	}
	return nil
}

func deliveryBootstrapExecutionDigest(plan delivery.BootstrapPlan, operations []DeliveryRecordOperation, rechecks []DeliveryBootstrapRecheck) string {
	plan.PlanID = ""
	content, _ := json.Marshal(struct {
		Plan       delivery.BootstrapPlan
		Operations []DeliveryRecordOperation
		Rechecks   []DeliveryBootstrapRecheck
	}{Plan: plan, Operations: operations, Rechecks: rechecks})
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func acquireBootstrapPlanInput(ctx context.Context, client DeliveryBootstrapGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, input DeliveryBootstrapInput) (delivery.BootstrapPlanInput, []DeliveryRecordOperation, []DeliveryBootstrapRecheck, error) {
	checkpoint := store.Snapshot.Checkpoint
	facts := []delivery.BootstrapSourceFact{}
	relationships := []delivery.BootstrapRelationship{}
	ambiguities := []delivery.BootstrapAmbiguity{}
	ownershipPlans := []delivery.BootstrapOwnershipRecord{}
	operations := []DeliveryRecordOperation{}
	writer := delivery.WriterProvenance{Workflow: strings.TrimSpace(input.WorkflowName), RunID: input.RunID}

	issues, err := client.ListOpenIssuesContext(ctx, repo)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	managed := map[int]delivery.ManagedIssueReference{}
	awaiting := map[int]bool{}
	for _, issue := range issues {
		comments, err := client.ListIssueCommentsContext(ctx, repo, issue.Number)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels, Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy})
		if !ownership.Managed {
			continue
		}
		factID := fmt.Sprintf("issue-%d", issue.Number)
		facts = append(facts, bootstrapFact(factID, "managedIssue", "githubIssue", map[string]string{
			"number": fmt.Sprint(issue.Number), "nodeId": issue.NodeID, "state": issue.State, "ownershipSource": string(ownership.Source),
		}))
		var record policy.ManagedIssueRecord
		if ownership.Source == policy.IssueOwnershipRecord {
			record = *ownership.Record
		} else if ownership.Source == policy.IssueOwnershipLegacyFingerprint {
			record = policy.NewManagedIssueRecord(issue.NodeID, issue.Number)
			body := policy.RenderManagedIssueRecord(record)
			ownershipPlans = append(ownershipPlans, delivery.BootstrapOwnershipRecord{Issue: delivery.ResourceIdentity{Number: issue.Number, NodeID: issue.NodeID}, Digest: record.Digest, Body: body, SourceFactIDs: []string{factID}})
			resource := fmt.Sprintf("%s#%d", repo, issue.Number)
			operations = append(operations,
				DeliveryRecordOperation{ID: fmt.Sprintf("issue-%d-ownership-record", issue.Number), Resource: resource, Action: "create_ownership_record"},
				DeliveryRecordOperation{ID: fmt.Sprintf("issue-%d-ownership-index", issue.Number), Resource: resource, Action: "add_ownership_index"},
			)
		} else {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("issue-%d-ownership", issue.Number), Message: fmt.Sprintf("managed issue #%d has invalid or unavailable ownership", issue.Number), FactIDs: []string{factID}})
			continue
		}
		managed[issue.Number] = delivery.ManagedIssueReference{Number: issue.Number, NodeID: issue.NodeID, OwnershipDigest: record.Digest}
		awaiting[issue.Number] = containsLabel(issue.Labels, cfg.IssuePolicy.AwaitingReviewLabel)
	}

	closed, err := client.ListClosedPullRequestsBoundedContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	if !closed.Complete {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "staging-history-cap", Message: "closed staging history reached the 100-item bootstrap cap"})
	}
	closedPromotions, err := client.ListClosedPullRequestsBoundedContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	if !closedPromotions.Complete {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "promotion-history-cap", Message: "closed promotion history reached the 100-item bootstrap cap"})
	}
	mergedPromotions := []gh.PullRequest{}
	for _, candidate := range closedPromotions.PullRequests {
		if candidate.Merged && policy.ClassifyPullRequestFlow(policy.PullRequest{Number: candidate.Number, Title: candidate.Title, Body: candidate.Body, BaseRef: candidate.BaseRef, HeadRef: candidate.HeadRef, BaseRepositoryFullName: candidate.BaseRepositoryFullName, HeadRepositoryFullName: candidate.HeadRepositoryFullName}, cfg) == policy.PRFlowPromotion {
			mergedPromotions = append(mergedPromotions, candidate)
		}
	}
	sort.Slice(mergedPromotions, func(i, j int) bool {
		if mergedPromotions[i].MergedAt.Equal(mergedPromotions[j].MergedAt) {
			return mergedPromotions[i].Number > mergedPromotions[j].Number
		}
		return mergedPromotions[i].MergedAt.After(mergedPromotions[j].MergedAt)
	})
	if len(mergedPromotions) > 0 {
		last := mergedPromotions[0]
		facts = append(facts, bootstrapFact(fmt.Sprintf("last-promotion-%d", last.Number), "lastAcknowledgedPromotionCandidate", "githubPullRequestHistory", map[string]string{
			"number": fmt.Sprint(last.Number), "headSha": last.HeadSHA, "mergedAt": last.MergedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}))
	}

	boundarySHA := checkpoint.Coverage.StagingSHA
	boundaryBaseSHA := checkpoint.GenesisBaseSHA
	boundaryFacts := []string{}
	explicit := false
	if input.GenesisPromotion > 0 {
		promotion, err := client.GetPullRequestContext(ctx, repo, input.GenesisPromotion)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		factID := fmt.Sprintf("promotion-%d", promotion.Number)
		facts = append(facts, bootstrapFact(factID, "acknowledgedPromotion", "githubPullRequest", map[string]string{"number": fmt.Sprint(promotion.Number), "headSha": promotion.HeadSHA, "merged": fmt.Sprint(promotion.Merged)}))
		boundaryFacts = append(boundaryFacts, factID)
		if !promotion.Merged || policy.ClassifyPullRequestFlow(policy.PullRequest{Number: promotion.Number, Title: promotion.Title, Body: promotion.Body, BaseRef: promotion.BaseRef, HeadRef: promotion.HeadRef, BaseRepositoryFullName: promotion.BaseRepositoryFullName, HeadRepositoryFullName: promotion.HeadRepositoryFullName}, cfg) != policy.PRFlowPromotion {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "invalid-genesis-promotion", Message: "selected genesis promotion is not a merged managed promotion", FactIDs: []string{factID}})
		} else {
			boundarySHA, boundaryBaseSHA, explicit = promotion.HeadSHA, promotion.MergeRevision, true
		}
	} else if strings.TrimSpace(input.GenesisStagingSHA) != "" {
		boundarySHA, explicit = strings.TrimSpace(input.GenesisStagingSHA), true
		fact := bootstrapFact("explicit-staging-boundary", "genesisBoundary", "operatorInput", map[string]string{"stagingSha": boundarySHA})
		facts, boundaryFacts = append(facts, fact), []string{fact.ID}
	} else if len(closed.PullRequests) > 0 || len(mergedPromotions) > 0 {
		factIDs := []string{}
		if len(mergedPromotions) > 0 {
			factIDs = append(factIDs, fmt.Sprintf("last-promotion-%d", mergedPromotions[0].Number))
		}
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "genesis-history-ambiguous", Message: "existing staging/promotion history requires an explicit reviewed genesis boundary", FactIDs: factIDs, RequiresGenesisBoundary: true})
	}
	if boundarySHA != checkpoint.Coverage.StagingSHA {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "checkpoint-boundary-mismatch", Message: "selected genesis boundary does not match the pinned checkpoint coverage", FactIDs: boundaryFacts})
	}
	if checkpoint.Generation != 1 || checkpoint.HeadSequence != 0 || len(checkpoint.ActiveRecords) != 0 {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "ledger-not-genesis", Message: "bootstrap requires a fresh genesis checkpoint; use delivery-record for reconciliation"})
	}
	staging, err := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	facts = append(facts, bootstrapFact("current-staging-head", "currentStagingHead", "githubBranch", map[string]string{
		"branch": cfg.Repository.StagingBranch, "sha": staging.SHA,
	}))
	base, err := client.GetBranchContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	if boundaryBaseSHA == "" {
		comparison, compareErr := client.CompareCommitsContext(ctx, repo, base.SHA, boundarySHA)
		if compareErr != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(compareErr)
		}
		boundaryBaseSHA = comparison.MergeBaseSHA
		if comparison.Status == "ahead" || comparison.Status == "identical" {
			boundaryBaseSHA = base.SHA
		}
	}
	facts = append(facts, bootstrapFact("acknowledged-base-boundary", "acknowledgedBaseBoundary", "reviewedGitHubAncestry", map[string]string{
		"branch": cfg.Repository.BaseBranch, "sha": boundaryBaseSHA,
	}))

	workPlans := []delivery.BootstrapStagedWork{}
	workDistances := map[int]int{}
	issueMatches := map[int][]string{}
	for _, pr := range closed.PullRequests {
		if !pr.Merged || !isManagedWorkPullRequest(pr, cfg) || pr.MergeRevision == "" {
			continue
		}
		factID := fmt.Sprintf("work-pr-%d", pr.Number)
		facts = append(facts, bootstrapFact(factID, "mergedStagingWork", "githubPullRequest", map[string]string{
			"number": fmt.Sprint(pr.Number), "nodeId": pr.NodeID, "baseSha": pr.BaseSHA, "headSha": pr.HeadSHA, "mergeRevision": pr.MergeRevision, "mergedAt": pr.MergedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}))
		comparison, err := client.CompareCommitsContext(ctx, repo, boundarySHA, pr.MergeRevision)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		switch comparison.Status {
		case "ahead":
			workDistances[pr.Number] = comparison.AheadBy
		case "behind", "identical":
			continue
		default:
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-boundary", pr.Number), Message: fmt.Sprintf("merged work PR #%d diverges from the reviewed staging boundary", pr.Number), FactIDs: []string{factID}})
			continue
		}
		contained, err := client.CompareCommitsContext(ctx, repo, pr.MergeRevision, staging.SHA)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		if contained.Status != "ahead" && contained.Status != "identical" {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-current-staging", pr.Number), Message: fmt.Sprintf("merged work PR #%d is not contained in the current staging head", pr.Number), FactIDs: []string{factID, "current-staging-head"}})
			continue
		}
		refs := uniqueSortedNumbers(append(policy.ExtractReferenceIssueNumbersForPolicy(pr.Title, cfg.PRPolicy.RequiredReferenceKeyword), policy.ExtractReferenceIssueNumbersForPolicy(pr.Body, cfg.PRPolicy.RequiredReferenceKeyword)...))
		managedRefs := []delivery.ManagedIssueReference{}
		sourceIDs := []string{factID}
		for _, number := range refs {
			issueRef, exists := managed[number]
			if !exists {
				ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-issue-%d", pr.Number, number), Message: fmt.Sprintf("merged work PR #%d references issue #%d without durable managed ownership", pr.Number, number), FactIDs: []string{factID}})
				continue
			}
			managedRefs = append(managedRefs, issueRef)
			issueFact := fmt.Sprintf("issue-%d", number)
			sourceIDs = append(sourceIDs, issueFact)
			relationships = append(relationships, delivery.BootstrapRelationship{Kind: "stagesManagedIssue", FromFactID: factID, ToFactID: issueFact, EvidenceFactIDs: []string{}})
			issueMatches[number] = append(issueMatches[number], factID)
		}
		if len(managedRefs) == 0 {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-no-managed-issue", pr.Number), Message: fmt.Sprintf("merged work PR #%d has no unambiguous managed issue relationship", pr.Number), FactIDs: []string{factID}})
			continue
		}
		workPlans = append(workPlans, delivery.BootstrapStagedWork{SourceFactIDs: sourceIDs, Input: delivery.StagedWorkAppendInput{
			PullRequest: delivery.ResourceIdentity{Number: pr.Number, NodeID: pr.NodeID}, StagingBranch: cfg.Repository.StagingBranch,
			BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA, MergeRevision: pr.MergeRevision, MergedAt: pr.MergedAt.UTC().Format("2006-01-02T15:04:05Z"), Issues: managedRefs, Writer: writer,
		}})
	}
	for number, isAwaiting := range awaiting {
		if !isAwaiting {
			continue
		}
		matches := issueMatches[number]
		if len(matches) != 1 {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("awaiting-issue-%d-relationship", number), Message: fmt.Sprintf("awaiting-review issue #%d has %d inferred post-genesis work PRs", number, len(matches)), FactIDs: append([]string{fmt.Sprintf("issue-%d", number)}, matches...)})
		}
	}
	sort.Slice(workPlans, func(i, j int) bool {
		left := workDistances[workPlans[i].Input.PullRequest.Number]
		right := workDistances[workPlans[j].Input.PullRequest.Number]
		if left == right {
			return workPlans[i].Input.PullRequest.Number < workPlans[j].Input.PullRequest.Number
		}
		return left < right
	})
	for index := range workPlans {
		if index > 0 && workDistances[workPlans[index-1].Input.PullRequest.Number] == workDistances[workPlans[index].Input.PullRequest.Number] {
			factIDs := append([]string{}, workPlans[index-1].SourceFactIDs...)
			factIDs = append(factIDs, workPlans[index].SourceFactIDs...)
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-distance", workPlans[index].Input.PullRequest.Number), Message: "two merged work PRs have the same ancestry distance from the reviewed boundary", FactIDs: factIDs})
		}
		workPlans[index].Order = uint64(index + 1)
		operations = append(operations,
			DeliveryRecordOperation{ID: fmt.Sprintf("work-pr-%d-record", workPlans[index].Input.PullRequest.Number), Resource: fmt.Sprintf("%s#%d", repo, store.LedgerIssue.Number), Action: "append_record"},
			DeliveryRecordOperation{ID: fmt.Sprintf("work-pr-%d-checkpoint", workPlans[index].Input.PullRequest.Number), Resource: fmt.Sprintf("%s#comment-%d", repo, store.CheckpointComment.ID), Action: "update_checkpoint"},
		)
	}
	previousRevision := boundarySHA
	for _, work := range workPlans {
		comparison, err := client.CompareCommitsContext(ctx, repo, previousRevision, work.Input.MergeRevision)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		if comparison.Status != "ahead" {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("work-pr-%d-order", work.Input.PullRequest.Number), Message: fmt.Sprintf("merged work PR #%d does not extend the prior inferred staging revision", work.Input.PullRequest.Number), FactIDs: work.SourceFactIDs})
		}
		previousRevision = work.Input.MergeRevision
	}
	shadow, shadowFacts, shadowAmbiguities, err := acquireBootstrapPromotionShadows(ctx, client, repo, cfg, closed, workPlans)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, err
	}
	facts = append(facts, shadowFacts...)
	ambiguities = append(ambiguities, shadowAmbiguities...)
	rechecks := []DeliveryBootstrapRecheck{}
	if len(workPlans) > 0 {
		planned, complete, warnings, err := planPromotionRechecks(ctx, client, repo, cfg, workPlans[0].Input.MergeRevision)
		if err != nil {
			return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
		}
		if !complete {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "promotion-recheck-incomplete", Message: strings.Join(warnings, "; "), FactIDs: workPlans[0].SourceFactIDs})
		}
		lastWork := workPlans[len(workPlans)-1]
		for _, plannedRecheck := range planned {
			rechecks = append(rechecks, DeliveryBootstrapRecheck{AfterStagedPullRequest: lastWork.Input.PullRequest.Number, Recheck: plannedRecheck})
			operations = append(operations, DeliveryRecordOperation{ID: fmt.Sprintf("promotion-%d-policy-recheck", plannedRecheck.PullRequest.Number), Resource: fmt.Sprintf("%s#%d@%s", repo, plannedRecheck.PullRequest.Number, plannedRecheck.HeadSHA), Action: "rerequest_check"})
		}
	}
	retryListing, err := client.ListIssueCommentsAfterContext(ctx, repo, store.LedgerIssue.Number, store.CheckpointComment.UpdatedAt)
	if err != nil {
		return delivery.BootstrapPlanInput{}, nil, nil, classifyGitHubError(err)
	}
	if !retryListing.Complete {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "bootstrap-retry-boundary-incomplete", Message: "bootstrap retry discovery did not reach the checkpoint boundary"})
	}
	retryComments := make([]delivery.StoredComment, 0, len(retryListing.Comments))
	for _, comment := range retryListing.Comments {
		retryComments = append(retryComments, storedDeliveryComment(comment))
	}
	boundary := &delivery.BootstrapGenesisBoundary{Explicit: explicit, StagingSHA: boundarySHA, BaseSHA: boundaryBaseSHA, SourceFactIDs: boundaryFacts, Rationale: "reviewed last acknowledged base and staging boundary"}
	return delivery.BootstrapPlanInput{
		Genesis:     delivery.GenesisCheckpointInput{LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository, Issue: checkpoint.Issue, StagingSHA: checkpoint.Coverage.StagingSHA, Writer: checkpoint.Coverage.Writer, ObservedAt: checkpoint.Coverage.ObservedAt},
		SourceFacts: facts, Relationships: relationships, Ambiguities: ambiguities, GenesisBoundary: boundary,
		OwnershipRecords: ownershipPlans, StagedWork: workPlans, RetryComments: retryComments, ShadowComparisons: shadow,
	}, operations, rechecks, nil
}

func acquireBootstrapPromotionShadows(ctx context.Context, client DeliveryBootstrapGitHub, repo string, cfg config.Config, closed gh.PullRequestListing, work []delivery.BootstrapStagedWork) ([]delivery.BootstrapShadowComparison, []delivery.BootstrapSourceFact, []delivery.BootstrapAmbiguity, error) {
	open, err := client.ListOpenPullRequestsBoundedContext(ctx, repo, cfg.Repository.BaseBranch)
	if err != nil {
		return nil, nil, nil, classifyGitHubError(err)
	}
	facts := []delivery.BootstrapSourceFact{}
	ambiguities := []delivery.BootstrapAmbiguity{}
	if !open.Complete {
		ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: "promotion-shadow-open-cap", Message: "open promotion listing reached its bounded cap"})
	}
	ledgerWork := make([]delivery.PromotionProjectionWork, 0, len(work))
	for _, item := range work {
		issues := make([]int, 0, len(item.Input.Issues))
		for _, issue := range item.Input.Issues {
			issues = append(issues, issue.Number)
		}
		ledgerWork = append(ledgerWork, delivery.PromotionProjectionWork{PullRequestNumber: item.Input.PullRequest.Number, IssueNumbers: issues})
	}
	comparisons := []delivery.BootstrapShadowComparison{}
	for _, promotion := range open.PullRequests {
		if classifyTransportPullRequest(promotion, cfg) != policy.PRFlowPromotion {
			continue
		}
		factID := fmt.Sprintf("promotion-shadow-%d", promotion.Number)
		facts = append(facts, bootstrapFact(factID, "promotionShadow", "boundedGitHubAndDeliveryFacts", map[string]string{
			"number": fmt.Sprint(promotion.Number), "nodeId": promotion.NodeID, "baseSha": promotion.BaseSHA, "headSha": promotion.HeadSHA,
		}))
		legacy := delivery.PromotionProjection{Complete: closed.Complete, Work: []delivery.PromotionProjectionWork{}}
		for _, item := range work {
			containedByBase, err := client.CompareCommitsContext(ctx, repo, item.Input.MergeRevision, promotion.BaseSHA)
			if err != nil {
				return nil, nil, nil, classifyGitHubError(err)
			}
			if containedByBase.Status == "ahead" || containedByBase.Status == "identical" {
				continue
			}
			containedByHead, err := client.CompareCommitsContext(ctx, repo, item.Input.MergeRevision, promotion.HeadSHA)
			if err != nil {
				return nil, nil, nil, classifyGitHubError(err)
			}
			if containedByHead.Status != "ahead" && containedByHead.Status != "identical" {
				continue
			}
			issues := make([]int, 0, len(item.Input.Issues))
			for _, issue := range item.Input.Issues {
				issues = append(issues, issue.Number)
			}
			legacy.Work = append(legacy.Work, delivery.PromotionProjectionWork{PullRequestNumber: item.Input.PullRequest.Number, IssueNumbers: issues})
		}
		comparison := delivery.ComparePromotionProjections(delivery.PromotionProjection{Complete: closed.Complete && open.Complete, Work: ledgerWork}, legacy)
		comparisons = append(comparisons, delivery.BootstrapShadowComparison{
			PullRequest: delivery.ResourceIdentity{Number: promotion.Number, NodeID: promotion.NodeID}, BaseSHA: promotion.BaseSHA, HeadSHA: promotion.HeadSHA, Comparison: comparison,
		})
		if !comparison.Matches {
			ambiguities = append(ambiguities, delivery.BootstrapAmbiguity{Code: fmt.Sprintf("promotion-shadow-%d-mismatch", promotion.Number), Message: fmt.Sprintf("promotion #%d delivery and ancestry shadow projections do not agree", promotion.Number), FactIDs: []string{factID}})
		}
	}
	return comparisons, facts, ambiguities, nil
}

func applyDeliveryBootstrap(ctx context.Context, client DeliveryBootstrapGitHub, repo string, cfg config.Config, store acquiredDeliveryStore, result DeliveryBootstrapResult) (DeliveryBootstrapResult, error) {
	results := make([]operation.Result, len(result.Operations))
	for index, planned := range result.Operations {
		results[index] = operation.Result{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusNotAttempted}
	}
	operationIndexes := map[string]int{}
	for index, planned := range result.Operations {
		operationIndexes[planned.ID] = index
	}
	committedAny := false
	rechecksDone := false
	fail := func(index int, message string, err error) (DeliveryBootstrapResult, error) {
		results[index].Status = operation.StatusFailed
		results[index].Error = operationFailure("github", message, err)
		if committedAny && !rechecksDone {
			reconcileBootstrapPromotionRechecks(ctx, client, repo, result.Rechecks, operationIndexes, results)
			rechecksDone = true
		}
		report := operation.NewReport(results)
		result.Report = &report
		return result, apperror.WithReport(classifyGitHubError(err), report)
	}
	currentStore := store
	if boundaryIndex, planned := operationIndexes[bootstrapGenesisBoundaryOperationID]; planned {
		body, err := bootstrapGenesisBoundaryBody(store.Snapshot.Locator, store.Snapshot.Checkpoint, result.GenesisCheckpoint)
		if err != nil {
			return fail(boundaryIndex, "bootstrap genesis boundary is invalid", err)
		}
		checkpointNow, err := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
		if err != nil {
			return fail(boundaryIndex, "bootstrap genesis boundary preflight failed", err)
		}
		if checkpointNow.ID != store.CheckpointComment.ID || checkpointNow.NodeID != store.CheckpointComment.NodeID || checkpointNow.Body != store.CheckpointComment.Body {
			return fail(boundaryIndex, "bootstrap genesis boundary preflight failed", fmt.Errorf("checkpoint changed after planning"))
		}
		updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, store.CheckpointComment.ID, body)
		if err != nil {
			readback, readErr := client.GetIssueCommentContext(ctx, repo, store.CheckpointComment.ID)
			if readErr != nil || readback.Body != body {
				return fail(boundaryIndex, "bootstrap genesis boundary update failed", err)
			}
			updated = readback
		}
		if trustErr := validateTrustedDeliveryWrite(updated, repo, store.LedgerIssue.Number); trustErr != nil || updated.ID != store.CheckpointComment.ID || updated.NodeID != store.CheckpointComment.NodeID || updated.Body != body {
			if trustErr == nil {
				trustErr = fmt.Errorf("bootstrap genesis boundary update returned stale content or identity")
			}
			return fail(boundaryIndex, "bootstrap genesis boundary update was not trusted", trustErr)
		}
		results[boundaryIndex].Status = operation.StatusApplied
		committedAny = true
		currentStore, err = acquireDeliveryStore(ctx, client, repo, store.Snapshot.Locator)
		if err != nil {
			return fail(boundaryIndex, "bootstrap genesis boundary readback failed", err)
		}
		if currentStore.Snapshot.Checkpoint.Digest != result.GenesisCheckpoint.Digest {
			return fail(boundaryIndex, "bootstrap genesis boundary readback failed", fmt.Errorf("checkpoint digest changed after update"))
		}
	}
	for _, ownership := range result.OwnershipRecords {
		commentIndex := operationIndexes[fmt.Sprintf("issue-%d-ownership-record", ownership.Issue.Number)]
		labelIndex := operationIndexes[fmt.Sprintf("issue-%d-ownership-index", ownership.Issue.Number)]
		comment, err := client.CreateIssueCommentReturningContext(ctx, repo, ownership.Issue.Number, ownership.Body)
		if err != nil {
			return fail(commentIndex, "bootstrap ownership backfill failed", err)
		}
		if err := validateTrustedDeliveryWrite(comment, repo, ownership.Issue.Number); err != nil || comment.Body != ownership.Body {
			if err == nil {
				err = fmt.Errorf("bootstrap ownership backfill returned stale content")
			}
			return fail(commentIndex, "bootstrap ownership backfill was not trusted", err)
		}
		results[commentIndex].Status = operation.StatusApplied
		if err := client.AddIssueLabelsContext(ctx, repo, ownership.Issue.Number, []string{policy.ManagedIssueIndexLabel}); err != nil {
			return fail(labelIndex, "bootstrap ownership index failed", err)
		}
		results[labelIndex].Status = operation.StatusApplied
	}
	if len(result.StagedWork) > 0 {
		freshStore, err := acquireDeliveryStore(ctx, client, repo, store.Snapshot.Locator)
		if err != nil {
			firstIndex := operationIndexes[fmt.Sprintf("work-pr-%d-record", result.StagedWork[0].Record.PullRequest.Number)]
			return fail(firstIndex, "bootstrap checkpoint preflight failed", err)
		}
		currentStore = freshStore
	}
	for _, planned := range result.StagedWork {
		recordIndex := operationIndexes[fmt.Sprintf("work-pr-%d-record", planned.Record.PullRequest.Number)]
		checkpointIndex := operationIndexes[fmt.Sprintf("work-pr-%d-checkpoint", planned.Record.PullRequest.Number)]
		latestPR, err := client.GetPullRequestContext(ctx, repo, planned.Record.PullRequest.Number)
		if err != nil {
			return fail(recordIndex, "bootstrap staged-work pull request preflight failed", err)
		}
		if !pullRequestMatchesStagedWorkRecord(latestPR, planned.Record, cfg) {
			return fail(recordIndex, "bootstrap staged-work pull request changed after planning", fmt.Errorf("stale pull request facts or issue relationships"))
		}
		appendPlan, err := delivery.PlanStagedWorkAppend(currentStore.Snapshot, delivery.StagedWorkAppendInput{
			PullRequest: planned.Record.PullRequest, StagingBranch: planned.Record.StagingBranch, BaseSHA: planned.Record.BaseSHA, HeadSHA: planned.Record.HeadSHA,
			MergeRevision: planned.Record.MergeRevision, MergedAt: planned.Record.MergedAt, Issues: planned.Record.Issues, Writer: planned.Record.Writer,
		})
		if err != nil {
			return fail(recordIndex, "bootstrap staged-work plan became stale", err)
		}
		if !appendPlan.Applicable {
			return fail(recordIndex, "bootstrap staged-work plan became stale", fmt.Errorf("record is already committed"))
		}
		if appendPlan.Record == nil || appendPlan.Record.Digest != planned.Record.Digest {
			return fail(recordIndex, "bootstrap staged-work plan became stale", fmt.Errorf("record digest changed after review"))
		}
		body, err := delivery.RenderStagedWorkRecord(*appendPlan.Record)
		if err != nil {
			return fail(recordIndex, "bootstrap staged-work encoding failed", err)
		}
		var comment *gh.IssueComment
		if planned.ExistingComment != nil {
			reviewed, readErr := client.GetIssueCommentContext(ctx, repo, planned.ExistingComment.DatabaseID)
			if readErr != nil {
				return fail(recordIndex, "reviewed bootstrap staged-work retry disappeared", readErr)
			}
			if reviewed.NodeID != planned.ExistingComment.NodeID || validateTrustedDeliveryWrite(reviewed, repo, currentStore.LedgerIssue.Number) != nil || reviewed.Body != body {
				return fail(recordIndex, "reviewed bootstrap staged-work retry changed", fmt.Errorf("reviewed retry identity, trust, or bytes changed after planning"))
			}
			comment = &reviewed
		} else {
			comment, err = findTrustedStagedWorkRetry(ctx, client, repo, currentStore.LedgerIssue.Number, currentStore.CheckpointComment.UpdatedAt, appendPlan.Record.RetryID)
			if err != nil {
				return fail(recordIndex, "bootstrap staged-work retry lookup failed", err)
			}
			if comment != nil {
				return fail(recordIndex, "bootstrap staged-work retry appeared after review", fmt.Errorf("retry comment must be included in a new reviewed plan"))
			}
		}
		if comment == nil {
			created, createErr := client.CreateIssueCommentReturningContext(ctx, repo, currentStore.LedgerIssue.Number, body)
			if createErr != nil {
				comment, err = findTrustedStagedWorkRetry(ctx, client, repo, currentStore.LedgerIssue.Number, currentStore.CheckpointComment.UpdatedAt, appendPlan.Record.RetryID)
				if err != nil {
					return fail(recordIndex, "bootstrap staged-work append was ambiguous", err)
				}
				if comment == nil {
					return fail(recordIndex, "bootstrap staged-work append failed", createErr)
				}
			} else {
				comment = &created
			}
			if err := validateTrustedDeliveryWrite(*comment, repo, currentStore.LedgerIssue.Number); err != nil {
				return fail(recordIndex, "bootstrap staged-work append was not trusted", err)
			}
			results[recordIndex].Status = operation.StatusApplied
		} else {
			results[recordIndex].Status = operation.StatusUnchanged
		}
		parsedRecord, err := delivery.ParseStagedWorkComment(storedDeliveryComment(*comment))
		if err != nil || parsedRecord.Digest != appendPlan.Record.Digest {
			if err == nil {
				err = fmt.Errorf("bootstrap staged-work retry does not match the reviewed record")
			}
			return fail(recordIndex, "bootstrap staged-work append was not exact", err)
		}
		commit, err := delivery.FinalizeStagedWorkAppend(appendPlan, currentStore.Snapshot.Checkpoint, delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID})
		if err != nil {
			return fail(checkpointIndex, "bootstrap staged-work finalization failed", err)
		}
		updated, err := client.UpdateIssueCommentReturningContext(ctx, repo, currentStore.CheckpointComment.ID, commit.CheckpointBody)
		if err != nil {
			readback, readErr := client.GetIssueCommentContext(ctx, repo, currentStore.CheckpointComment.ID)
			if readErr != nil || readback.Body != commit.CheckpointBody {
				return fail(checkpointIndex, "bootstrap checkpoint update failed", err)
			}
			updated = readback
		}
		if trustErr := validateTrustedDeliveryWrite(updated, repo, currentStore.LedgerIssue.Number); trustErr != nil || updated.Body != commit.CheckpointBody || updated.ID != currentStore.CheckpointComment.ID || updated.NodeID != currentStore.CheckpointComment.NodeID {
			if trustErr == nil {
				trustErr = fmt.Errorf("delivery checkpoint update returned stale content or identity")
			}
			return fail(checkpointIndex, "bootstrap checkpoint update was not trusted", trustErr)
		}
		results[checkpointIndex].Status = operation.StatusApplied
		committedAny = true
		currentStore, err = acquireDeliveryStore(ctx, client, repo, appendPlan.Locator)
		if err != nil {
			return fail(checkpointIndex, "bootstrap checkpoint readback failed", err)
		}
	}
	if len(result.Rechecks) > 0 {
		failedIndex, err := reconcileBootstrapPromotionRechecks(ctx, client, repo, result.Rechecks, operationIndexes, results)
		rechecksDone = true
		if err != nil {
			return fail(failedIndex, "bootstrap promotion policy recheck failed", err)
		}
	}
	report := operation.NewReport(results)
	result.Report = &report
	return result, nil
}

func bootstrapGenesisBoundaryBody(locator delivery.DeliveryStoreLocator, current, planned delivery.DeliveryCheckpoint) (string, error) {
	expected := current
	expected.GenesisBaseSHA = planned.GenesisBaseSHA
	expected.Digest = planned.Digest
	expectedBody, err := delivery.RenderCheckpointIndex(locator, expected)
	if err != nil {
		return "", fmt.Errorf("reviewed checkpoint changes more than the genesis base boundary: %w", err)
	}
	plannedBody, err := delivery.RenderCheckpointIndex(locator, planned)
	if err != nil {
		return "", err
	}
	if expectedBody != plannedBody {
		return "", fmt.Errorf("reviewed checkpoint changes more than the genesis base boundary")
	}
	return plannedBody, nil
}

func reconcileBootstrapPromotionRechecks(ctx context.Context, client DeliveryBootstrapGitHub, repo string, planned []DeliveryBootstrapRecheck, operationIndexes map[string]int, results []operation.Result) (int, error) {
	for _, item := range planned {
		recheck := item.Recheck
		index := operationIndexes[fmt.Sprintf("promotion-%d-policy-recheck", recheck.PullRequest.Number)]
		if results[index].Status == operation.StatusApplied || results[index].Status == operation.StatusUnchanged {
			continue
		}
		status, err := reconcilePromotionRecheck(ctx, client, repo, recheck)
		results[index].Status = status
		if err != nil {
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "bootstrap promotion policy recheck failed", err)
			return index, err
		}
	}
	return -1, nil
}

func pullRequestMatchesStagedWorkRecord(pr gh.PullRequest, record delivery.StagedWorkRecord, cfg config.Config) bool {
	if !pr.Merged || pr.Number != record.PullRequest.Number || pr.NodeID != record.PullRequest.NodeID || pr.BaseSHA != record.BaseSHA || pr.HeadSHA != record.HeadSHA || pr.MergeRevision != record.MergeRevision || pr.MergedAt.UTC().Format("2006-01-02T15:04:05Z") != record.MergedAt || !isManagedWorkPullRequest(pr, cfg) {
		return false
	}
	return true
}

func bootstrapFact(id, kind, source string, details map[string]string) delivery.BootstrapSourceFact {
	content, _ := json.Marshal(struct {
		ID, Kind, Source string
		Details          map[string]string
	}{id, kind, source, details})
	digest := sha256.Sum256(content)
	return delivery.BootstrapSourceFact{ID: id, Kind: kind, Source: source, ObservedDigest: "sha256:" + hex.EncodeToString(digest[:]), Details: details}
}
