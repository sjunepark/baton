package gh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGetPullRequestIncludesRevisionAndMergeability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/pulls/7" {
			t.Fatalf("path = %s", r.URL.String())
		}
		w.Write([]byte(`{"number":7,"draft":true,"mergeable":false,"mergeable_state":"dirty","user":{"login":"octo","type":"User"},"base":{"ref":"main","sha":"base-sha"},"head":{"ref":"work","sha":"head-sha"}}`))
	}))
	defer server.Close()

	pullRequest, err := NewClient(server.URL, "token", server.Client()).GetPullRequest("example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pullRequest.BaseSHA != "base-sha" || pullRequest.HeadSHA != "head-sha" || !pullRequest.Draft || pullRequest.Mergeable != "conflicting" || pullRequest.MergeState != "dirty" || pullRequest.Author != (Actor{Login: "octo", Type: "User"}) {
		t.Fatalf("pull request = %+v", pullRequest)
	}
}

func TestListPullRequestReviewsPaginatesAndPreservesReviewRevision(t *testing.T) {
	pages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		count := 100
		if page == 2 {
			count = 1
		}
		items := make([]map[string]any, count)
		for index := range items {
			id := int64((page-1)*100 + index + 1)
			items[index] = map[string]any{"id": id, "state": "APPROVED", "commit_id": fmt.Sprintf("sha-%d", id), "submitted_at": "2026-07-12T01:02:03Z", "user": map[string]any{"login": fmt.Sprintf("user-%d", id), "type": "User"}}
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	reviews, err := NewClient(server.URL, "token", server.Client()).ListPullRequestReviews("example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 101 || reviews[100].CommitSHA != "sha-101" || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("reviews=%d last=%+v pages=%v", len(reviews), reviews[100], pages)
	}
}

func TestGetRequestedReviewersIncludesUsersAndTeams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"users":[{"login":"alice"}],"teams":[{"slug":"maintainers"}]}`))
	}))
	defer server.Close()

	requests, err := NewClient(server.URL, "token", server.Client()).GetRequestedReviewers("example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(requests) != "[{user alice } {team  maintainers}]" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestGetBranchAndEffectiveRulesPreserveRequiredCheckIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/repo/branches/main":
			w.Write([]byte(`{"name":"main","protected":true,"commit":{"sha":"base"},"protection":{"required_status_checks":{"contexts":["legacy"]}}}`))
		case "/repos/example/repo/rules/branches/main":
			w.Write([]byte(`[{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"test","integration_id":42}]}},{"type":"pull_request","parameters":{"dismiss_stale_reviews_on_push":true,"require_last_push_approval":true,"required_approving_review_count":2}}]`))
		default:
			t.Fatalf("path = %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	branch, err := client.GetBranch("example/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := client.GetEffectiveBranchRules("example/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !branch.Protected || branch.SHA != "base" || len(branch.LegacyRequiredChecks) != 1 || len(rules.RequiredChecks) != 1 || rules.RequiredChecks[0].IntegrationID != 42 || !rules.StrictRequiredChecks || !rules.DismissStaleReviews || !rules.RequireLastPushApproval || rules.RequiredApprovingReviewCount != 2 {
		t.Fatalf("branch=%+v rules=%+v", branch, rules)
	}
}

func TestGetEffectiveBranchRulesPaginates(t *testing.T) {
	pages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		count := 100
		if page == 2 {
			count = 1
		}
		rules := make([]map[string]any, count)
		for index := range rules {
			rules[index] = map[string]any{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": []map[string]any{{"context": fmt.Sprintf("check-%d-%d", page, index)}}}}
		}
		json.NewEncoder(w).Encode(rules)
	}))
	defer server.Close()

	rules, err := NewClient(server.URL, "token", server.Client()).GetEffectiveBranchRules("example/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.RequiredChecks) != 101 || rules.RequiredChecks[100].Context != "check-2-0" || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("rules=%d last=%+v pages=%v", len(rules.RequiredChecks), rules.RequiredChecks[100], pages)
	}
}

func TestGetClassicBranchRulesPreservesReviewAndStrictCheckPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/branches/main/protection" {
			t.Fatalf("path = %s", r.URL.String())
		}
		w.Write([]byte(`{"required_status_checks":{"strict":true,"contexts":["test","legacy"],"checks":[{"context":"test","app_id":42}]},"required_pull_request_reviews":{"dismiss_stale_reviews":true,"require_last_push_approval":true,"required_approving_review_count":2}}`))
	}))
	defer server.Close()

	rules, err := NewClient(server.URL, "token", server.Client()).GetClassicBranchRules("example/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !rules.StrictRequiredChecks || !rules.DismissStaleReviews || !rules.RequireLastPushApproval || rules.RequiredApprovingReviewCount != 2 || len(rules.RequiredChecks) != 2 || rules.RequiredChecks[0].IntegrationID != 42 || rules.RequiredChecks[1].Context != "legacy" {
		t.Fatalf("rules = %+v", rules)
	}
}
