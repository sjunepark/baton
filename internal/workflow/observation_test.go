package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/repository"
)

type observationGitHubStub struct {
	*deliveryRecordGitHubStub
	mu           sync.Mutex
	issues       []gh.Issue
	pullRequests []gh.PullRequest
	branch       *gh.BranchHealth
	checkState   string
	err          error
	checkCalls   int
}

func (stub *observationGitHubStub) ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error) {
	issues := append([]gh.Issue(nil), stub.issues...)
	for index := range issues {
		if issues[index].NodeID == "" {
			issues[index].NodeID = fmt.Sprintf("I_%d", issues[index].Number)
		}
	}
	return issues, stub.err
}

func (stub *observationGitHubStub) ListIssueCommentsContext(_ context.Context, _ string, number int) ([]gh.IssueComment, error) {
	record := policy.NewManagedIssueRecord(fmt.Sprintf("I_%d", number), number)
	return []gh.IssueComment{{ID: int64(number), Body: policy.RenderManagedIssueRecord(record), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}}, stub.err
}

func (stub *observationGitHubStub) ListOpenPullRequestsContext(context.Context, string, string) ([]gh.PullRequest, error) {
	return normalizedTestPullRequests(stub.pullRequests), stub.err
}

func normalizedTestPullRequests(pullRequests []gh.PullRequest) []gh.PullRequest {
	result := append([]gh.PullRequest(nil), pullRequests...)
	for index := range result {
		if result[index].BaseRepositoryFullName == "" {
			result[index].BaseRepositoryFullName = "example/repo"
		}
		if result[index].HeadRepositoryFullName == "" {
			result[index].HeadRepositoryFullName = result[index].BaseRepositoryFullName
		}
	}
	return result
}

func (stub *observationGitHubStub) GetCheckRollupContext(_ context.Context, _ string, number int, _ string) (gh.CheckRollup, error) {
	if number == 0 {
		state := ""
		if stub.branch != nil {
			state = stub.branch.CheckState
		}
		return gh.CheckRollup{State: state, Complete: true}, stub.err
	}
	stub.mu.Lock()
	stub.checkCalls++
	stub.mu.Unlock()
	return gh.CheckRollup{State: stub.checkState, Complete: true}, stub.err
}

func (stub *observationGitHubStub) GetPullRequestContext(_ context.Context, _ string, number int) (gh.PullRequest, error) {
	for _, pullRequest := range normalizedTestPullRequests(stub.pullRequests) {
		if pullRequest.Number == number {
			if pullRequest.BaseSHA == "" {
				pullRequest.BaseSHA = "base"
			}
			if pullRequest.Mergeable == "" {
				pullRequest.Mergeable = "mergeable"
			}
			if pullRequest.MergeState == "" {
				pullRequest.MergeState = "clean"
			}
			return pullRequest, stub.err
		}
	}
	return gh.PullRequest{}, stub.err
}

func (stub *observationGitHubStub) GetBranchContext(_ context.Context, _ string, ref string) (gh.Branch, error) {
	return gh.Branch{Ref: ref, SHA: "branch-" + ref}, stub.err
}

func (stub *observationGitHubStub) GetClassicBranchRulesContext(_ context.Context, _ string, ref string) (gh.BranchRules, error) {
	return gh.BranchRules{Branch: ref}, stub.err
}

func (stub *observationGitHubStub) GetEffectiveBranchRulesContext(_ context.Context, _ string, ref string) (gh.BranchRules, error) {
	return gh.BranchRules{Branch: ref}, stub.err
}

func (stub *observationGitHubStub) GetReviewThreadsContext(context.Context, string, int) (gh.ReviewThreadResult, error) {
	return gh.ReviewThreadResult{Complete: true}, stub.err
}

func (stub *observationGitHubStub) ListPullRequestReviewsContext(context.Context, string, int) ([]gh.PullRequestReview, error) {
	return nil, stub.err
}

func (stub *observationGitHubStub) GetRequestedReviewersContext(context.Context, string, int) ([]gh.ReviewRequest, error) {
	return nil, stub.err
}

