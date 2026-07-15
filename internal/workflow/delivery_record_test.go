package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

type deliveryRecordGitHubStub struct {
	repository       gh.RepositoryIdentity
	issues           map[int]gh.Issue
	comments         map[int][]gh.IssueComment
	checkpoint       gh.IssueComment
	pull             map[int]gh.PullRequest
	open             gh.PullRequestListing
	closed           gh.PullRequestListing
	closedByBase     map[string]gh.PullRequestListing
	openIssues       []gh.Issue
	branch           gh.Branch
	records          map[int64]gh.IssueComment
	created          []string
	updated          []string
	rerequest        []int64
	checkCalls       int
	transitionChecks bool
	divergeCoverage  bool
	newestIncomplete bool
	ambiguousUpdate  bool
	comparisons      map[string]gh.CommitComparison
	checkRollups     map[int]gh.CheckRollup
	checkErrors      map[int]error
}

func (stub *deliveryRecordGitHubStub) GetRepositoryIdentityContext(context.Context, string) (gh.RepositoryIdentity, error) {
	return stub.repository, nil
}
func (stub *deliveryRecordGitHubStub) GetIssueContext(_ context.Context, _ string, number int) (gh.Issue, error) {
	issue, ok := stub.issues[number]
	if !ok {
		return gh.Issue{}, fmt.Errorf("missing issue %d", number)
	}
	return issue, nil
}
func (stub *deliveryRecordGitHubStub) GetIssueCommentContext(_ context.Context, _ string, id int64) (gh.IssueComment, error) {
	if id != stub.checkpoint.ID {
		comment, exists := stub.records[id]
		if !exists {
			return gh.IssueComment{}, fmt.Errorf("missing comment %d", id)
		}
		return comment, nil
	}
	return stub.checkpoint, nil
}
func (stub *deliveryRecordGitHubStub) GetPullRequestContext(_ context.Context, _ string, number int) (gh.PullRequest, error) {
	return stub.pull[number], nil
}
func (stub *deliveryRecordGitHubStub) GetBranchContext(context.Context, string, string) (gh.Branch, error) {
	return stub.branch, nil
}

func (stub *deliveryRecordGitHubStub) ListClosedPullRequestsBoundedContext(_ context.Context, _ string, base string) (gh.PullRequestListing, error) {
	if listing, exists := stub.closedByBase[base]; exists {
		return listing, nil
	}
	return stub.closed, nil
}
func (stub *deliveryRecordGitHubStub) ListOpenPullRequestsBoundedContext(context.Context, string, string) (gh.PullRequestListing, error) {
	return stub.open, nil
}
func (stub *deliveryRecordGitHubStub) ListIssueCommentsContext(_ context.Context, _ string, number int) ([]gh.IssueComment, error) {
	return stub.comments[number], nil
}
func (stub *deliveryRecordGitHubStub) AddIssueLabelsContext(context.Context, string, int, []string) error {
	return nil
}
func (stub *deliveryRecordGitHubStub) CreateIssueCommentReturningContext(_ context.Context, _ string, _ int, body string) (gh.IssueComment, error) {
	stub.created = append(stub.created, body)
	comment := gh.IssueComment{ID: int64(1000 + len(stub.created)), NodeID: fmt.Sprintf("IC_%d", 1000+len(stub.created)), IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: body, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}}
	if stub.records == nil {
		stub.records = map[int64]gh.IssueComment{}
	}
	stub.records[comment.ID] = comment
	return comment, nil
}
func (stub *deliveryRecordGitHubStub) UpdateIssueCommentReturningContext(_ context.Context, _ string, _ int64, body string) (gh.IssueComment, error) {
	stub.updated = append(stub.updated, body)
	stub.checkpoint.Body = body
	if stub.ambiguousUpdate {
		return gh.IssueComment{}, fmt.Errorf("ambiguous update response")
	}
	return stub.checkpoint, nil
}
func (stub *deliveryRecordGitHubStub) ListNewestIssueCommentsContext(context.Context, string, int) (gh.IssueCommentListing, error) {
	comments := make([]gh.IssueComment, 0, len(stub.records))
	for _, comment := range stub.records {
		// The GraphQL newest-comment listing does not include the REST issue URL.
		comment.IssueURL = ""
		comments = append(comments, comment)
	}
	return gh.IssueCommentListing{Comments: comments, Complete: !stub.newestIncomplete}, nil
}
func (stub *deliveryRecordGitHubStub) CompareCommitsContext(_ context.Context, _ string, base, head string) (gh.CommitComparison, error) {
	if comparison, ok := stub.comparisons[base+"..."+head]; ok {
		return comparison, nil
	}
	if base == head {
		return gh.CommitComparison{Status: "identical"}, nil
	}
	if stub.divergeCoverage && base == strings.Repeat("0", 40) {
		return gh.CommitComparison{Status: "diverged"}, nil
	}
	ahead := 1
	if base == strings.Repeat("0", 40) && head == strings.Repeat("6", 40) {
		ahead = 2
	}
	return gh.CommitComparison{Status: "ahead", AheadBy: ahead}, nil
}

