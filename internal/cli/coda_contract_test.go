package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/queue"
	batonsnapshot "github.com/sjunepark/baton/internal/snapshot"
)

type codaNextContract struct {
	SchemaVersion         int               `json:"schemaVersion"`
	Kind                  string            `json:"kind"`
	SelectedAction        string            `json:"selectedAction"`
	Repo                  string            `json:"repo"`
	Reason                string            `json:"reason"`
	SelectionReason       string            `json:"selectionReason"`
	SelectionRequired     bool              `json:"selectionRequired"`
	Candidates            []json.RawMessage `json:"candidates"`
	DeferredEligibleItems []json.RawMessage `json:"deferredEligibleItems"`
	BlockedItems          []string          `json:"blockedItems"`
	Instructions          []string          `json:"instructions"`
}

func TestCodaNextV3GoldenContracts(t *testing.T) {
	fixtures := map[string]string{
		"issue":           "next-v3-issue.json",
		"issue-selection": "next-v3-issue-selection.json",
		"pull-request":    "next-v3-pull-request.json",
		"branch":          "next-v3-branch.json",
		"none":            "next-v3-none.json",
		"sync-staging":    "next-v3-sync-staging.json",
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			content := readCodaFixture(t, fixture)
			producerJSON, err := json.Marshal(codaNextProducer(name))
			if err != nil {
				t.Fatal(err)
			}
			assertEquivalentJSON(t, producerJSON, content)
			var consumer codaNextContract
			if err := json.Unmarshal(content, &consumer); err != nil {
				t.Fatal(err)
			}
			if consumer.SchemaVersion != 3 || consumer.Kind != "nextCandidates" || consumer.SelectedAction == "" || consumer.Repo == "" {
				t.Fatalf("unsupported next contract: %+v", consumer)
			}
			if consumer.Candidates == nil || consumer.DeferredEligibleItems == nil || consumer.BlockedItems == nil || consumer.Instructions == nil {
				t.Fatalf("collection fields must be JSON arrays: %+v", consumer)
			}
			for _, raw := range append(consumer.Candidates, consumer.DeferredEligibleItems...) {
				assertCodaCandidate(t, raw)
			}
		})
	}
}

func TestCodaQueueV2GoldenContract(t *testing.T) {
	content := readCodaFixture(t, "queue-v2.json")
	producerJSON, err := json.Marshal(codaQueueProducer())
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentJSON(t, producerJSON, content)
	var consumer struct {
		SchemaVersion int                        `json:"schemaVersion"`
		Kind          string                     `json:"kind"`
		Repo          string                     `json:"repo"`
		Counts        map[string]json.RawMessage `json:"counts"`
		BranchHealth  *struct {
			Ref        string `json:"ref"`
			SHA        string `json:"sha"`
			CheckState string `json:"checkState"`
		} `json:"branchHealth"`
		Issues []struct {
			Eligible      bool     `json:"eligible"`
			Action        string   `json:"action"`
			PriorityLabel string   `json:"priorityLabel"`
			Reasons       []string `json:"reasons"`
			LinkedPRs     []int    `json:"linkedPrs"`
			Issue         struct {
				Number int      `json:"number"`
				Title  string   `json:"title"`
				URL    string   `json:"url"`
				Labels []string `json:"labels"`
			} `json:"issue"`
		} `json:"issues"`
		PullRequests []struct {
			ReferencedIssues []int `json:"referencedIssues"`
			PullRequest      struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
				URL    string `json:"url"`
			} `json:"pullRequest"`
		} `json:"pullRequests"`
	}
	if err := json.Unmarshal(content, &consumer); err != nil {
		t.Fatal(err)
	}
	if consumer.SchemaVersion != 2 || consumer.Kind != "queueSnapshot" || consumer.Repo == "" || consumer.Counts == nil {
		t.Fatalf("unsupported queue contract: %+v", consumer)
	}
	for _, field := range []string{"totalIssues", "eligibleIssues", "skippedIssues", "openPullRequests"} {
		if _, ok := consumer.Counts[field]; !ok {
			t.Fatalf("queue counts missing %q", field)
		}
	}
	var branchHealthState string
	if err := json.Unmarshal(consumer.Counts["branchHealthState"], &branchHealthState); err != nil || branchHealthState == "" {
		t.Fatalf("queue counts missing branchHealthState: %v", err)
	}
	if consumer.Issues == nil || consumer.PullRequests == nil {
		t.Fatalf("queue collections must be JSON arrays: %+v", consumer)
	}
	if consumer.BranchHealth == nil || consumer.BranchHealth.Ref == "" || consumer.BranchHealth.SHA == "" || consumer.BranchHealth.CheckState == "" {
		t.Fatalf("queue branch health is incomplete: %+v", consumer.BranchHealth)
	}
	for index, issue := range consumer.Issues {
		if issue.Issue.Number == 0 || issue.Issue.Title == "" || issue.Issue.URL == "" || issue.Issue.Labels == nil || issue.Reasons == nil || issue.LinkedPRs == nil {
			t.Fatalf("queue issue contract is incomplete: %+v", issue)
		}
		if index == 0 && (!issue.Eligible || issue.Action == "" || issue.PriorityLabel == "") {
			t.Fatalf("eligible queue issue lost action or priority: %+v", issue)
		}
	}
	for _, pullRequest := range consumer.PullRequests {
		if pullRequest.PullRequest.Number == 0 || pullRequest.PullRequest.Title == "" || pullRequest.PullRequest.URL == "" || pullRequest.ReferencedIssues == nil {
			t.Fatalf("queue pull request contract is incomplete: %+v", pullRequest)
		}
	}
}

