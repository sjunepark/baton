package gh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

func TestFetchCommitListingDetectsCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/open-creo/creo/pulls/10/commits" {
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
	messages, reachedCap, err := client.FetchCommitListing("open-creo/creo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 250 || !reachedCap {
		t.Fatalf("messages=%d reachedCap=%v", len(messages), reachedCap)
	}
}

func TestApplyIssueDecisionUsesLabelsAndPolicyComment(t *testing.T) {
	var sawAdd, sawRemove, sawPatch bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/open-creo/creo/issues/12/labels":
			sawAdd = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/open-creo/creo/issues/12/labels/agent:blocked":
			sawRemove = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/open-creo/creo/issues/12/comments":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"id":99,"body":"<!-- creo-agent-issue-policy:v1 -->\nold"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/open-creo/creo/issues/comments/99":
			sawPatch = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	cfg := config.DefaultCreoCompat()
	decision := policy.IssuePolicyDecision{
		IsFormIssue:       true,
		LabelsToAdd:       []string{"bug"},
		LabelsToRemove:    []string{"agent:blocked"},
		PolicyCommentBody: nil,
	}
	client := NewClient(server.URL, "token", server.Client())
	if err := client.ApplyIssueDecision("open-creo/creo", 12, decision, cfg.IssuePolicy.PolicyCommentMarker); err != nil {
		t.Fatal(err)
	}
	if !sawAdd || !sawRemove || !sawPatch {
		t.Fatalf("sawAdd=%v sawRemove=%v sawPatch=%v", sawAdd, sawRemove, sawPatch)
	}
}

func TestCreateIssueComment(t *testing.T) {
	var sawComment bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/open-creo/creo/issues/12/comments" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		sawComment = true
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	if err := client.CreateIssueComment("open-creo/creo", 12, "done"); err != nil {
		t.Fatal(err)
	}
	if !sawComment {
		t.Fatal("comment was not posted")
	}
}

func TestGetBranchHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/open-creo/creo/git/ref/heads/agent":
			w.Write([]byte(`{"object":{"sha":"abc123"}}`))
		case "/repos/open-creo/creo/commits/abc123/check-runs":
			w.Write([]byte(`{"check_runs":[{"name":"test","status":"completed","conclusion":"failure","html_url":"https://example/check"}]}`))
		case "/repos/open-creo/creo/commits/abc123/status":
			w.Write([]byte(`{"statuses":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	health, err := client.GetBranchHealth("open-creo/creo", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if health.Ref != "agent" || health.SHA != "abc123" || health.CheckState != "failure" {
		t.Fatalf("health = %#v", health)
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
	result, err := client.GetReviewThreads("open-creo/creo", 10)
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
				"login": "reviewer",
			},
		}
	}
	return comments
}