func TestBootstrapPromotionShadowAgreementAndMismatch(t *testing.T) {
	cfg := config.DefaultConfig()
	mergeRevision := strings.Repeat("3", 40)
	baseSHA := strings.Repeat("4", 40)
	headSHA := strings.Repeat("5", 40)
	promotion := gh.PullRequest{
		Number: 50, NodeID: "PR_50", BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", BaseSHA: baseSHA, HeadSHA: headSHA, State: "open",
	}
	work := []delivery.BootstrapStagedWork{{Input: delivery.StagedWorkAppendInput{
		PullRequest: delivery.ResourceIdentity{Number: 20, NodeID: "PR_20"}, MergeRevision: mergeRevision,
		Issues: []delivery.ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: strings.Repeat("a", 64)}},
	}}}
	stub := &deliveryRecordGitHubStub{
		open: gh.PullRequestListing{PullRequests: []gh.PullRequest{promotion}, Complete: true},
		comparisons: map[string]gh.CommitComparison{
			mergeRevision + "..." + baseSHA: {Status: "diverged"},
			mergeRevision + "..." + headSHA: {Status: "ahead"},
		},
	}
	comparisons, _, ambiguities, err := acquireBootstrapPromotionShadows(context.Background(), stub, "example/repo", cfg, gh.PullRequestListing{Complete: true}, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparisons) != 1 || !comparisons[0].Comparison.Matches || len(ambiguities) != 0 {
		t.Fatalf("matching shadow = %+v ambiguities=%+v", comparisons, ambiguities)
	}

	stub.comparisons[mergeRevision+"..."+headSHA] = gh.CommitComparison{Status: "diverged"}
	comparisons, _, ambiguities, err = acquireBootstrapPromotionShadows(context.Background(), stub, "example/repo", cfg, gh.PullRequestListing{Complete: true}, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparisons) != 1 || comparisons[0].Comparison.Matches || len(ambiguities) != 1 || ambiguities[0].Code != "promotion-shadow-50-mismatch" {
		t.Fatalf("mismatching shadow = %+v ambiguities=%+v", comparisons, ambiguities)
	}
}

