package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/snapshot"
)

func TestMergeBranchRulesIncludesClassicProtectionPolicy(t *testing.T) {
	rules := mergeBranchRules(
		gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{{Context: "ruleset"}}, RequiredApprovingReviewCount: 1},
		gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{{Context: "classic"}}, RequiredApprovingReviewCount: 2, StrictRequiredChecks: true, DismissStaleReviews: true, RequireLastPushApproval: true},
	)
	if len(rules.RequiredChecks) != 2 || rules.RequiredApprovingReviewCount != 2 || !rules.StrictRequiredChecks || !rules.DismissStaleReviews || !rules.RequireLastPushApproval {
		t.Fatalf("rules = %+v", rules)
	}
}

type classicProtectionGitHub struct {
	observationGitHubStub
	classicRules gh.BranchRules
	classicErr   error
	classicCalls int
}

func (client *classicProtectionGitHub) GetBranchContext(_ context.Context, _ string, ref string) (gh.Branch, error) {
	return gh.Branch{Ref: ref, SHA: "branch-" + ref, Protected: true}, nil
}

func (client *classicProtectionGitHub) GetClassicBranchRulesContext(_ context.Context, _ string, _ string) (gh.BranchRules, error) {
	client.classicCalls++
	return client.classicRules, client.classicErr
}

func TestAcquireBranchFactsMergesClassicProtection(t *testing.T) {
	client := &classicProtectionGitHub{classicRules: gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{{Context: "test", IntegrationID: 42}}, RequiredApprovingReviewCount: 2, StrictRequiredChecks: true}}
	branches, rules, err := (ObservationWorkflow{}).acquireBranchFacts(context.Background(), client, "example/repo", "", map[string]struct{}{"main": {}})
	if err != nil {
		t.Fatal(err)
	}
	merged := rules["main"]
	if client.classicCalls != 1 || len(branches) != 1 || branches[0].Completeness != snapshot.Complete || len(merged.RequiredChecks) != 1 || merged.RequiredChecks[0].IntegrationID != 42 || merged.RequiredApprovingReviewCount != 2 || !merged.StrictRequiredChecks {
		t.Fatalf("calls=%d branches=%+v rules=%+v", client.classicCalls, branches, merged)
	}
}

func TestAcquireBranchFactsDegradesOnClassicProtectionFailure(t *testing.T) {
	client := &classicProtectionGitHub{classicErr: gh.APIError{Method: "GET", Path: "/protection", StatusCode: 403, Status: "403 Forbidden", RequestID: "request-1"}}
	branches, _, err := (ObservationWorkflow{}).acquireBranchFacts(context.Background(), client, "example/repo", "", map[string]struct{}{"main": {}})
	if err != nil {
		t.Fatal(err)
	}
	if client.classicCalls != 1 || len(branches) != 1 || branches[0].Completeness != snapshot.Degraded || len(branches[0].Warnings) != 1 || branches[0].Warnings[0].Scope != "branch:main:classic-rules" {
		t.Fatalf("calls=%d branches=%+v", client.classicCalls, branches)
	}
}

type recommendationFactsGitHub struct {
	observationGitHubStub
	detailCalls      int
	verifiedHeadSHA  string
	incompleteChecks bool
	reviewerError    error
	mergeable        string
	currentBaseRef   string
}

func (client *recommendationFactsGitHub) GetPullRequestContext(_ context.Context, _ string, number int) (gh.PullRequest, error) {
	client.detailCalls++
	pullRequest, err := client.observationGitHubStub.GetPullRequestContext(context.Background(), "", number)
	if client.mergeable != "" {
		pullRequest.Mergeable = client.mergeable
	}
	if client.currentBaseRef != "" {
		pullRequest.BaseRef = client.currentBaseRef
	}
	if client.detailCalls%2 == 0 && client.verifiedHeadSHA != "" {
		pullRequest.HeadSHA = client.verifiedHeadSHA
	}
	return pullRequest, err
}

func (client *recommendationFactsGitHub) GetCheckRollupContext(ctx context.Context, repo string, number int, sha string) (gh.CheckRollup, error) {
	rollup, err := client.observationGitHubStub.GetCheckRollupContext(ctx, repo, number, sha)
	if number > 0 && client.incompleteChecks {
		rollup.Complete = false
		rollup.Warnings = []string{"GitHub check-run cap reached"}
	}
	return rollup, err
}

func (client *recommendationFactsGitHub) GetRequestedReviewersContext(context.Context, string, int) ([]gh.ReviewRequest, error) {
	if client.reviewerError != nil {
		return nil, client.reviewerError
	}
	return nil, nil
}

