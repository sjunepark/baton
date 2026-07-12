package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
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

func TestReviewThreadBodyTruncation(t *testing.T) {
	result := gh.ReviewThreadResult{PRNumber: 12, Threads: []gh.ReviewThread{{Comments: []gh.ReviewComment{{Body: "abcdef"}}}}}
	truncated := truncateReviewThreadBodies(result, 3, false)
	comment := truncated.Threads[0].Comments[0]
	if comment.Body != "abc" || comment.BodyChars != 6 || !comment.BodyTruncated || comment.FullCommand != "baton review-threads 12 --full --json" {
		t.Fatalf("comment = %#v", comment)
	}
}