func TestDeliveryRecorderCommitsExactBaseToStagingSynchronization(t *testing.T) {
	cfg, locator, checkpointBody := deliveryWorkflowFixture(t)
	event := gh.PullRequestEvent{
		Action: "closed", Number: 30, NodeID: "PR_30", BaseRef: cfg.Repository.StagingBranch, HeadRef: cfg.Repository.BaseBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", BaseSHA: strings.Repeat("0", 40),
		HeadSHA: strings.Repeat("4", 40), State: "closed", Merged: true, MergedAt: time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC), MergeRevision: strings.Repeat("5", 40),
	}
	pr := gh.PullRequest{
		Number: event.Number, NodeID: event.NodeID, BaseRef: event.BaseRef, HeadRef: event.HeadRef, BaseRepositoryFullName: event.BaseRepositoryFullName,
		HeadRepositoryFullName: event.HeadRepositoryFullName, BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA, State: event.State,
		Merged: event.Merged, MergedAt: event.MergedAt, MergeRevision: event.MergeRevision,
	}
	stub := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues:     map[int]gh.Issue{900: {Number: 900, NodeID: "I_900", Locked: true}}, pull: map[int]gh.PullRequest{30: pr},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpointBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		branch:     gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: event.MergeRevision}, records: map[int64]gh.IssueComment{}, open: gh.PullRequestListing{Complete: true},
		comparisons: map[string]gh.CommitComparison{}, checkRollups: map[int]gh.CheckRollup{},
	}
	store, err := acquireDeliveryStore(context.Background(), stub, "example/repo", locator)
	if err != nil {
		t.Fatal(err)
	}
	input := DeliveryRecordInput{WorkflowName: "Delivery Recorder", RunID: 99}
	staleEvent := event
	staleEvent.BaseSHA = strings.Repeat("8", 40)
	if stale, staleErr := runDeliverySynchronization(context.Background(), stub, "example/repo", cfg, store, staleEvent, input); staleErr == nil || !strings.Contains(staleErr.Error(), "stale") || stale.Synchronization != nil {
		t.Fatalf("stale event preview = %+v, err = %v", stale, staleErr)
	}
	preview, err := runDeliverySynchronization(context.Background(), stub, "example/repo", cfg, store, event, input)
	if err != nil || preview.Synchronization == nil || !preview.Synchronization.Applicable || len(preview.Operations) != 2 {
		t.Fatalf("preview = %+v, err = %v", preview, err)
	}
	rewrittenStaging := strings.Repeat("9", 40)
	stub.branch.SHA = rewrittenStaging
	stub.comparisons[event.MergeRevision+"..."+rewrittenStaging] = gh.CommitComparison{Status: "diverged"}
	stale, err := applyDeliverySynchronization(context.Background(), stub, "example/repo", cfg, store, pr, preview)
	if err == nil || stale.Report == nil || len(stub.created) != 0 || len(stub.updated) != 0 {
		t.Fatalf("stale staging apply = %+v, err = %v", stale, err)
	}
	stub.branch.SHA = event.MergeRevision
	input.Apply = true
	applied, err := runDeliverySynchronization(context.Background(), stub, "example/repo", cfg, store, event, input)
	if err != nil || applied.Report == nil || applied.Report.Status != operation.ReportCompleted || len(stub.created) != 1 || len(stub.updated) != 1 {
		t.Fatalf("applied = %+v, err = %v", applied, err)
	}
	committed, err := acquireDeliveryStore(context.Background(), stub, "example/repo", locator)
	if err != nil || committed.Snapshot.Checkpoint.BaseIntegration == nil || len(committed.Snapshot.BaseIntegrations) != 1 || committed.Snapshot.BaseIntegrations[0].Source != delivery.IntegrationSynchronization {
		t.Fatalf("committed synchronization = %+v, err = %v", committed.Snapshot, err)
	}
}

func TestDeliveryRecorderRejectsSquashOrRebaseSynchronizationTopology(t *testing.T) {
	cfg, locator, checkpointBody := deliveryWorkflowFixture(t)
	event := gh.PullRequestEvent{Action: "closed", Number: 30, NodeID: "PR_30", BaseRef: cfg.Repository.StagingBranch, HeadRef: cfg.Repository.BaseBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", BaseSHA: strings.Repeat("0", 40), HeadSHA: strings.Repeat("4", 40),
		State: "closed", Merged: true, MergedAt: time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC), MergeRevision: strings.Repeat("5", 40)}
	pr := gh.PullRequest{Number: event.Number, NodeID: event.NodeID, BaseRef: event.BaseRef, HeadRef: event.HeadRef, BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA, State: "closed", Merged: true, MergedAt: event.MergedAt, MergeRevision: event.MergeRevision}
	stub := &deliveryRecordGitHubStub{repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}, issues: map[int]gh.Issue{900: {Number: 900, NodeID: "I_900", Locked: true}}, pull: map[int]gh.PullRequest{30: pr}, checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpointBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}}, branch: gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: event.MergeRevision}, records: map[int64]gh.IssueComment{}, comparisons: map[string]gh.CommitComparison{event.HeadSHA + "..." + event.MergeRevision: {Status: "diverged"}}}
	store, err := acquireDeliveryStore(context.Background(), stub, "example/repo", locator)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runDeliverySynchronization(context.Background(), stub, "example/repo", cfg, store, event, DeliveryRecordInput{WorkflowName: "Delivery Recorder", RunID: 99})
	if err != nil || result.Applicable || !strings.Contains(strings.Join(result.Warnings, " "), "ancestry") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}
