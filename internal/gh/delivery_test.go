package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetRepositoryIdentityAndExactIssueComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/repo":
			w.Write([]byte(`{"node_id":"R_1","full_name":"example/repo","html_url":"https://github.example.com/example/repo","allow_merge_commit":true,"allow_squash_merge":true,"allow_rebase_merge":false}`))
		case "/repos/example/repo/issues/comments/42":
			w.Write([]byte(`{"id":42,"node_id":"IC_42","body":"record","created_at":"2026-07-15T01:00:00Z","updated_at":"2026-07-15T02:00:00Z","user":{"login":"github-actions[bot]","type":"Bot"}}`))
		default:
			t.Fatalf("path = %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	repository, err := client.GetRepositoryIdentity("example/repo")
	if err != nil {
		t.Fatal(err)
	}
	if repository != (RepositoryIdentity{Host: "github.example.com", FullName: "example/repo", NodeID: "R_1"}) {
		t.Fatalf("repository = %+v", repository)
	}
	settings, err := client.GetRepositorySettingsContext(t.Context(), "example/repo")
	if err != nil || !settings.AllowMergeCommit || !settings.AllowSquashMerge || settings.AllowRebaseMerge {
		t.Fatalf("settings = %+v, err = %v", settings, err)
	}
	comment, err := client.GetIssueComment("example/repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if comment.ID != 42 || comment.NodeID != "IC_42" || comment.Body != "record" || comment.Author != (Actor{Login: "github-actions[bot]", Type: "Bot"}) || comment.CreatedAt.Format(time.RFC3339) != "2026-07-15T01:00:00Z" || comment.UpdatedAt.Format(time.RFC3339) != "2026-07-15T02:00:00Z" {
		t.Fatalf("comment = %+v", comment)
	}
}

func TestIssueCommentReturningMutationsPreserveExistingErrorOnlyMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/issues/7/comments":
			if input.Body != "created" {
				t.Fatalf("body = %q", input.Body)
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":101,"node_id":"IC_101","body":"created","created_at":"2026-07-15T01:00:00Z","updated_at":"2026-07-15T01:00:00Z"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/example/repo/issues/comments/101":
			if input.Body != "updated" {
				t.Fatalf("body = %q", input.Body)
			}
			w.Write([]byte(`{"id":101,"node_id":"IC_101","body":"updated","created_at":"2026-07-15T01:00:00Z","updated_at":"2026-07-15T02:00:00Z"}`))
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	created, err := client.CreateIssueCommentReturning("example/repo", 7, "created")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.UpdateIssueCommentReturning("example/repo", created.ID, "updated")
	if err != nil {
		t.Fatal(err)
	}
	if created.NodeID != "IC_101" || updated.Body != "updated" || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("created=%+v updated=%+v", created, updated)
	}
}

func TestListNewestIssueCommentsIsBoundedAndReportsCompleteness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(request.Query, "comments(last: $limit)") || request.Variables["limit"] != float64(100) || request.Variables["number"] != float64(900) {
			t.Fatalf("request = %+v", request)
		}
		w.Write([]byte(`{"data":{"repository":{"issue":{"comments":{"nodes":[{"databaseId":101,"id":"IC_101","body":"record","createdAt":"2026-07-15T01:00:00Z","updatedAt":"2026-07-15T02:00:00Z","author":{"login":"github-actions[bot]","__typename":"Bot"}}],"pageInfo":{"hasPreviousPage":true}}}}}}`))
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).ListNewestIssueComments("example/repo", 900)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Complete || len(listing.Comments) != 1 || listing.Comments[0].NodeID != "IC_101" || listing.Comments[0].Author.Type != "Bot" {
		t.Fatalf("listing = %+v", listing)
	}
}

