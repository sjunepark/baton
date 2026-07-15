package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

type transitionGitHub struct {
	*deliveryRecordGitHubStub
	pr            gh.PullRequest
	issues        map[int]gh.Issue
	getErr        map[int]error
	addErr        map[int]error
	closeErr      map[int]error
	removeErr     map[int]error
	checkpointErr error
	added         []int
	closed        []int
	removed       []int
	omitOwnership map[int]bool
}

func (stub *transitionGitHub) GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error) {
	return stub.pr, nil
}

func (stub *transitionGitHub) GetIssueContext(_ context.Context, _ string, number int) (gh.Issue, error) {
	if stub.deliveryRecordGitHubStub != nil {
		if issue, exists := stub.deliveryRecordGitHubStub.issues[number]; exists {
			return issue, nil
		}
	}
	if err := stub.getErr[number]; err != nil {
		return gh.Issue{}, err
	}
	issue := stub.issues[number]
	if issue.NodeID == "" {
		issue.NodeID = fmt.Sprintf("I_%d", number)
	}
	return issue, nil
}

func (stub *transitionGitHub) ListIssueCommentsContext(_ context.Context, _ string, number int) ([]gh.IssueComment, error) {
	if stub.omitOwnership[number] {
		return nil, nil
	}
	record := policy.NewManagedIssueRecord(fmt.Sprintf("I_%d", number), number)
	return []gh.IssueComment{{ID: int64(number), Body: policy.RenderManagedIssueRecord(record), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}}, nil
}

func TestPullRequestTransitionUsesDurableOwnershipAfterBodyAndLabelEdits(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7")
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{
		7: {Number: 7, NodeID: "I_7", Body: "form headings removed after work opened", State: "open", Labels: []string{"needs:discussion"}},
	}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.added) != 1 || stub.added[0] != 7 || result.Report == nil || result.Report.Status != operation.ReportCompleted {
		t.Fatalf("added=%v report=%+v", stub.added, result.Report)
	}
}