func completeRecommendationClient() *recommendationFactsGitHub {
	return &recommendationFactsGitHub{observationGitHubStub: observationGitHubStub{
		issues:       []gh.Issue{{Number: 7, Title: "Issue", Labels: []string{"agent:ready-bounded"}}},
		pullRequests: []gh.PullRequest{{Number: 12, Title: "PR", BaseRef: "agent", BaseSHA: "base", HeadRef: "work", HeadSHA: "head", Mergeable: "mergeable", MergeState: "clean"}},
		branch:       &gh.BranchHealth{Ref: "agent", SHA: "base", CheckState: "success"},
		checkState:   "success",
	}}
}

func TestAcquireRecommendationFactsIsCompleteForStableFacts(t *testing.T) {
	client := completeRecommendationClient()
	facts, err := observationWorkflowForTest(client).AcquireRecommendationFactsContext(context.Background(), RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Completeness != snapshot.Complete || len(facts.PullRequests) != 1 || facts.PullRequests[0].Completeness != snapshot.Complete || client.detailCalls != 2 {
		t.Fatalf("facts=%+v detailCalls=%d", facts, client.detailCalls)
	}
}

func TestMergedWorkHistoryPreventsIssueReadmission(t *testing.T) {
	client := completeRecommendationClient()
	client.pullRequests = nil
	client.closedPullRequests = []gh.PullRequest{{Number: 42, BaseRef: "agent", HeadRef: "agent-work/7", Body: "Refs #7", Merged: true}}
	repositorySnapshot, err := observationWorkflowForTest(client).Snapshot(RepositoryInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if repositorySnapshot.Queue.Issues[0].Eligible {
		t.Fatalf("merged work issue returned to intake: %+v", repositorySnapshot.Queue.Issues[0])
	}
}

func TestNextRefusesWhenMergedWorkHistoryIsIncomplete(t *testing.T) {
	client := completeRecommendationClient()
	client.closedErr = gh.APIError{Method: "GET", StatusCode: 503}
	_, err := observationWorkflowForTest(client).Next(RepositoryInput{}, "")
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Details["completeness"] != "degraded" || !strings.Contains(applicationError.Details["warningScopes"], "mergedWorkPullRequests:agent") {
		t.Fatalf("error = %+v", applicationError)
	}
}

func TestNextRefusesRecommendationWhenChecksAreIncomplete(t *testing.T) {
	client := completeRecommendationClient()
	client.incompleteChecks = true

	_, err := observationWorkflowForTest(client).Next(RepositoryInput{}, "")
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.GitHub || applicationError.Details["completeness"] != "degraded" || applicationError.Details["warningCodes"] != "truncated" {
		t.Fatalf("error = %+v", applicationError)
	}
}

func TestAcquireRecommendationFactsPreservesPartialFactsAndSafeFailureMetadata(t *testing.T) {
	client := completeRecommendationClient()
	client.reviewerError = gh.APIError{Method: "GET", Path: "/requested_reviewers", StatusCode: 503, RequestID: "request-1"}

	facts, err := observationWorkflowForTest(client).AcquireRecommendationFactsContext(context.Background(), RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Completeness != snapshot.Degraded || len(facts.Issues) != 1 || len(facts.PullRequests) != 1 || facts.PullRequests[0].Checks.State != "success" {
		t.Fatalf("facts = %+v", facts)
	}
	if len(facts.Warnings) != 1 || facts.Warnings[0].Scope != "pullRequest:12:reviewRequests" || facts.Warnings[0].HTTPStatus != 503 || facts.Warnings[0].RequestID != "request-1" {
		t.Fatalf("warnings = %+v", facts.Warnings)
	}
}

func TestAcquireRecommendationFactsDetectsRevisionRace(t *testing.T) {
	client := completeRecommendationClient()
	client.verifiedHeadSHA = "new-head"

	facts, err := observationWorkflowForTest(client).AcquireRecommendationFactsContext(context.Background(), RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Completeness != snapshot.Degraded || len(facts.Warnings) != 1 || facts.Warnings[0].Code != "stale" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestAcquireRecommendationFactsDetectsRetargetBeforeDetail(t *testing.T) {
	client := completeRecommendationClient()
	client.currentBaseRef = "main"

	facts, err := observationWorkflowForTest(client).AcquireRecommendationFactsContext(context.Background(), RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Completeness != snapshot.Degraded || len(facts.Warnings) != 1 || facts.Warnings[0].Code != "stale" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestAcquireRecommendationFactsTreatsUnknownMergeabilityAsIncomplete(t *testing.T) {
	client := completeRecommendationClient()
	client.mergeable = "unknown"

	facts, err := observationWorkflowForTest(client).AcquireRecommendationFactsContext(context.Background(), RepositoryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Completeness != snapshot.Degraded || len(facts.Warnings) != 1 || facts.Warnings[0].Scope != "pullRequest:12:mergeability" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestAcquisitionWarningRecognizesRateLimit(t *testing.T) {
	warning := acquisitionWarning("issues", gh.APIError{StatusCode: 403, RateLimited: true})
	if warning.Code != "rate_limited" || !warning.Retryable {
		t.Fatalf("warning = %+v", warning)
	}
}