func (stub *deliveryRecordGitHubStub) GetCheckRollupContext(_ context.Context, _ string, number int, sha string) (gh.CheckRollup, error) {
	stub.checkCalls++
	if err := stub.checkErrors[number]; err != nil {
		return gh.CheckRollup{}, err
	}
	if rollup, ok := stub.checkRollups[number]; ok {
		return rollup, nil
	}
	check := gh.CheckState{ID: 700, Name: deliveryPolicyCheckName, Status: "completed", Conclusion: "success"}
	if stub.transitionChecks && stub.checkCalls == 1 {
		check.Status, check.Conclusion = "in_progress", ""
	}
	return gh.CheckRollup{Complete: true, PRNumber: number, HeadSHA: sha, Checks: []gh.CheckState{check}}, nil
}
func (stub *deliveryRecordGitHubStub) RerequestCheckRunContext(_ context.Context, _ string, id int64) error {
	stub.rerequest = append(stub.rerequest, id)
	return nil
}
func (stub *deliveryRecordGitHubStub) ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error) {
	return stub.openIssues, nil
}

func TestDeliveryRecordPlansAndAppliesStagedWorkBeforeRecheckingPromotion(t *testing.T) {
	cfg, locator, checkpoint := deliveryWorkflowFixture(t)
	configPath := filepath.Join(t.TempDir(), "baton.yml")
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	mergedAt := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	work := gh.PullRequest{
		Number: 20, NodeID: "PR_20", Title: "feat: work", Body: "Refs #7", BaseRef: cfg.Repository.StagingBranch, BaseSHA: strings.Repeat("1", 40),
		HeadRef: cfg.Repository.WorkBranchPrefix + "7", HeadSHA: strings.Repeat("2", 40), BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		State: "closed", Merged: true, MergedAt: mergedAt, MergeRevision: strings.Repeat("3", 40),
	}
	work2 := work
	work2.Number, work2.NodeID, work2.HeadRef = 21, "PR_21", cfg.Repository.WorkBranchPrefix+"8"
	work2.BaseSHA, work2.HeadSHA, work2.MergeRevision = work.MergeRevision, strings.Repeat("5", 40), strings.Repeat("6", 40)
	work2.MergedAt = mergedAt.Add(time.Hour)
	promotion := gh.PullRequest{Number: 50, NodeID: "PR_50", Title: "promote", BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch, BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", HeadSHA: strings.Repeat("4", 40), State: "open"}
	ownership := policy.NewManagedIssueRecord("I_7", 7)
	stub := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues: map[int]gh.Issue{
			locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true},
			7:                    {Number: 7, NodeID: "I_7", State: "open"},
		},
		comments:   map[int][]gh.IssueComment{7: {{ID: 77, Body: policy.RenderManagedIssueRecord(ownership), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}}},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpoint, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		pull:       map[int]gh.PullRequest{20: work, 21: work2, 50: promotion}, open: gh.PullRequestListing{PullRequests: []gh.PullRequest{promotion}, Complete: true},
		closed: gh.PullRequestListing{PullRequests: []gh.PullRequest{work2, work}, Complete: true}, branch: gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: work2.MergeRevision}, records: map[int64]gh.IssueComment{},
		transitionChecks: true,
	}
	eventPath := filepath.Join(t.TempDir(), "event.json")
	event := fmt.Sprintf(`{"action":"closed","pull_request":{"number":21,"node_id":"PR_21","title":"feat: work","body":"Refs #7","state":"closed","merged":true,"merged_at":"%s","merge_commit_sha":"%s","base":{"ref":"agent","sha":"%s","repo":{"full_name":"example/repo"}},"head":{"ref":"agent-work/8","sha":"%s","repo":{"full_name":"example/repo"}}},"repository":{"full_name":"example/repo"}}`, work2.MergedAt.Format(time.RFC3339), work2.MergeRevision, work2.BaseSHA, work2.HeadSHA)
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := NewDeliveryRecordWorkflow()
	workflow.newClient = func(context.Context, DeliveryRecordInput) (DeliveryRecordGitHub, error) { return stub, nil }
	result, err := workflow.RunContext(context.Background(), DeliveryRecordInput{EventPath: eventPath, ConfigPath: configPath, Apply: true, RunID: 99, WorkflowName: "Delivery Recorder"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Append == nil || result.Append.Record == nil || result.Append.Record.PullRequest.Number != 20 {
		t.Fatalf("append plan = %+v", result.Append)
	}
	if result.Report == nil || result.Report.Status != "completed" || len(stub.created) != 2 || len(stub.updated) != 2 || len(result.Candidates) != 2 {
		t.Fatalf("result/report = %+v created=%d updated=%d", result.Report, len(stub.created), len(stub.updated))
	}
	if len(stub.rerequest) != 1 || stub.rerequest[0] != 700 {
		t.Fatalf("rerequested checks = %v", stub.rerequest)
	}
	operationIDs := map[string]struct{}{}
	if len(result.Report.Operations) != len(result.Operations) {
		t.Fatalf("operation plan/report lengths differ: %d != %d", len(result.Operations), len(result.Report.Operations))
	}
	for index, planned := range result.Operations {
		if _, duplicate := operationIDs[planned.ID]; duplicate {
			t.Fatalf("duplicate operation ID %q in %+v", planned.ID, result.Operations)
		}
		operationIDs[planned.ID] = struct{}{}
		if result.Report.Operations[index].ID != planned.ID {
			t.Fatalf("operation plan/report order differs at %d: %q != %q", index, planned.ID, result.Report.Operations[index].ID)
		}
	}
}

func TestDeliveryRecordIsNoOpUntilLocatorIsPinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baton.yml")
	content, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewDeliveryRecordWorkflow().RunContext(context.Background(), DeliveryRecordInput{ConfigPath: path, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applicable || result.Report == nil || result.Report.Status != "completed" || len(result.Warnings) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeliveryRecordAdvancesCoverageAfterCompleteEmptyScan(t *testing.T) {
	cfg, locator, checkpoint := deliveryWorkflowFixture(t)
	configPath := filepath.Join(t.TempDir(), "baton.yml")
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	stagingSHA := strings.Repeat("9", 40)
	stub := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues:     map[int]gh.Issue{locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true}},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpoint, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		closed:     gh.PullRequestListing{Complete: true}, open: gh.PullRequestListing{Complete: true}, branch: gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: stagingSHA}, records: map[int64]gh.IssueComment{},
	}
	workflow := NewDeliveryRecordWorkflow()
	workflow.newClient = func(context.Context, DeliveryRecordInput) (DeliveryRecordGitHub, error) { return stub, nil }
	stub.divergeCoverage = true
	blocked, err := workflow.RunContext(context.Background(), DeliveryRecordInput{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Complete || blocked.Applicable || len(blocked.Operations) != 0 {
		t.Fatalf("divergent coverage result = %+v", blocked)
	}
	stub.divergeCoverage = false
	preview, err := workflow.RunContext(context.Background(), DeliveryRecordInput{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Coverage == nil || !preview.Coverage.Applicable || len(preview.Operations) != 1 || preview.Operations[0].Action != "advance_coverage" {
		t.Fatalf("coverage preview = %+v", preview)
	}
	stub.ambiguousUpdate = true
	result, err := workflow.RunContext(context.Background(), DeliveryRecordInput{ConfigPath: configPath, Apply: true, RunID: 99, WorkflowName: "Delivery Recorder"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage == nil || result.Coverage.Checkpoint == nil || result.Coverage.Checkpoint.Coverage.StagingSHA != stagingSHA {
		t.Fatalf("coverage = %+v", result.Coverage)
	}
	if len(result.Operations) != 1 || result.Operations[0].Action != "advance_coverage" || len(stub.updated) != 1 || result.Report == nil || result.Report.Status != "completed" {
		t.Fatalf("result = %+v updated=%d", result, len(stub.updated))
	}
}

func TestFinalPromotionCleanupRunsWhenCandidateReconciliationIsIncomplete(t *testing.T) {
	cfg := config.DefaultConfig()
	promotion := gh.PullRequest{
		Number: 50, NodeID: "PR_50", Title: "promote", BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", HeadSHA: strings.Repeat("4", 40), State: "open",
	}
	stub := &deliveryRecordGitHubStub{
		pull: map[int]gh.PullRequest{50: promotion},
		open: gh.PullRequestListing{PullRequests: []gh.PullRequest{promotion}, Complete: true},
	}
	planned := DeliveryRecordOperation{ID: "work-pr-20-checkpoint", Resource: "example/repo#comment-100", Action: "update_checkpoint"}
	report := operation.NewReport([]operation.Result{{ID: planned.ID, Resource: planned.Resource, Action: planned.Action, Status: operation.StatusApplied}})
	result := DeliveryRecordResult{
		SchemaVersion: 1, Kind: "deliveryRecordPlan", Repository: "example/repo", Complete: false, Applicable: false,
		Candidates: []delivery.ResourceIdentity{}, Ownership: []DeliveryOwnershipBackfill{}, Rechecks: []DeliveryPromotionRecheck{}, Operations: []DeliveryRecordOperation{planned}, Warnings: []string{"candidate scan incomplete"}, Report: &report,
	}

	finalized, err := finalizeDeliveryBatchRechecks(context.Background(), stub, "example/repo", cfg, strings.Repeat("3", 40), result)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Complete || len(stub.rerequest) != 1 || len(finalized.Rechecks) != 1 || finalized.Report == nil || finalized.Report.Status != operation.ReportCompleted {
		t.Fatalf("finalized = %+v rerequest=%v", finalized, stub.rerequest)
	}
}

func TestDeliveryBootstrapInitializationCreatesReviewedCheckpointAndReturnsLocator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baton.yml")
	content, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues:     map[int]gh.Issue{900: {Number: 900, NodeID: "I_900", Locked: true}},
		comments:   map[int][]gh.IssueComment{}, pull: map[int]gh.PullRequest{},
		records: map[int64]gh.IssueComment{},
	}
	workflow := NewDeliveryBootstrapWorkflow()
	workflow.newClient = func(context.Context, DeliveryBootstrapInput) (DeliveryBootstrapGitHub, error) { return stub, nil }
	input := DeliveryBootstrapInput{
		ConfigPath: path, Repository: "example/repo", Initialize: true, LedgerIssue: 900, LedgerID: "ledger-1", GenesisStagingSHA: strings.Repeat("a", 40),
		WorkflowName: "Delivery Recorder", RunID: 99, ObservedAt: "2026-07-14T00:00:00Z",
	}
	preview, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Initialization == nil || preview.Initialization.PlanID == "" || !preview.Initialization.Applicable {
		t.Fatalf("preview = %+v", preview.Initialization)
	}
	input.Apply, input.ReviewedPlanID = true, preview.Initialization.PlanID
	applied, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Initialization.Locator == nil || applied.Initialization.Locator.Checkpoint.DatabaseID == 0 || applied.Report == nil || applied.Report.Status != "completed" {
		t.Fatalf("applied = %+v report=%+v", applied.Initialization, applied.Report)
	}
}

func TestDeliveryBootstrapAppliesEachRecordAndCheckpointSeparately(t *testing.T) {
	cfg, locator, checkpoint := deliveryWorkflowFixture(t)
	cfg.Delivery.Authority = config.DeliveryAuthorityShadow
	configPath := filepath.Join(t.TempDir(), "baton.yml")
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	mergedAt := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	work := gh.PullRequest{
		Number: 20, NodeID: "PR_20", Title: "feat: work", Body: "Refs #7", BaseRef: cfg.Repository.StagingBranch, BaseSHA: strings.Repeat("1", 40),
		HeadRef: cfg.Repository.WorkBranchPrefix + "7", HeadSHA: strings.Repeat("2", 40), BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo",
		State: "closed", Merged: true, MergedAt: mergedAt, MergeRevision: strings.Repeat("3", 40),
	}
	work2 := work
	work2.Number, work2.NodeID, work2.HeadRef = 21, "PR_21", cfg.Repository.WorkBranchPrefix+"8"
	work2.BaseSHA, work2.HeadSHA, work2.MergeRevision = work.MergeRevision, strings.Repeat("5", 40), strings.Repeat("6", 40)
	work2.MergedAt = mergedAt.Add(time.Hour)
	ownership := policy.NewManagedIssueRecord("I_7", 7)
	stub := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues: map[int]gh.Issue{
			locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true},
			7:                    {Number: 7, NodeID: "I_7", State: "open"},
		},
		openIssues: []gh.Issue{{Number: 7, NodeID: "I_7", State: "open"}},
		comments:   map[int][]gh.IssueComment{7: {{ID: 77, Body: policy.RenderManagedIssueRecord(ownership), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}}},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpoint, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		pull:       map[int]gh.PullRequest{20: work, 21: work2}, open: gh.PullRequestListing{Complete: true},
		closedByBase: map[string]gh.PullRequestListing{
			cfg.Repository.StagingBranch: {PullRequests: []gh.PullRequest{work2, work}, Complete: true},
			cfg.Repository.BaseBranch:    {Complete: true},
		},
		branch: gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: work2.MergeRevision}, records: map[int64]gh.IssueComment{},
		newestIncomplete: true,
	}
	workflow := NewDeliveryBootstrapWorkflow()
	workflow.newClient = func(context.Context, DeliveryBootstrapInput) (DeliveryBootstrapGitHub, error) { return stub, nil }
	input := DeliveryBootstrapInput{ConfigPath: configPath, GenesisStagingSHA: strings.Repeat("0", 40), WorkflowName: "Delivery Recorder", RunID: 99}
	preview, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Applicable || len(preview.StagedWork) != 2 || len(preview.Operations) != 4 {
		t.Fatalf("preview = %+v", preview)
	}
	orphanBody, err := delivery.RenderStagedWorkRecord(preview.StagedWork[0].Record)
	if err != nil {
		t.Fatal(err)
	}
	stub.records[500] = gh.IssueComment{ID: 500, NodeID: "IC_500", IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: orphanBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}}
	input.Apply, input.ReviewedPlanID = true, preview.PlanID
	applied, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Report == nil || applied.Report.Status != "completed" || len(applied.Report.Operations) != 4 || applied.Report.Operations[0].Status != "unchanged" || len(stub.created) != 1 || len(stub.updated) != 2 {
		t.Fatalf("applied = %+v created=%d updated=%d", applied.Report, len(stub.created), len(stub.updated))
	}
}