func TestPullRequestTransitionIsNoOpDuringShadowMigration(t *testing.T) {
	input, _, _ := transitionFixture(t, "Refs #7")
	cfg, err := config.Load(input.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Delivery.Authority = config.DeliveryAuthorityShadow
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.ConfigPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		t.Fatal("shadow transition must not construct a GitHub client")
		return nil, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil || result.Report == nil || result.Report.Status != operation.ReportCompleted || len(result.Operations) != 0 || len(result.Warnings) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestPullRequestTransitionRefusesMissingDurableOwnershipBeforeWriting(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7")
	stub := &transitionGitHub{
		deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{7: {Number: 7, NodeID: "I_7", State: "open"}}, omitOwnership: map[int]bool{7: true},
	}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || len(stub.added) != 0 || result.Report == nil || result.Report.Status != operation.ReportRefused {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func (stub *transitionGitHub) AddIssueLabelsContext(_ context.Context, _ string, number int, _ []string) error {
	stub.added = append(stub.added, number)
	return stub.addErr[number]
}

func (stub *transitionGitHub) CloseIssueContext(_ context.Context, _ string, number int) error {
	stub.closed = append(stub.closed, number)
	if err := stub.closeErr[number]; err != nil {
		return err
	}
	issue := stub.issues[number]
	issue.State = "closed"
	stub.issues[number] = issue
	return nil
}

func (stub *transitionGitHub) RemoveIssueLabelContext(_ context.Context, _ string, number int, _ string) error {
	stub.removed = append(stub.removed, number)
	if err := stub.removeErr[number]; err != nil {
		return err
	}
	issue := stub.issues[number]
	issue.Labels = nil
	stub.issues[number] = issue
	return nil
}

func (stub *transitionGitHub) UpdateIssueCommentReturningContext(ctx context.Context, repo string, id int64, body string) (gh.IssueComment, error) {
	if stub.checkpointErr != nil {
		return gh.IssueComment{}, stub.checkpointErr
	}
	return stub.deliveryRecordGitHubStub.UpdateIssueCommentReturningContext(ctx, repo, id, body)
}

func TestPullRequestTransitionApplyIsIdempotent(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{
		7: {Number: 7, State: "open", Labels: []string{"needs:review"}},
		8: {Number: 8, State: "closed"},
	}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.added) != 0 || result.Report == nil || result.Report.Status != operation.ReportCompleted {
		t.Fatalf("added=%v report=%+v", stub.added, result.Report)
	}
	for _, result := range result.Report.Operations {
		if result.Status != operation.StatusUnchanged {
			t.Fatalf("operation = %+v", result)
		}
	}
}

func TestPullRequestTransitionPreflightsAllIssuesBeforeWriting(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}}, getErr: map[int]error{8: errors.New("boom")}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || len(stub.added) != 0 || result.Report == nil || result.Report.Status != operation.ReportFailed {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionReportsPartialApply(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}, 8: {Number: 8, State: "open"}}, addErr: map[int]error{8: errors.New("boom")}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportPartial || len(stub.added) != 2 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionRefusesStaleEvent(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7")
	latest.HeadSHA = "new-head"
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.added) != 0 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionIgnoresEditedReferencesAfterDeliverySnapshot(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7")
	latest.Body = "Refs #8"
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil || result.Report == nil || result.Report.Status != operation.ReportCompleted || len(stub.added) != 1 || stub.added[0] != 7 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionRefusesReferencedPullRequest(t *testing.T) {
	input, latest, transport := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{deliveryRecordGitHubStub: transport, pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}, 8: {Number: 8, State: "open", PullRequest: true}}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.added) != 0 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPromotionTransitionClosesDeliveredIssuesAndCommitsCursorLast(t *testing.T) {
	input, latest, stub := promotionTransitionFixture(t)
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.closed) != 1 || stub.closed[0] != 7 || len(stub.removed) != 1 || len(stub.created) != 1 || len(stub.updated) != 1 {
		t.Fatalf("closed=%v removed=%v created=%d updated=%d", stub.closed, stub.removed, len(stub.created), len(stub.updated))
	}
	if result.Report == nil || result.Report.Status != operation.ReportCompleted || len(result.Report.Operations) != 4 {
		t.Fatalf("result = %+v", result)
	}
	for _, item := range result.Report.Operations {
		if item.Status != operation.StatusApplied {
			t.Fatalf("operation = %+v", item)
		}
	}

	// A human reopen after the durable cursor commit must survive duplicate
	// event delivery. The duplicate resolves from the integration record before
	// reading or mutating delivered issues.
	issue := stub.issues[7]
	issue.State = "open"
	stub.issues[7] = issue
	stub.omitOwnership = map[int]bool{7: true}
	closedBefore := len(stub.closed)
	result, err = workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.closed) != closedBefore || len(result.Operations) != 0 || result.Report == nil || result.Report.Status != operation.ReportCompleted {
		t.Fatalf("duplicate result=%+v closed=%v latest=%+v", result, stub.closed, latest)
	}
}

func TestPromotionTransitionPartialFailureRetriesSatisfiedIssueMutation(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	stub.removeErr = map[int]error{7: errors.New("remove failed")}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportPartial || len(stub.created) != 0 {
		t.Fatalf("first result=%+v err=%v created=%d", result, err, len(stub.created))
	}
	if stub.issues[7].State != "closed" {
		t.Fatalf("close mutation was not preserved: %+v", stub.issues[7])
	}
	issue := stub.issues[7]
	issue.State = "open"
	stub.issues[7] = issue
	stub.removeErr = nil
	result, err = workflow.RunContext(context.Background(), input)
	if err != nil || result.Report == nil || result.Report.Status != operation.ReportCompleted || len(stub.closed) != 2 || len(stub.created) != 1 || len(stub.updated) != 1 {
		t.Fatalf("retry result=%+v err=%v closed=%v", result, err, stub.closed)
	}
}