func TestListIssueCommentsAfterPaginatesOnlyToCheckpointBoundary(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(request.Query, "before: $before") {
			t.Fatalf("query = %s", request.Query)
		}
		switch requests {
		case 1:
			if request.Variables["before"] != nil {
				t.Fatalf("first before = %#v", request.Variables["before"])
			}
			w.Write([]byte(`{"data":{"repository":{"issue":{"comments":{"nodes":[{"databaseId":103,"id":"IC_103","body":"new","createdAt":"2026-07-15T03:00:00Z","updatedAt":"2026-07-15T03:00:00Z","author":{"login":"github-actions[bot]","__typename":"Bot"}}],"pageInfo":{"hasPreviousPage":true,"startCursor":"cursor-2"}}}}}}`))
		case 2:
			if request.Variables["before"] != "cursor-2" {
				t.Fatalf("second before = %#v", request.Variables["before"])
			}
			w.Write([]byte(`{"data":{"repository":{"issue":{"comments":{"nodes":[{"databaseId":101,"id":"IC_101","body":"old","createdAt":"2026-07-15T01:00:00Z","updatedAt":"2026-07-15T01:00:00Z","author":{"login":"github-actions[bot]","__typename":"Bot"}},{"databaseId":102,"id":"IC_102","body":"boundary","createdAt":"2026-07-15T02:00:00Z","updatedAt":"2026-07-15T02:00:00Z","author":{"login":"github-actions[bot]","__typename":"Bot"}}],"pageInfo":{"hasPreviousPage":true,"startCursor":"cursor-1"}}}}}}`))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	boundary := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	listing, err := NewClient(server.URL, "token", server.Client()).ListIssueCommentsAfterContext(t.Context(), "example/repo", 900, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.Complete || requests != 2 || len(listing.Comments) != 2 || listing.Comments[0].ID != 103 || listing.Comments[1].ID != 102 {
		t.Fatalf("requests=%d listing=%+v", requests, listing)
	}
}

func TestListNewestIssueCommentsRejectsMissingIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":{"repository":{"issue":null}}}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "token", server.Client()).ListNewestIssueComments("example/repo", 900)
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestListNewestIssueCommentsIsCompleteAtExactLimitWithoutPreviousPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nodes := make([]map[string]any, newestIssueCommentLimit)
		for index := range nodes {
			nodes[index] = map[string]any{
				"databaseId": index + 1, "id": fmt.Sprintf("IC_%d", index+1), "body": "ordinary",
				"createdAt": "2026-07-15T01:00:00Z", "updatedAt": "2026-07-15T01:00:00Z",
				"author": map[string]any{"login": "github-actions[bot]", "__typename": "Bot"},
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{"comments": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasPreviousPage": false}}}}}})
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).ListNewestIssueComments("example/repo", 900)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.Complete || len(listing.Comments) != newestIssueCommentLimit {
		t.Fatalf("listing = complete:%t comments:%d", listing.Complete, len(listing.Comments))
	}
}

func TestBoundedPullRequestListingsExposeCapForOpenAndClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/pulls" || r.URL.Query().Get("base") != "agent" || r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("request = %s", r.URL.String())
		}
		count := 1
		if r.URL.Query().Get("state") == "open" {
			count = 100
		} else if r.URL.Query().Get("state") != "closed" {
			t.Fatalf("state = %q", r.URL.Query().Get("state"))
		}
		pullRequests := make([]map[string]any, count)
		for index := range pullRequests {
			pullRequests[index] = map[string]any{"number": index + 1, "node_id": fmt.Sprintf("PR_%d", index+1), "base": map[string]any{"ref": "agent"}, "head": map[string]any{"ref": "agent-work/test"}}
		}
		json.NewEncoder(w).Encode(pullRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	open, err := client.ListOpenPullRequestsBounded("example/repo", "agent")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := client.ListClosedPullRequestsBounded("example/repo", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if open.Complete || len(open.PullRequests) != 100 || open.PullRequests[0].NodeID != "PR_1" || !closed.Complete || len(closed.PullRequests) != 1 {
		t.Fatalf("open=%+v closed=%+v", open, closed)
	}
}

func TestClosedPullRequestListingPaginatesToUpdatedBoundary(t *testing.T) {
	boundary := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/repo/pulls" || r.URL.Query().Get("state") != "closed" || r.URL.Query().Get("base") != "agent" || r.URL.Query().Get("sort") != "updated" || r.URL.Query().Get("direction") != "desc" {
			t.Fatalf("request = %s", r.URL.String())
		}
		page := r.URL.Query().Get("page")
		if page == "1" {
			pullRequests := make([]map[string]any, 100)
			for index := range pullRequests {
				pullRequests[index] = map[string]any{"number": index + 1, "node_id": fmt.Sprintf("PR_%d", index+1), "updated_at": boundary.Format(time.RFC3339)}
			}
			json.NewEncoder(w).Encode(pullRequests)
			return
		}
		if page != "2" {
			t.Fatalf("page = %q", page)
		}
		json.NewEncoder(w).Encode([]map[string]any{{"number": 101, "node_id": "PR_101", "updated_at": boundary.Add(-time.Second).Format(time.RFC3339)}})
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).ListClosedPullRequestsUpdatedSinceContext(context.Background(), "example/repo", "agent", boundary)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.Complete || len(listing.PullRequests) != 100 || listing.PullRequests[99].Number != 100 || !listing.PullRequests[0].UpdatedAt.Equal(boundary) {
		t.Fatalf("listing = complete:%t pullRequests:%d last:%+v", listing.Complete, len(listing.PullRequests), listing.PullRequests[len(listing.PullRequests)-1])
	}
}

func TestClosedPullRequestListingRejectsAnUnstableNewestPage(t *testing.T) {
	boundary := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		number := calls
		json.NewEncoder(w).Encode([]map[string]any{{"number": number, "node_id": fmt.Sprintf("PR_%d", number), "updated_at": boundary.Format(time.RFC3339)}})
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).ListClosedPullRequestsUpdatedSinceContext(context.Background(), "example/repo", "agent", boundary)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Complete || calls != 2 {
		t.Fatalf("listing = %+v calls=%d", listing, calls)
	}
}

func TestClosedPullRequestListingIncludesTheFractionalBoundarySecond(t *testing.T) {
	boundary := time.Date(2026, 7, 15, 1, 0, 0, 500_000_000, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"number": 1, "node_id": "PR_1", "updated_at": "2026-07-15T01:00:00Z"}})
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).ListClosedPullRequestsUpdatedSinceContext(context.Background(), "example/repo", "agent", boundary)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.Complete || len(listing.PullRequests) != 1 {
		t.Fatalf("listing = %+v", listing)
	}
}

func TestCompareCommitsAndRerequestExactCheckRun(t *testing.T) {
	var rerequested bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/repos/example/repo/compare/base%2Fref...head%2Fref":
			w.Write([]byte(`{"status":"ahead","ahead_by":2,"behind_by":0,"total_commits":2,"merge_base_commit":{"sha":"base-sha"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/check-runs/123/rerequest":
			rerequested = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("request = %s %s escaped=%s", r.Method, r.URL.String(), r.URL.EscapedPath())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	comparison, err := client.CompareCommits("example/repo", "base/ref", "head/ref")
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Status != "ahead" || comparison.AheadBy != 2 || comparison.MergeBaseSHA != "base-sha" {
		t.Fatalf("comparison = %+v", comparison)
	}
	if err := client.RerequestCheckRun("example/repo", 123); err != nil {
		t.Fatal(err)
	}
	if !rerequested {
		t.Fatal("check run was not rerequested")
	}
}
