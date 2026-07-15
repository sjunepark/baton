package gh

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchCommitListingDetectsCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example-org/example-repo/pulls/10/commits" {
			t.Fatalf("unexpected path %s", r.URL.String())
		}
		page := r.URL.Query().Get("page")
		count := 100
		if page == "3" {
			count = 50
		}
		items := make([]map[string]any, count)
		for i := range items {
			items[i] = map[string]any{"commit": map[string]any{"message": "Meaningful commit"}}
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	listing, err := client.FetchCommitListing("example-org/example-repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Messages) != 250 || listing.Count != 250 || !listing.GitHubCapReached {
		t.Fatalf("listing=%#v", listing)
	}
}

func TestFetchCommitListingBelowGitHubCapIsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"commit":{"message":"Meaningful commit"}}]`))
	}))
	defer server.Close()

	listing, err := NewClient(server.URL, "token", server.Client()).FetchCommitListing("example/repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Count != 1 || listing.GitHubCapReached {
		t.Fatalf("listing=%#v", listing)
	}
}

func TestCreateIssueComment(t *testing.T) {
	var sawComment bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/example-org/example-repo/issues/12/comments" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		sawComment = true
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if err := client.CreateIssueComment("example-org/example-repo", 12, "done"); err != nil {
		t.Fatal(err)
	}
	if !sawComment {
		t.Fatal("comment was not posted")
	}
}

func TestCloseIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/example/repo/issues/7" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["state"] != "closed" {
			t.Fatalf("body = %#v", body)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	if err := NewClient(server.URL, "token", server.Client()).CloseIssue("example/repo", 7); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorPreservesSafeRetryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "request-123")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"message":"token=secret"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "secret", server.Client())
	err := client.CreateIssueComment("example/repo", 1, "done")
	var apiErr APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.RequestID != "request-123" || apiErr.RetryAfter != 7*time.Second || !apiErr.UpstreamRetryable() {
		t.Fatalf("api error = %+v", apiErr)
	}
	if strings.Contains(apiErr.Error(), "secret") {
		t.Fatalf("safe error leaked response body: %q", apiErr.Error())
	}
}

func TestAPIErrorRecognizesPrimaryRateLimitReset(t *testing.T) {
	reset := time.Now().Add(2 * time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "request-rate")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	err := NewClient(server.URL, "token", server.Client()).CreateIssueComment("example/repo", 1, "done")
	var apiErr APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if !apiErr.RateLimited || !apiErr.UpstreamRetryable() || !apiErr.RateLimitReset.Equal(reset) || apiErr.RetryAfter <= 0 {
		t.Fatalf("api error = %+v", apiErr)
	}
	if apiErr.UpstreamDetails()["rateLimited"] != "true" {
		t.Fatalf("details = %v", apiErr.UpstreamDetails())
	}
}

func TestAPIErrorDoesNotTreatOrdinaryResetHeaderAsRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "42")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(2*time.Minute).Unix()))
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	err := NewClient(server.URL, "token", server.Client()).CreateIssueComment("example/repo", 1, "done")
	var apiErr APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.RateLimited || apiErr.UpstreamRetryable() || apiErr.RetryAfter != 0 || !apiErr.RateLimitReset.IsZero() {
		t.Fatalf("api error = %+v", apiErr)
	}
}

func TestGraphQLResponseRecognizesRateLimitError(t *testing.T) {
	reset := time.Now().Add(2 * time.Minute).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "graphql-rate")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.Write([]byte(`{"errors":[{"message":"API rate limit exceeded","extensions":{"type":"RATE_LIMITED"}}]}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "token", server.Client()).GetReviewThreads("example/repo", 7)
	var apiErr APIError
	if !errors.As(err, &apiErr) || !apiErr.RateLimited || !apiErr.UpstreamRetryable() || apiErr.RequestID != "graphql-rate" || !apiErr.RateLimitReset.Equal(reset) {
		t.Fatalf("error = %T %+v", err, apiErr)
	}
}

func TestGraphQLResponseRecognizesRateLimitTypeWithoutHeaders(t *testing.T) {
	graphQLErr := graphQLError{}
	graphQLErr.Extensions.Type = "RATE_LIMITED"
	apiErr := graphQLResponseError([]graphQLError{graphQLErr}, http.Header{})
	if !apiErr.RateLimited || !apiErr.UpstreamRetryable() {
		t.Fatalf("api error = %+v", apiErr)
	}
}

func TestGetBranchHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example-org/example-repo/git/ref/heads/agent":
			w.Write([]byte(`{"object":{"sha":"abc123"}}`))
		case "/repos/example-org/example-repo/commits/abc123/check-runs":
			w.Write([]byte(`{"check_runs":[{"id":123,"name":"test","status":"completed","conclusion":"failure","html_url":"https://example/check"}]}`))
		case "/repos/example-org/example-repo/commits/abc123/statuses":
			w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	health, err := client.GetBranchHealth("example-org/example-repo", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if health.Ref != "agent" || health.SHA != "abc123" || health.CheckState != "failure" {
		t.Fatalf("health = %#v", health)
	}
}