func TestPromotionTransitionCreatesRecordFromIncompleteNewestWindow(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	stub.newestIncomplete = true
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil || result.Report == nil || result.Report.Status != operation.ReportCompleted || len(stub.created) != 1 || len(stub.updated) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPromotionTransitionReportsRetryableGitHubFailure(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	stub.closeErr = map[int]error{7: gh.APIError{Method: http.MethodPatch, Path: "/repos/example/repo/issues/7", StatusCode: http.StatusServiceUnavailable}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || len(result.Report.Operations) == 0 || result.Report.Operations[0].Error == nil || !result.Report.Operations[0].Error.Retryable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPromotionTransitionSkipsManualCloseButRemovesAwaitingReviewIndex(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	issue := stub.issues[7]
	issue.State = "closed"
	stub.issues[7] = issue
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.closed) != 0 || len(stub.removed) != 1 || result.Report.Operations[0].Status != operation.StatusUnchanged {
		t.Fatalf("result=%+v closed=%v removed=%v", result, stub.closed, stub.removed)
	}
}

func TestPromotionTransitionAdoptsOrphanRecordAfterCursorFailure(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	stub.checkpointErr = errors.New("checkpoint unavailable")
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportPartial || len(stub.created) != 1 || len(stub.updated) != 0 {
		t.Fatalf("first result=%+v err=%v created=%d updated=%d", result, err, len(stub.created), len(stub.updated))
	}
	firstDigest := result.BaseIntegrationDigest
	stub.checkpointErr = nil
	stub.newestIncomplete = true
	input.RunID++
	result, err = workflow.RunContext(context.Background(), input)
	if err != nil || result.Report == nil || result.Report.Status != operation.ReportCompleted || len(stub.created) != 1 || len(stub.updated) != 1 || result.BaseIntegrationDigest != firstDigest {
		t.Fatalf("retry result=%+v err=%v created=%d updated=%d", result, err, len(stub.created), len(stub.updated))
	}
	if result.Report.Operations[len(result.Report.Operations)-2].Resource != "baseIntegration:"+result.BaseIntegrationDigest {
		t.Fatalf("retry report did not adopt exact orphan digest: %+v", result.Report)
	}
}

func TestPromotionTransitionRefusesStaleMergedRevisionBeforeWriting(t *testing.T) {
	input, _, stub := promotionTransitionFixture(t)
	stub.pr.HeadSHA = strings.Repeat("9", 40)
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.closed) != 0 || len(stub.created) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func promotionTransitionFixture(t *testing.T) (PullRequestTransitionInput, gh.PullRequest, *transitionGitHub) {
	t.Helper()
	root := t.TempDir()
	cfg, locator, checkpointBody := deliveryWorkflowFixture(t)
	checkpoint, err := delivery.ParseCheckpointIndex(locator, delivery.StoredComment{
		Comment: locator.Checkpoint, Body: checkpointBody,
		AuthorLogin: delivery.TrustedAuthorLogin, AuthorType: delivery.TrustedAuthorType,
	})
	if err != nil {
		t.Fatal(err)
	}
	ownership := policy.NewManagedIssueRecord("I_7", 7)
	appendPlan, err := delivery.PlanStagedWorkAppend(delivery.Snapshot{Locator: locator, Checkpoint: checkpoint}, delivery.StagedWorkAppendInput{
		PullRequest: delivery.ResourceIdentity{Number: 20, NodeID: "PR_20"}, StagingBranch: cfg.Repository.StagingBranch,
		BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40), MergeRevision: strings.Repeat("3", 40),
		MergedAt: "2026-07-14T01:00:00Z", Writer: delivery.WriterProvenance{Workflow: "Delivery Recorder", RunID: 1},
		Issues: []delivery.ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: ownership.Digest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := delivery.FinalizeStagedWorkAppend(appendPlan, checkpoint, delivery.CommentIdentity{DatabaseID: 201, NodeID: "IC_201"})
	if err != nil {
		t.Fatal(err)
	}
	revision := delivery.PromotionRevision{
		RepositoryNodeID: locator.Repository.NodeID, PullRequest: delivery.ResourceIdentity{Number: 50, NodeID: "PR_50"},
		BaseSHA: strings.Repeat("4", 40), HeadSHA: strings.Repeat("3", 40),
	}
	sealPlan, err := delivery.PlanPromotionSeal(delivery.Snapshot{
		Locator: locator, Checkpoint: staged.Checkpoint, StagedWork: []delivery.StagedWorkRecord{staged.Record},
	}, delivery.PromotionSealInput{
		Revision: revision, Writer: delivery.WriterProvenance{Workflow: "PR Policy", RunID: 2}, SealedAt: "2026-07-14T02:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	seal, err := delivery.FinalizePromotionSeal(sealPlan, staged.Checkpoint, delivery.CommentIdentity{DatabaseID: 202, NodeID: "IC_202"})
	if err != nil {
		t.Fatal(err)
	}
	configContent, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "baton.yml")
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "event.json")
	event := `{"action":"closed","repository":{"full_name":"example/repo"},"pull_request":{"number":50,"node_id":"PR_50","title":"Promote","body":"","state":"closed","merged":true,"merged_at":"2026-07-14T03:00:00Z","merge_commit_sha":"` + strings.Repeat("5", 40) + `","base":{"ref":"main","sha":"` + revision.BaseSHA + `","repo":{"full_name":"example/repo"}},"head":{"ref":"agent","sha":"` + revision.HeadSHA + `","repo":{"full_name":"example/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	input := PullRequestTransitionInput{EventPath: eventPath, ConfigPath: configPath, Apply: true, WorkflowName: "Work Item Transition", RunID: 99}
	latest := gh.PullRequest{
		Number: 50, NodeID: "PR_50", Title: "Promote", BaseRef: cfg.Repository.BaseBranch, HeadRef: cfg.Repository.StagingBranch,
		BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", BaseSHA: revision.BaseSHA, HeadSHA: revision.HeadSHA,
		MergeRevision: strings.Repeat("5", 40), State: "closed", Merged: true,
	}
	transport := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues:     map[int]gh.Issue{locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true}},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: seal.CheckpointBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		records: map[int64]gh.IssueComment{
			201: {ID: 201, NodeID: "IC_201", IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: staged.RecordBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
			202: {ID: 202, NodeID: "IC_202", IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: seal.RecordBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		},
	}
	stub := &transitionGitHub{
		deliveryRecordGitHubStub: transport, pr: latest,
		issues: map[int]gh.Issue{7: {Number: 7, NodeID: "I_7", State: "open", Labels: []string{cfg.IssuePolicy.AwaitingReviewLabel}}},
	}
	return input, latest, stub
}

func transitionFixture(t *testing.T, body string) (PullRequestTransitionInput, gh.PullRequest, *deliveryRecordGitHubStub) {
	t.Helper()
	root := t.TempDir()
	cfg, locator, checkpointBody := deliveryWorkflowFixture(t)
	checkpoint, err := delivery.ParseCheckpointIndex(locator, delivery.StoredComment{Comment: locator.Checkpoint, Body: checkpointBody, AuthorLogin: delivery.TrustedAuthorLogin, AuthorType: delivery.TrustedAuthorType})
	if err != nil {
		t.Fatal(err)
	}
	issueNumbers := uniqueSortedNumbers(append(policy.ExtractReferenceIssueNumbersForPolicy(body, cfg.PRPolicy.RequiredReferenceKeyword), policy.ExtractReferenceIssueNumbersForPolicy("", cfg.PRPolicy.RequiredReferenceKeyword)...))
	issueReferences := make([]delivery.ManagedIssueReference, 0, len(issueNumbers))
	for _, number := range issueNumbers {
		record := policy.NewManagedIssueRecord(fmt.Sprintf("I_%d", number), number)
		issueReferences = append(issueReferences, delivery.ManagedIssueReference{Number: number, NodeID: record.IssueNodeID, OwnershipDigest: record.Digest})
	}
	appendPlan, err := delivery.PlanStagedWorkAppend(delivery.Snapshot{Locator: locator, Checkpoint: checkpoint}, delivery.StagedWorkAppendInput{
		PullRequest: delivery.ResourceIdentity{Number: 42, NodeID: "PR_42"}, StagingBranch: cfg.Repository.StagingBranch,
		BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40), MergeRevision: strings.Repeat("3", 40), MergedAt: "2026-07-14T01:00:00Z",
		Issues: issueReferences, Writer: delivery.WriterProvenance{Workflow: "Delivery Recorder", RunID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := delivery.FinalizeStagedWorkAppend(appendPlan, checkpoint, delivery.CommentIdentity{DatabaseID: 201, NodeID: "IC_201"})
	if err != nil {
		t.Fatal(err)
	}
	configContent, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "baton.yml")
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "event.json")
	event := `{"action":"closed","repository":{"full_name":"example/repo"},"pull_request":{"number":42,"node_id":"PR_42","title":"work","body":"` + body + `","state":"closed","merged":true,"merged_at":"2026-07-14T01:00:00Z","merge_commit_sha":"` + strings.Repeat("3", 40) + `","base":{"ref":"agent","sha":"` + strings.Repeat("1", 40) + `","repo":{"full_name":"example/repo"}},"head":{"ref":"agent-work/42","sha":"` + strings.Repeat("2", 40) + `","repo":{"full_name":"example/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	input := PullRequestTransitionInput{EventPath: eventPath, ConfigPath: configPath, Apply: true, WorkflowName: "Delivery Recorder", RunID: 99}
	latest := gh.PullRequest{Number: 42, NodeID: "PR_42", Title: "work", Body: body, BaseRef: "agent", BaseSHA: strings.Repeat("1", 40), HeadRef: "agent-work/42", BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", HeadSHA: strings.Repeat("2", 40), MergeRevision: strings.Repeat("3", 40), State: "closed", Merged: true}
	transport := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues:     map[int]gh.Issue{locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true}},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: commit.CheckpointBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		records:    map[int64]gh.IssueComment{201: {ID: 201, NodeID: "IC_201", IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: commit.RecordBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}}},
	}
	return input, latest, transport
}

func TestPullRequestTransitionDoesNotConstructClientForNonManagedFlows(t *testing.T) {
	dir := t.TempDir()
	configPath := writeWorkflowConfig(t, dir)
	tests := []struct {
		name  string
		event string
		flow  policy.PRFlow
	}{
		{name: "unmanaged", event: `{"action":"closed","repository":{"full_name":"example/repo"},"pull_request":{"number":1,"merged":true,"base":{"ref":"agent","repo":{"full_name":"example/repo"}},"head":{"ref":"feature/1","repo":{"full_name":"example/repo"}}}}`, flow: policy.PRFlowUnmanaged},
		{name: "misrouted", event: `{"action":"closed","repository":{"full_name":"example/repo"},"pull_request":{"number":2,"merged":true,"base":{"ref":"release","repo":{"full_name":"example/repo"}},"head":{"ref":"agent-work/2","repo":{"full_name":"example/repo"}}}}`, flow: policy.PRFlowMisroutedWork},
		{name: "indeterminate", event: `{"action":"closed","pull_request":{"number":3,"merged":true,"base":{"ref":"agent"},"head":{"ref":"agent-work/3"}}}`, flow: policy.PRFlowIndeterminate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eventPath := filepath.Join(dir, test.name+".json")
			if err := os.WriteFile(eventPath, []byte(test.event), 0o600); err != nil {
				t.Fatal(err)
			}
			workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
				t.Fatal("non-managed transition must not construct a GitHub client")
				return nil, nil
			}}
			result, err := workflow.RunContext(context.Background(), PullRequestTransitionInput{EventPath: eventPath, ConfigPath: configPath, Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Flow != test.flow || result.Report == nil || len(result.Report.Operations) != 0 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}