func (stub *observationGitHubStub) GetBranchHealthContext(context.Context, string, string) (*gh.BranchHealth, error) {
	return stub.branch, stub.err
}

func TestObservationWorkflowBuildsQueueFromTransportFacts(t *testing.T) {
	client := &observationGitHubStub{
		issues: []gh.Issue{{Number: 7, Title: "Fix", URL: "https://example/7", Labels: []string{"agent:ready-bounded"}}},
		branch: &gh.BranchHealth{Ref: "agent", SHA: "abc", CheckState: "success"},
	}
	workflow := observationWorkflowForTest(client)

	snapshot, err := workflow.Queue(RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Repo != "example/repo" || len(snapshot.Issues) != 1 || !snapshot.Issues[0].Eligible {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if client.checkCalls != 0 {
		t.Fatalf("queue check calls = %d, want 0", client.checkCalls)
	}
}

func TestObservationWorkflowNextEnrichesChecksBeforeRecommendation(t *testing.T) {
	client := &observationGitHubStub{
		pullRequests: []gh.PullRequest{{Number: 12, Title: "PR", URL: "https://example/pr/12", BaseRef: "agent", HeadRef: "agent-work/12", HeadSHA: "abc"}},
		branch:       &gh.BranchHealth{Ref: "agent", SHA: "def", CheckState: "success"},
		checkState:   "failure",
	}
	workflow := observationWorkflowForTest(client)

	next, err := workflow.Next(RepositoryInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.SelectedAction != "pr-followup" || len(next.Candidates) != 1 || next.Candidates[0].CheckState != "failure" {
		t.Fatalf("next = %+v", next)
	}
	if client.checkCalls != 1 {
		t.Fatalf("check calls = %d, want 1", client.checkCalls)
	}
}

func TestObservationWorkflowFiltersUnmanagedPullRequestsBeforeEnrichment(t *testing.T) {
	client := &observationGitHubStub{
		pullRequests: []gh.PullRequest{{Number: 13, Title: "Ordinary PR", BaseRef: "agent", HeadRef: "feature/13", HeadSHA: "abc"}},
		branch:       &gh.BranchHealth{Ref: "agent", SHA: "def", CheckState: "success"},
	}
	next, err := observationWorkflowForTest(client).Next(RepositoryInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Candidates) != 0 || client.checkCalls != 0 {
		t.Fatalf("next=%+v checkCalls=%d, want unmanaged no-op before enrichment", next, client.checkCalls)
	}
}

func TestObservationWorkflowReturnsTypedAuthError(t *testing.T) {
	workflow := observationWorkflowForTest(&observationGitHubStub{})
	workflow.newClient = func(context.Context, RepositoryInput) (ObservationGitHub, error) {
		return nil, errors.New("credential command included secret")
	}

	_, err := workflow.Queue(RepositoryInput{})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Auth || applicationError.Message != "GitHub credentials are not available" {
		t.Fatalf("error = %+v", applicationError)
	}
	if applicationError.Error() == applicationError.Cause.Error() {
		t.Fatal("public message exposes diagnostic cause")
	}
}

func TestObservationWorkflowPreservesGitHubCategoryForHTTPAuthenticationFailure(t *testing.T) {
	client := &observationGitHubStub{err: gh.APIError{Method: "GET", Path: "/repos/example/repo/issues", Status: "401 Unauthorized", StatusCode: 401, RequestID: "request-1"}}
	workflow := observationWorkflowForTest(client)
	_, err := workflow.Queue(RepositoryInput{})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.GitHub || applicationError.ExitCode() != 5 || applicationError.HTTPStatus != 401 || applicationError.RequestID != "request-1" {
		t.Fatalf("error = %+v", applicationError)
	}
	if applicationError.Retryable {
		t.Fatalf("internal retryability should remain accurate: %+v", applicationError)
	}
}

func observationWorkflowForTest(client ObservationGitHub) ObservationWorkflow {
	cfg := config.DefaultConfig()
	cfg.Repository.BaseBranch = cfg.Repository.StagingBranch
	return ObservationWorkflow{
		resolve: func(context.Context, repository.Options) (repository.Context, error) {
			return repository.Context{Repository: "example/repo", Config: cfg}, nil
		},
		newClient: func(context.Context, RepositoryInput) (ObservationGitHub, error) { return client, nil },
	}
}

type concurrentObservationGitHub struct {
	observationGitHubStub
	started  chan int
	release  chan struct{}
	mu       sync.Mutex
	inFlight int
	max      int
}

type unmanagedIssueObservationGitHub struct{ observationGitHubStub }

func (*unmanagedIssueObservationGitHub) ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error) {
	return nil, nil
}

func TestObservationWorkflowDoesNotEnrollIssueFromLabelsAlone(t *testing.T) {
	client := &unmanagedIssueObservationGitHub{observationGitHubStub: observationGitHubStub{
		issues: []gh.Issue{{Number: 7, NodeID: "I_7", Title: "Coincidental labels", Labels: []string{"agent:ready-bounded", "baton:managed"}}},
		branch: &gh.BranchHealth{Ref: "agent", SHA: "abc", CheckState: "success"},
	}}
	snapshot, err := observationWorkflowForTest(client).Queue(RepositoryInput{})
	if err == nil {
		t.Fatalf("snapshot = %+v, want invalid index-without-record failure", snapshot)
	}

	client.issues[0].Labels = []string{"agent:ready-bounded"}
	snapshot, err = observationWorkflowForTest(client).Queue(RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Issues) != 0 {
		t.Fatalf("labels alone enrolled issue: %+v", snapshot.Issues)
	}
}

func (client *concurrentObservationGitHub) GetCheckRollupContext(ctx context.Context, _ string, number int, _ string) (gh.CheckRollup, error) {
	client.mu.Lock()
	client.inFlight++
	if client.inFlight > client.max {
		client.max = client.inFlight
	}
	client.mu.Unlock()
	client.started <- number
	select {
	case <-client.release:
	case <-ctx.Done():
		return gh.CheckRollup{}, ctx.Err()
	}
	client.mu.Lock()
	client.inFlight--
	client.mu.Unlock()
	return gh.CheckRollup{State: "success"}, nil
}

func TestObservationCheckEnrichmentIsBoundedAndPreservesOrder(t *testing.T) {
	client := &concurrentObservationGitHub{started: make(chan int, 6), release: make(chan struct{}, 6)}
	prs := make([]gh.PullRequest, 6)
	for index := range prs {
		prs[index] = gh.PullRequest{Number: index + 1, HeadSHA: "sha"}
	}
	done := make(chan error, 1)
	go func() {
		done <- (ObservationWorkflow{enrichmentLimit: 3}).enrichChecks(context.Background(), client, "example/repo", prs)
	}()
	for range 3 {
		<-client.started
	}
	select {
	case number := <-client.started:
		t.Fatalf("fourth request started before a worker was released: %d", number)
	default:
	}
	for range 3 {
		client.release <- struct{}{}
	}
	for range 3 {
		<-client.started
		client.release <- struct{}{}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	max := client.max
	client.mu.Unlock()
	if max != 3 {
		t.Fatalf("max concurrent checks = %d, want 3", max)
	}
	for index, pr := range prs {
		if pr.Number != index+1 || pr.CheckState != "success" {
			t.Fatalf("prs[%d]=%+v", index, pr)
		}
	}
}

func TestObservationCheckEnrichmentStopsSchedulingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &observationGitHubStub{}
	prs := []gh.PullRequest{{Number: 1, HeadSHA: "one"}, {Number: 2, HeadSHA: "two"}}
	err := (ObservationWorkflow{enrichmentLimit: 2}).enrichChecks(ctx, client, "example/repo", prs)
	if !errors.Is(err, context.Canceled) || client.checkCalls != 0 {
		t.Fatalf("err=%v checkCalls=%d", err, client.checkCalls)
	}
}