func TestBuildCheckRollupIncludesSummaryAndHelp(t *testing.T) {
	rollup := buildCheckRollup("example-org/example-repo", 10, "abc123", []CheckState{
		{Name: "unit", Status: "completed", Conclusion: "success"},
		{Name: "lint", Status: "completed", Conclusion: "failure"},
		{Name: "deploy", Status: "in_progress"},
		{Name: "optional", Status: "completed", Conclusion: "skipped"},
	})
	if rollup.Count != 4 || rollup.Summary.Passed != 1 || rollup.Summary.Failed != 1 || rollup.Summary.Pending != 1 || rollup.Summary.Skipped != 1 {
		t.Fatalf("rollup summary = %#v count=%d", rollup.Summary, rollup.Count)
	}
	if rollup.State != "failure" {
		t.Fatalf("state = %q, want failure", rollup.State)
	}
	if len(rollup.Help) == 0 {
		t.Fatal("rollup help should include next commands")
	}
}

func TestGetReviewThreadsPaginatesThreadsAndComments(t *testing.T) {
	var sawSecondThreadPage, sawSecondCommentPage bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(payload.Query, "pullRequest(number: $number)"):
			cursor, _ := payload.Variables["threadsCursor"].(string)
			if cursor == "" {
				writeReviewThreadsResponse(w, []map[string]any{
					reviewThreadPayload("thread-1", "main.go", reviewCommentPayloads(100), true, "comment-page-2"),
				}, true, "thread-page-2")
				return
			}
			if cursor != "thread-page-2" {
				t.Fatalf("threadsCursor = %v, want thread-page-2", payload.Variables["threadsCursor"])
			}
			sawSecondThreadPage = true
			writeReviewThreadsResponse(w, []map[string]any{
				reviewThreadPayload("thread-2", "other.go", reviewCommentPayloads(1), false, ""),
			}, false, "")
		case strings.Contains(payload.Query, "node(id: $id)"):
			if payload.Variables["id"] != "thread-1" || payload.Variables["commentsCursor"] != "comment-page-2" {
				t.Fatalf("comment variables = %#v", payload.Variables)
			}
			sawSecondCommentPage = true
			writeReviewThreadCommentsResponse(w, reviewCommentPayloads(1), false, "")
		default:
			t.Fatalf("unexpected GraphQL query %s", payload.Query)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	result, err := client.GetReviewThreads("example-org/example-repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !sawSecondThreadPage || !sawSecondCommentPage {
		t.Fatalf("sawSecondThreadPage=%v sawSecondCommentPage=%v", sawSecondThreadPage, sawSecondCommentPage)
	}
	if len(result.Threads) != 2 {
		t.Fatalf("threads = %d, want 2", len(result.Threads))
	}
	if len(result.Threads[0].Comments) != 101 {
		t.Fatalf("first thread comments = %d, want 101", len(result.Threads[0].Comments))
	}
	if result.Count != 2 || !result.Complete || result.Summary.Total != 2 || result.Summary.Unresolved != 2 || result.Summary.HumanUnresolved != 2 {
		t.Fatalf("thread summary = %#v count=%d", result.Summary, result.Count)
	}
	if len(result.Help) == 0 {
		t.Fatal("review thread help should include next commands")
	}
}

func writeReviewThreadsResponse(w http.ResponseWriter, nodes []map[string]any, hasNext bool, endCursor string) {
	json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{
					"reviewThreads": map[string]any{
						"nodes": nodes,
						"pageInfo": map[string]any{
							"hasNextPage": hasNext,
							"endCursor":   endCursor,
						},
					},
				},
			},
		},
	})
}

func writeReviewThreadCommentsResponse(w http.ResponseWriter, nodes []map[string]any, hasNext bool, endCursor string) {
	json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"node": map[string]any{
				"comments": map[string]any{
					"nodes": nodes,
					"pageInfo": map[string]any{
						"hasNextPage": hasNext,
						"endCursor":   endCursor,
					},
				},
			},
		},
	})
}

func reviewThreadPayload(id, path string, comments []map[string]any, commentsHasNext bool, commentsEndCursor string) map[string]any {
	return map[string]any{
		"id":         id,
		"isResolved": false,
		"isOutdated": false,
		"path":       path,
		"line":       12,
		"comments": map[string]any{
			"nodes": comments,
			"pageInfo": map[string]any{
				"hasNextPage": commentsHasNext,
				"endCursor":   commentsEndCursor,
			},
		},
	}
}

func reviewCommentPayloads(count int) []map[string]any {
	comments := make([]map[string]any, count)
	for i := range comments {
		comments[i] = map[string]any{
			"body": fmt.Sprintf("comment %d", i+1),
			"url":  fmt.Sprintf("https://example.test/comment/%d", i+1),
			"author": map[string]any{
				"login":      "reviewer",
				"__typename": "User",
			},
		}
	}
	return comments
}