func TestCodaStructuredErrorV1GoldenContract(t *testing.T) {
	content := readCodaFixture(t, "error-v1-config.json")
	var result errorResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Kind != "error" || result.Category != "config" || result.ExitCode != exitConfig || result.Message == "" || result.Hint == "" || result.Retryable {
		t.Fatalf("structured error contract = %+v", result)
	}
	producerJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentJSON(t, producerJSON, content)
}

func TestCodaRepositorySnapshotV2GoldenContract(t *testing.T) {
	fixtures := map[string]string{
		"issue":        "repository-snapshot-v2.json",
		"pull-request": "repository-snapshot-v2-pr.json",
		"branch":       "repository-snapshot-v2-branch.json",
		"degraded":     "repository-snapshot-v2-degraded.json",
		"human-choice": "repository-snapshot-v2-human-choice.json",
		"ready-pr":     "repository-snapshot-v2-ready-pr.json",
		"sync-staging": "repository-snapshot-v2-sync-staging.json",
	}
	for scenario, fixture := range fixtures {
		t.Run(scenario, func(t *testing.T) {
			producerJSON, err := json.Marshal(codaRepositorySnapshotProducer(t, scenario))
			if err != nil {
				t.Fatal(err)
			}
			content := readCodaFixture(t, fixture)
			assertEquivalentJSON(t, producerJSON, content)
			assertCodaRepositorySnapshotConsumer(t, scenario, content)
		})
	}
}

