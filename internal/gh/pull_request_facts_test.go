package gh

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

const planUnavailableMessage = "Upgrade to GitHub Pro or make this repository public to enable this feature."

func TestGetPullRequestIncludesRevisionAndMergeability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/pulls/7" {
			t.Fatalf("path = %s", r.URL.String())
		}
		w.Write([]byte(`{"number":7,"node_id":"PR_7","draft":true,"mergeable":false,"mergeable_state":"dirty","merged_at":"2026-07-15T01:02:03Z","merge_commit_sha":"merge-sha","user":{"login":"octo","type":"User"},"base":{"ref":"main","sha":"base-sha"},"head":{"ref":"work","sha":"head-sha"}}`))
	}))
	defer server.Close()

	pullRequest, err := NewClient(server.URL, "token", server.Client()).GetPullRequest("example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pullRequest.NodeID != "PR_7" || pullRequest.BaseSHA != "base-sha" || pullRequest.HeadSHA != "head-sha" || !pullRequest.Draft || pullRequest.Mergeable != "conflicting" || pullRequest.MergeState != "dirty" || pullRequest.Author != (Actor{Login: "octo", Type: "User"}) || !pullRequest.Merged || pullRequest.MergeRevision != "merge-sha" {
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
			w.Write([]byte(`[{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"test","integration_id":42}]}},{"type":"pull_request","parameters":{"dismiss_stale_reviews_on_push":true,"require_last_push_approval":true,"required_approving_review_count":2,"allowed_merge_methods":["merge","squash"]}},{"type":"pull_request","parameters":{"allowed_merge_methods":["merge","rebase"]}},{"type":"required_linear_history"},{"type":"merge_queue"}]`))
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
	if !branch.Protected || branch.SHA != "base" || len(branch.LegacyRequiredChecks) != 1 || len(rules.RequiredChecks) != 1 || rules.RequiredChecks[0].IntegrationID != 42 || !rules.StrictRequiredChecks || !rules.DismissStaleReviews || !rules.RequireLastPushApproval || !rules.RequiredLinearHistory || !rules.MergeQueueEnabled || !rules.AllowedMergeMethodsSet || !reflect.DeepEqual(rules.AllowedMergeMethods, []string{"merge"}) || rules.RequiredApprovingReviewCount != 2 {
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
		w.Write([]byte(`{"required_status_checks":{"strict":true,"contexts":["test","legacy"],"checks":[{"context":"test","app_id":42}]},"required_pull_request_reviews":{"dismiss_stale_reviews":true,"require_last_push_approval":true,"required_approving_review_count":2},"required_linear_history":{"enabled":true}}`))
	}))
	defer server.Close()

	rules, err := NewClient(server.URL, "token", server.Client()).GetClassicBranchRules("example/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !rules.StrictRequiredChecks || !rules.DismissStaleReviews || !rules.RequireLastPushApproval || !rules.RequiredLinearHistory || rules.RequiredApprovingReviewCount != 2 || len(rules.RequiredChecks) != 2 || rules.RequiredChecks[0].IntegrationID != 42 || rules.RequiredChecks[1].Context != "legacy" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestGetClassicBranchRulesReportsUnprotectedBranchAsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/branches/agent/protection" {
			t.Fatalf("path = %s", r.URL.String())
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Branch not protected"})
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "token", server.Client()).GetClassicBranchRules("example/repo", "agent")
	if !IsNotFound(err) {
		t.Fatalf("error = %T %v, want not found", err, err)
	}
}

func TestBranchRuleAPIsTreatPlanUnavailableAsEmptyRules(t *testing.T) {
	tests := []struct {
		name             string
		path             string
		documentationURL string
		getRules         func(*Client) (BranchRules, error)
	}{
		{
			name:             "classic protection",
			path:             "/repos/example/repo/branches/main/protection",
			documentationURL: "https://docs.github.com/rest/branches/branch-protection#get-branch-protection",
			getRules:         func(client *Client) (BranchRules, error) { return client.GetClassicBranchRules("example/repo", "main") },
		},
		{
			name:             "effective rules",
			path:             "/repos/example/repo/rules/branches/main",
			documentationURL: "https://docs.github.com/rest/repos/rules#get-rules-for-a-branch",
			getRules: func(client *Client) (BranchRules, error) {
				return client.GetEffectiveBranchRules("example/repo", "main")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("path = %s, want %s", r.URL.Path, tt.path)
				}
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"message": planUnavailableMessage, "documentation_url": tt.documentationURL})
			}))
			defer server.Close()

			rules, err := tt.getRules(NewClient(server.URL, "token", server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			if rules.Branch != "main" || len(rules.RequiredChecks) != 0 || rules.RequiredApprovingReviewCount != 0 {
				t.Fatalf("rules = %+v, want empty rules for main", rules)
			}
		})
	}
}

func TestBranchRuleAPIsPreserveUnrecognizedForbiddenErrors(t *testing.T) {
	tests := []struct {
		name             string
		message          string
		documentationURL string
	}{
		{
			name:             "permission failure",
			message:          "Resource not accessible by integration",
			documentationURL: "https://docs.github.com/rest/branches/branch-protection#get-branch-protection",
		},
		{
			name:             "unrecognized documentation",
			message:          planUnavailableMessage,
			documentationURL: "https://docs.github.com/rest/overview/resources-in-the-rest-api",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"message": tt.message, "documentation_url": tt.documentationURL})
			}))
			defer server.Close()

			_, err := NewClient(server.URL, "token", server.Client()).GetClassicBranchRules("example/repo", "main")
			var apiErr APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
				t.Fatalf("error = %T %v, want forbidden APIError", err, err)
			}
		})
	}
}
