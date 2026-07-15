package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/repository"
	"github.com/sjunepark/baton/internal/workitem"
)

type pullRequestGitHub struct {
	pr      gh.PullRequest
	checks  gh.CheckRollup
	threads gh.ReviewThreadResult
	issues  []gh.Issue
}

func (f pullRequestGitHub) GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error) {
	return f.pr, nil
}
func (f pullRequestGitHub) GetCheckRollupContext(context.Context, string, int, string) (gh.CheckRollup, error) {
	return f.checks, nil
}
func (f pullRequestGitHub) GetReviewThreadsContext(context.Context, string, int) (gh.ReviewThreadResult, error) {
	return f.threads, nil
}
func (f pullRequestGitHub) ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error) {
	return f.issues, nil
}
func (f pullRequestGitHub) ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error) {
	return nil, nil
}

func TestPullRequestWorkflowComposesDashboard(t *testing.T) {
	resolveCalls := 0
	workflow := PullRequestWorkflow{newClient: func(context.Context, PullRequestInput) (PullRequestGitHub, error) {
		return pullRequestGitHub{
			pr:      gh.PullRequest{Number: 42, Title: "Refs #7", Body: "Also refs #8 and refs #7", HeadRef: "agent/42", BaseRef: "agent", HeadSHA: "abc"},
			checks:  gh.CheckRollup{State: "failure", Count: 2, Summary: gh.CheckSummary{Failed: 1, Pending: 1}, Complete: true},
			threads: gh.ReviewThreadResult{Count: 3, Summary: gh.ThreadSummary{Total: 3, Unresolved: 2, HumanUnresolved: 1, BotUnresolved: 1}, Complete: true},
		}, nil
	}, resolve: func(context.Context, repository.Options) (repository.Context, error) {
		resolveCalls++
		cfg := config.DefaultConfig()
		return repository.Context{Repository: "example/repo", Config: cfg}, nil
	}, resolveTarget: func(context.Context, repository.Options, string) (repository.Target, error) {
		return repository.Target{Repository: "example/repo"}, nil
	}}
	result, err := workflow.Dashboard(PullRequestInput{Number: 42, Repository: "example/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "pullRequest" || result.Repo != "example/repo" || len(result.ReferencedIssues) != 2 || result.ReferencedIssues[0] != 7 || result.ReferencedIssues[1] != 8 {
		t.Fatalf("dashboard = %#v", result)
	}
	if result.LikelyNextCommand != "baton review-threads 42 --format toon" || !strings.Contains(strings.Join(result.Help, "\n"), "unresolved human comments") {
		t.Fatalf("next=%q help=%v", result.LikelyNextCommand, result.Help)
	}
	if resolveCalls != 1 {
		t.Fatalf("repository context resolved %d times", resolveCalls)
	}
}

func TestPullRequestWorkflowDegradesSafelyWithoutConfig(t *testing.T) {
	workflow := PullRequestWorkflow{newClient: func(context.Context, PullRequestInput) (PullRequestGitHub, error) {
		return pullRequestGitHub{
			pr:      gh.PullRequest{Number: 42, Title: "Refs #7", HeadSHA: "abc"},
			checks:  gh.CheckRollup{State: "success", Complete: true},
			threads: gh.ReviewThreadResult{Complete: true},
		}, nil
	}, resolve: func(context.Context, repository.Options) (repository.Context, error) {
		return repository.Context{}, config.ErrConfigNotFound
	}, resolveTarget: func(context.Context, repository.Options, string) (repository.Target, error) {
		return repository.Target{Repository: "example/repo"}, nil
	}}

	result, err := workflow.Dashboard(PullRequestInput{Number: 42, Repository: "example/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReferencedIssues) != 1 || result.ReferencedIssues[0] != 7 {
		t.Fatalf("referenced issues = %v", result.ReferencedIssues)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, "configuration unavailable") || !strings.Contains(warnings, "issue readiness unavailable") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if len(result.IssueReadiness) != 0 {
		t.Fatalf("issue readiness = %+v", result.IssueReadiness)
	}
}

func TestPullRequestDashboardDoesNotProjectIncompleteSummaries(t *testing.T) {
	workflow := PullRequestWorkflow{newClient: func(context.Context, PullRequestInput) (PullRequestGitHub, error) {
		return pullRequestGitHub{
			pr:      gh.PullRequest{Number: 42, HeadSHA: "abc"},
			checks:  gh.CheckRollup{Summary: gh.CheckSummary{Failed: 1}, Warnings: []string{"truncated"}},
			threads: gh.ReviewThreadResult{Summary: gh.ThreadSummary{HumanUnresolved: 1}, Warnings: []string{"truncated"}},
		}, nil
	}, resolve: func(context.Context, repository.Options) (repository.Context, error) {
		return repository.Context{Repository: "example/repo", Config: config.DefaultConfig()}, nil
	}}

	result, err := workflow.Dashboard(PullRequestInput{Number: 42, Repository: "example/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Checks.Summary.Failed != 0 || result.ReviewThreads.Summary.HumanUnresolved != 0 || result.LikelyNextCommand == "baton next --format toon" || len(result.Warnings) != 2 {
		t.Fatalf("dashboard = %+v", result)
	}
}

func TestPullRequestLikelyNextCommandForUnavailableSummaries(t *testing.T) {
	if got := PullRequestLikelyNextCommand(42, gh.CheckSummary{}, gh.ThreadSummary{}, false, true); got != "baton checks 42 --format toon" {
		t.Fatalf("missing checks next = %q", got)
	}
	if got := PullRequestLikelyNextCommand(42, gh.CheckSummary{}, gh.ThreadSummary{}, true, false); got != "baton review-threads 42 --format toon" {
		t.Fatalf("missing reviews next = %q", got)
	}
}

func TestPullRequestLikelyNextCommandInspectsUnknownReviewActors(t *testing.T) {
	threads := gh.ThreadSummary{UnknownUnresolved: 1}
	if got := PullRequestLikelyNextCommand(42, gh.CheckSummary{}, threads, true, true); got != "baton review-threads 42 --format toon" {
		t.Fatalf("next = %q", got)
	}
}

func TestBuildIssueReadinessUsesLabels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.IssuePolicy.ImplementationLabels = []string{"agent:ready-trivial"}
	cfg.IssuePolicy.CommentOnlyLabels = []string{"agent:needs-investigation"}
	cfg.IssuePolicy.SkipLabels = []string{"needs-info"}
	readiness := buildIssueReadiness([]int{7, 8, 9}, []queue.Issue{
		{Number: 7, Labels: []string{"agent:ready-trivial"}},
		{Number: 8, Labels: []string{"agent:ready-trivial", "needs-info"}},
	}, cfg, 0, 0)
	if len(readiness) != 3 || !readiness[0].Ready || readiness[1].Ready || readiness[2].Found {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestBuildIssueReadinessDoesNotReportOpenUnmanagedIssueAsClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	readiness := buildIssueReadinessWithOpenIssues([]int{7}, nil, map[int]struct{}{7: {}}, cfg, 0, 0)
	if len(readiness) != 1 || !readiness[0].Found || readiness[0].State != workitem.StateBlocked || readiness[0].Ready || !strings.Contains(strings.Join(readiness[0].Reasons, " "), "not Baton-managed") {
		t.Fatalf("readiness = %+v", readiness)
	}
}

func TestBuildIssueReadinessUsesSharedActiveWorkPRState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.IssuePolicy.ImplementationLabels = []string{"agent:ready-trivial"}
	readiness := buildIssueReadiness([]int{7}, []queue.Issue{{Number: 7, Labels: []string{"agent:ready-trivial"}}}, cfg, 42, 0)
	if len(readiness) != 1 || readiness[0].Ready || readiness[0].State != workitem.StateActiveWorkPR {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestBuildIssueReadinessDistinguishesMergedAndClosedUnmergedPRs(t *testing.T) {
	cfg := config.DefaultConfig()
	issue := []queue.Issue{{Number: 7, Labels: []string{"agent:ready-bounded"}}}
	merged := buildIssueReadiness([]int{7}, issue, cfg, 0, 42)
	closed := buildIssueReadiness([]int{7}, issue, cfg, 0, 0)
	if merged[0].State != workitem.StateAwaitingReview || closed[0].State != workitem.StateReady || !closed[0].Ready {
		t.Fatalf("merged=%+v closed=%+v", merged, closed)
	}
}

func TestMergedWorkIssueReferencesUseCoveredDeliveryRecord(t *testing.T) {
	store := delivery.Snapshot{
		Checkpoint: delivery.DeliveryCheckpoint{
			Coverage:      delivery.StagingCoverage{RecordSequence: 1},
			ActiveRecords: []delivery.RecordReference{{Kind: delivery.RecordStagedWork, Sequence: 1, Digest: "sha256:record"}},
		},
		StagedWork: []delivery.StagedWorkRecord{{
			RecordHeader: delivery.RecordHeader{Sequence: 1, Digest: "sha256:record"},
			PullRequest:  delivery.ResourceIdentity{Number: 42},
			Issues:       []delivery.ManagedIssueReference{{Number: 7}, {Number: 8}},
		}},
	}
	references, err := mergedWorkIssueReferencesFromStore(store, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 2 || references[0] != 7 || references[1] != 8 {
		t.Fatalf("references = %v", references)
	}
	if _, err := mergedWorkIssueReferencesFromStore(store, 99); err == nil {
		t.Fatal("missing staged-work record should fail instead of using mutable PR references")
	}
}

func TestMergedWorkDashboardRejectsMutableReferencesWithoutSealedRecord(t *testing.T) {
	cfg := config.DefaultConfig()
	result := PullRequestDashboard{
		Repo: "example/repo", PullRequest: queue.PullRequest{Number: 42, Merged: true, Ownership: policy.PRFlowWork},
		ReferencedIssues: []int{99},
	}
	PullRequestWorkflow{}.addIssueReadiness(context.Background(), &result, pullRequestSession{repo: result.Repo, client: pullRequestGitHub{}, config: &cfg})
	if result.ReferencedIssues != nil || len(result.IssueReadiness) != 0 || !strings.Contains(strings.Join(result.Warnings, " "), "sealed delivery authority is required") {
		t.Fatalf("dashboard = %+v", result)
	}
}

func TestMergedWorkDashboardRejectsMutableReferencesWithoutConfig(t *testing.T) {
	result := PullRequestDashboard{
		Repo: "example/repo", PullRequest: queue.PullRequest{Number: 42, Merged: true, Ownership: policy.PRFlowWork},
		ReferencedIssues: []int{99},
	}
	PullRequestWorkflow{}.addIssueReadiness(context.Background(), &result, pullRequestSession{repo: result.Repo, client: pullRequestGitHub{}})
	if result.ReferencedIssues != nil || len(result.IssueReadiness) != 0 || !strings.Contains(strings.Join(result.Warnings, " "), "sealed delivery evidence cannot be verified") {
		t.Fatalf("dashboard = %+v", result)
	}
}

func TestPullRequestDashboardV2GoldenContract(t *testing.T) {
	cfg := config.DefaultConfig()
	pullRequest := queue.PullRequest{
		Number: 42, Title: "Edited references", URL: "https://github.com/example/repo/pull/42",
		Body: "Refs #99", BaseRef: "agent", HeadRef: "agent-work/42", HeadSHA: "head-42", CheckState: "success",
		State: "closed", Merged: true, Ownership: policy.PRFlowWork,
	}
	result := buildPullRequestDashboard("example/repo", pullRequest, cfg)
	store := delivery.Snapshot{
		Checkpoint: delivery.DeliveryCheckpoint{
			Coverage:      delivery.StagingCoverage{RecordSequence: 1},
			ActiveRecords: []delivery.RecordReference{{Kind: delivery.RecordStagedWork, Sequence: 1, Digest: "sha256:record"}},
		},
		StagedWork: []delivery.StagedWorkRecord{{
			RecordHeader: delivery.RecordHeader{Sequence: 1, Digest: "sha256:record"},
			PullRequest:  delivery.ResourceIdentity{Number: 42},
			Issues:       []delivery.ManagedIssueReference{{Number: 7}},
		}},
	}
	var err error
	result.ReferencedIssues, err = mergedWorkIssueReferencesFromStore(store, pullRequest.Number)
	if err != nil {
		t.Fatal(err)
	}
	result.IssueReadiness = buildIssueReadiness(result.ReferencedIssues, []queue.Issue{{Number: 7, Labels: []string{"agent:ready-bounded"}}}, cfg, 0, pullRequest.Number)

	actual, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "pull-request-v2-merged-work.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actualValue, expectedValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("pull request dashboard contract\nactual: %s\nexpected: %s", actual, expected)
	}
}

func TestReviewThreadBodyTruncation(t *testing.T) {
	result := gh.ReviewThreadResult{PRNumber: 12, Threads: []gh.ReviewThread{{Comments: []gh.ReviewComment{{Body: "abcdef"}}}}}
	truncated := truncateReviewThreadBodies(result, 3, false)
	comment := truncated.Threads[0].Comments[0]
	if comment.Body != "abc" || comment.BodyChars != 6 || !comment.BodyTruncated || comment.FullCommand != "baton review-threads 12 --full --json" {
		t.Fatalf("comment = %#v", comment)
	}
}