func assertCodaRepositorySnapshotConsumer(t *testing.T, scenario string, content []byte) {
	t.Helper()
	var consumer struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		Repository    string `json:"repository"`
		Acquisition   struct {
			StartedAt   time.Time `json:"startedAt"`
			CompletedAt time.Time `json:"completedAt"`
		} `json:"acquisition"`
		Completeness string            `json:"completeness"`
		Warnings     []json.RawMessage `json:"warnings"`
		Queue        struct {
			Kind   string            `json:"kind"`
			Counts map[string]any    `json:"counts"`
			Issues []json.RawMessage `json:"issues"`
		} `json:"queue"`
		Branches       []json.RawMessage `json:"branches"`
		PullRequests   []json.RawMessage `json:"pullRequests"`
		Recommendation struct {
			Outcome           string  `json:"outcome"`
			Action            *string `json:"action"`
			SelectionRequired bool    `json:"selectionRequired"`
			Candidates        []struct {
				Identity struct {
					Repository string `json:"repository"`
					Kind       string `json:"kind"`
					Number     int    `json:"number"`
					Ref        string `json:"ref"`
					SHA        string `json:"sha"`
					BaseRef    string `json:"baseRef"`
					HeadRef    string `json:"headRef"`
					BaseSHA    string `json:"baseSha"`
					HeadSHA    string `json:"headSha"`
				} `json:"identity"`
			} `json:"candidates"`
			DeferredCandidates []json.RawMessage `json:"deferredCandidates"`
			Reasons            []string          `json:"reasons"`
			Instructions       []string          `json:"instructions"`
		} `json:"recommendation"`
	}
	if err := json.Unmarshal(content, &consumer); err != nil {
		t.Fatal(err)
	}
	if consumer.SchemaVersion != 2 || consumer.Kind != "repositorySnapshot" || consumer.Repository == "" || consumer.Acquisition.StartedAt.IsZero() || consumer.Acquisition.CompletedAt.Before(consumer.Acquisition.StartedAt) {
		t.Fatalf("unsupported snapshot contract: %+v", consumer)
	}
	if consumer.Warnings == nil || consumer.Branches == nil || consumer.PullRequests == nil || consumer.Queue.Kind != "queueSnapshot" || consumer.Queue.Counts == nil || consumer.Queue.Issues == nil {
		t.Fatalf("snapshot collections or queue facts are incomplete: %+v", consumer)
	}
	if consumer.Recommendation.Candidates == nil || consumer.Recommendation.DeferredCandidates == nil || consumer.Recommendation.Reasons == nil || consumer.Recommendation.Instructions == nil {
		t.Fatalf("snapshot recommendation is incomplete: %+v", consumer.Recommendation)
	}
	switch scenario {
	case "issue":
		if consumer.Completeness != "complete" || consumer.Recommendation.Outcome != "actionable" || consumer.Recommendation.Action == nil || *consumer.Recommendation.Action != "issue_implementation" || len(consumer.Recommendation.Candidates) != 1 || consumer.Recommendation.Candidates[0].Identity.Number != 18 {
			t.Fatalf("issue snapshot = %+v", consumer.Recommendation)
		}
	case "pull-request":
		identity := consumer.Recommendation.Candidates[0].Identity
		if len(consumer.PullRequests) != 1 || consumer.Recommendation.Action == nil || *consumer.Recommendation.Action != "pull_request_follow_up" || identity.BaseRef == "" || identity.HeadRef == "" || identity.BaseSHA == "" || identity.HeadSHA == "" {
			t.Fatalf("pull-request identity = %+v", identity)
		}
	case "branch":
		identity := consumer.Recommendation.Candidates[0].Identity
		if len(consumer.Branches) != 1 || consumer.Recommendation.Action == nil || *consumer.Recommendation.Action != "branch_health" || identity.Ref == "" || identity.SHA == "" {
			t.Fatalf("branch identity = %+v", identity)
		}
	case "degraded":
		if consumer.Completeness != "degraded" || len(consumer.Warnings) != 1 || consumer.Recommendation.Outcome != "degraded" || consumer.Recommendation.Action != nil || len(consumer.Recommendation.Candidates) != 0 {
			t.Fatalf("degraded snapshot = %+v", consumer.Recommendation)
		}
	case "human-choice":
		if consumer.Recommendation.Outcome != "human_choice_required" || consumer.Recommendation.Action == nil || *consumer.Recommendation.Action != "issue_implementation" || !consumer.Recommendation.SelectionRequired || len(consumer.Recommendation.Candidates) != 2 {
			t.Fatalf("human-choice snapshot = %+v", consumer.Recommendation)
		}
	case "ready-pr":
		identity := consumer.Recommendation.Candidates[0].Identity
		if consumer.Recommendation.Outcome != "human_choice_required" || consumer.Recommendation.Action != nil || consumer.Recommendation.SelectionRequired || len(consumer.Recommendation.Candidates) != 1 || identity.BaseSHA == "" || identity.HeadSHA == "" {
			t.Fatalf("ready-pr snapshot = %+v", consumer.Recommendation)
		}
	case "sync-staging":
		identity := consumer.Recommendation.Candidates[0].Identity
		if consumer.Recommendation.Outcome != "actionable" || consumer.Recommendation.Action == nil || *consumer.Recommendation.Action != "sync_staging" || len(consumer.Recommendation.Candidates) != 1 || identity.Kind != "repository" || identity.BaseRef == "" || identity.HeadRef == "" || identity.BaseSHA == "" || identity.HeadSHA == "" {
			t.Fatalf("sync-staging snapshot = %+v", consumer.Recommendation)
		}
	}
}