func deliveryWorkflowFixture(t *testing.T) (config.Config, delivery.DeliveryStoreLocator, string) {
	t.Helper()
	repository := delivery.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}
	issue := delivery.ResourceIdentity{Number: 900, NodeID: "I_900"}
	comment := delivery.CommentIdentity{DatabaseID: 100, NodeID: "IC_100"}
	locator := delivery.DeliveryStoreLocator{Repository: repository, Issue: issue, Checkpoint: comment}
	checkpoint, err := delivery.NewGenesisCheckpoint(delivery.GenesisCheckpointInput{LedgerID: "ledger-1", Repository: repository, Issue: issue, StagingSHA: strings.Repeat("0", 40), BaseSHA: strings.Repeat("4", 40), Writer: delivery.WriterProvenance{Workflow: "bootstrap", RunID: 1}, ObservedAt: "2026-07-14T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := delivery.RenderCheckpointIndex(locator, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Delivery = &config.DeliveryConfig{Authority: config.DeliveryAuthoritySealed, Host: repository.Host, Repository: config.DeliveryRepository{FullName: repository.FullName, NodeID: repository.NodeID}, Issue: config.DeliveryResource{Number: issue.Number, NodeID: issue.NodeID}, Checkpoint: config.DeliveryComment{DatabaseID: comment.DatabaseID, NodeID: comment.NodeID}}
	return cfg, locator, body
}