func codaRepositorySnapshotProducer(t *testing.T, scenario string) batonsnapshot.RepositorySnapshot {
	t.Helper()
	cfg := config.DefaultConfig()
	facts := batonsnapshot.Acquisition{Repository: "open-creo/creo", Completeness: batonsnapshot.Complete}
	queueProjection := queue.BuildSnapshot("open-creo/creo", cfg, nil, nil)
	switch scenario {
	case "issue":
		issue := gh.Issue{Number: 18, Title: "Implement unified snapshot", URL: "https://github.com/open-creo/creo/issues/18", Labels: []string{"agent:ready-bounded"}}
		facts.Issues = []batonsnapshot.IssueFacts{{Issue: issue}}
		queueProjection = queue.BuildSnapshot("open-creo/creo", cfg, []queue.Issue{{Number: issue.Number, Title: issue.Title, URL: issue.URL, Labels: issue.Labels}}, nil)
	case "pull-request":
		pr := gh.PullRequest{Number: 42, Title: "Repair checks", URL: "https://github.com/open-creo/creo/pull/42", BaseRef: "agent", BaseSHA: "base-42", HeadRef: "agent-work/42", HeadSHA: "head-42", Mergeable: "mergeable", MergeState: "clean"}
		facts.PullRequests = []batonsnapshot.PullRequestFacts{{PullRequest: pr, Checks: gh.CheckRollup{State: "failure", Complete: true}, ReviewThreads: gh.ReviewThreadResult{Complete: true}, Review: batonsnapshot.ReviewState{State: "approved"}, Completeness: batonsnapshot.Complete}}
		queueProjection = queue.BuildSnapshot("open-creo/creo", cfg, nil, []queue.PullRequest{{Number: pr.Number, Title: pr.Title, URL: pr.URL, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA, CheckState: "failure"}})
	case "ready-pr":
		pr := gh.PullRequest{Number: 43, Title: "Ready for disposition", URL: "https://github.com/open-creo/creo/pull/43", BaseRef: "agent", BaseSHA: "base-43", HeadRef: "agent-work/43", HeadSHA: "head-43", Mergeable: "mergeable", MergeState: "clean"}
		facts.PullRequests = []batonsnapshot.PullRequestFacts{{PullRequest: pr, Checks: gh.CheckRollup{State: "success", Complete: true}, ReviewThreads: gh.ReviewThreadResult{Complete: true}, Review: batonsnapshot.ReviewState{State: "approved"}, Completeness: batonsnapshot.Complete}}
		queueProjection = queue.BuildSnapshot("open-creo/creo", cfg, nil, []queue.PullRequest{{Number: pr.Number, Title: pr.Title, URL: pr.URL, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA, CheckState: "success"}})
	case "branch":
		branch := gh.Branch{Ref: "agent", SHA: "branch-sha"}
		facts.Branches = []batonsnapshot.BranchFacts{{Branch: branch, Checks: gh.CheckRollup{State: "failure", Complete: true}, Completeness: batonsnapshot.Complete}}
		queueProjection = queue.BuildSnapshotWithBranchHealth("open-creo/creo", cfg, nil, nil, &queue.BranchHealth{Ref: branch.Ref, SHA: branch.SHA, CheckState: "failure"})
	case "degraded":
		facts.Completeness = batonsnapshot.Degraded
		facts.Warnings = []batonsnapshot.Warning{{Code: "rate_limited", Scope: "pullRequests", Message: "GitHub facts are incomplete", Retryable: true, HTTPStatus: 429, RequestID: "request-1"}}
	case "human-choice":
		issues := []queue.Issue{{Number: 18, Title: "First tied issue", URL: "https://github.com/open-creo/creo/issues/18", Labels: []string{"agent:ready-bounded"}}, {Number: 19, Title: "Second tied issue", URL: "https://github.com/open-creo/creo/issues/19", Labels: []string{"agent:ready-bounded"}}}
		queueProjection = queue.BuildSnapshot("open-creo/creo", cfg, issues, nil)
	case "sync-staging":
		integration := delivery.BaseIntegrationFacts{
			State: delivery.BaseDirectWorkPending, ObservedBaseSHA: "4444444444444444444444444444444444444444", ObservedStagingSHA: "3333333333333333333333333333333333333333",
			IntegrationRecordDigest: strings.Repeat("a", 64), IntegrationSource: delivery.IntegrationPromotion,
			IntegratedBaseSHA: "2222222222222222222222222222222222222222", IntegratedStagingSHA: "3333333333333333333333333333333333333333", Reason: "base advanced beyond the acknowledged integration revision",
		}
		facts.BaseIntegration = &integration
		queueProjection.BaseIntegration = &integration
	default:
		t.Fatalf("unknown snapshot scenario %q", scenario)
	}
	startedAt := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	result, err := batonsnapshot.Build(batonsnapshot.BuildInput{
		Facts: facts,
		Queue: queueProjection, StartedAt: startedAt, CompletedAt: startedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertCodaCandidate(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var candidate map[string]json.RawMessage
	if err := json.Unmarshal(raw, &candidate); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := json.Unmarshal(candidate["type"], &kind); err != nil {
		t.Fatalf("candidate type: %v", err)
	}
	require := func(field string) {
		if len(candidate[field]) == 0 {
			t.Fatalf("%s candidate missing %q: %s", kind, field, raw)
		}
	}
	switch kind {
	case "issue":
		require("number")
		require("title")
		require("url")
		require("priorityLabel")
	case "pullRequest":
		require("number")
		require("title")
		require("url")
		require("headRef")
		require("baseRef")
		require("checkState")
	case "branch":
		require("ref")
		require("sha")
		require("checkState")
	case "repository":
		require("baseRef")
		require("headRef")
		require("ref")
		require("sha")
	default:
		t.Fatalf("unsupported Coda candidate type %q", kind)
	}
}

func codaNextProducer(name string) queue.NextCandidates {
	repo := "open-creo/creo"
	switch name {
	case "issue":
		return queue.RecommendNext(queue.Snapshot{Repo: repo, Issues: []queue.IssueState{{Issue: queue.Issue{Number: 5, Title: "[Work]: Audit xs font-size usage", URL: "https://github.com/open-creo/creo/issues/5"}, Eligible: true, Action: "issue-investigation", PriorityLabel: "priority:p2", PriorityRank: 3}}})
	case "issue-selection":
		return queue.RecommendNext(queue.Snapshot{Repo: repo, Issues: []queue.IssueState{
			{Issue: queue.Issue{Number: 18, Title: "First tied issue", URL: "https://github.com/open-creo/creo/issues/18"}, Eligible: true, Action: "issue-implementation", PriorityLabel: "priority:p1", PriorityRank: 2},
			{Issue: queue.Issue{Number: 19, Title: "Second tied issue", URL: "https://github.com/open-creo/creo/issues/19"}, Eligible: true, Action: "issue-implementation", PriorityLabel: "priority:p1", PriorityRank: 2},
			{Issue: queue.Issue{Number: 20, Title: "Lower-priority issue", URL: "https://github.com/open-creo/creo/issues/20"}, Eligible: true, Action: "issue-implementation", PriorityLabel: "priority:p3", PriorityRank: 4},
		}})
	case "pull-request":
		return queue.RecommendNext(queue.Snapshot{Repo: repo, PullRequests: []queue.PullState{{PullRequest: queue.PullRequest{Number: 42, Title: "Implement queue contract", URL: "https://github.com/open-creo/creo/pull/42", HeadRef: "agent-work/42-contract", BaseRef: "agent", CheckState: "failure"}}}})
	case "branch":
		return queue.RecommendNext(queue.Snapshot{Repo: repo, BranchHealth: &queue.BranchHealth{Ref: "agent", SHA: "0123456789abcdef0123456789abcdef01234567", CheckState: "failure"}})
	case "none":
		return queue.RecommendNext(queue.Snapshot{Repo: repo})
	case "sync-staging":
		return queue.RecommendNext(queue.Snapshot{
			Repo: repo, BaseBranch: "main", StagingBranch: "agent",
			BaseIntegration: &delivery.BaseIntegrationFacts{State: delivery.BaseDirectWorkPending, ObservedBaseSHA: "4444444444444444444444444444444444444444", ObservedStagingSHA: "3333333333333333333333333333333333333333"},
		})
	default:
		panic("unknown Coda next fixture " + name)
	}
}

func codaQueueProducer() queue.Snapshot {
	cfg := config.DefaultConfig()
	return queue.BuildSnapshotWithBranchHealth(
		"open-creo/creo",
		cfg,
		[]queue.Issue{
			{Number: 18, Title: "Implement queue contract", URL: "https://github.com/open-creo/creo/issues/18", Labels: []string{"enhancement", "agent:ready-bounded", "priority:p1"}},
			{Number: 21, Title: "Needs discussion", URL: "https://github.com/open-creo/creo/issues/21", Labels: []string{"needs:discussion"}},
		},
		[]queue.PullRequest{{Number: 42, Title: "Implement queue contract", URL: "https://github.com/open-creo/creo/pull/42", Body: "Refs #21", BaseRef: "agent", HeadRef: "agent-work/42-contract", HeadSHA: "abcdef0123456789abcdef0123456789abcdef01", CheckState: "success"}},
		&queue.BranchHealth{Ref: "agent", SHA: "0123456789abcdef0123456789abcdef01234567", CheckState: "success"},
	)
}

func assertEquivalentJSON(t *testing.T, actual, expected []byte) {
	t.Helper()
	var actualValue, expectedValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("producer JSON does not match golden fixture\nactual: %s\nexpected: %s", actual, expected)
	}
}

func readCodaFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "contracts", "coda", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
