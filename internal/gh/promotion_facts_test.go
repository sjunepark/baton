package gh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchPromotionHistoryPaginatesAssociationsAndDeduplicatesWorkPRs(t *testing.T) {
	graphqlCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/example/repo/compare/"):
			json.NewEncoder(w).Encode(map[string]any{
				"total_commits": 2,
				"commits":       []map[string]any{{"node_id": "commit-1"}, {"node_id": "commit-2"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			graphqlCalls++
			var request graphQLPayload
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(request.Query, "nodes(ids:") {
				writePromotionCommitNodes(w, []any{
					promotionCommitPayload("commit-1", []map[string]any{
						promotionPRPayload(10, "agent", "agent-work/10", "Refs #10"),
					}, true, "next-association"),
					promotionCommitPayload("commit-2", []map[string]any{
						promotionPRPayload(10, "agent", "agent-work/10", "Refs #10"),
						promotionPRPayload(11, "agent", "manual/11", "Refs #11"),
					}, false, ""),
				})
				return
			}
			if request.Variables["id"] != "commit-1" || request.Variables["cursor"] != "next-association" {
				t.Fatalf("variables = %#v", request.Variables)
			}
			writePromotionCommitNode(w, promotionCommitPayload("commit-1", []map[string]any{
				promotionPRPayload(12, "agent", "agent-work/12", "Refs #12"),
			}, false, ""))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	history, err := NewClient(server.URL, "token", server.Client()).FetchPromotionHistory("example/repo", "base", "head", "agent", "agent-work/")
	if err != nil {
		t.Fatal(err)
	}
	if !history.Complete || graphqlCalls != 2 || len(history.WorkPullRequests) != 2 || history.WorkPullRequests[0].Number != 10 || history.WorkPullRequests[1].Number != 12 {
		t.Fatalf("history = %+v graphqlCalls=%d", history, graphqlCalls)
	}
}

func TestFetchPromotionHistoryPaginatesComparison(t *testing.T) {
	comparePages := []string{}
	graphqlBatches := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			page := r.URL.Query().Get("page")
			comparePages = append(comparePages, page)
			count := 100
			if page == "2" {
				count = 1
			}
			commits := make([]map[string]any, count)
			for index := range commits {
				commits[index] = map[string]any{"node_id": fmt.Sprintf("commit-%s-%d", page, index)}
			}
			json.NewEncoder(w).Encode(map[string]any{"total_commits": 101, "commits": commits})
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var request graphQLPayload
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			ids := request.Variables["ids"].([]any)
			graphqlBatches = append(graphqlBatches, len(ids))
			nodes := make([]any, len(ids))
			for index, id := range ids {
				nodes[index] = promotionCommitPayload(id.(string), nil, false, "")
			}
			writePromotionCommitNodes(w, nodes)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	history, err := NewClient(server.URL, "token", server.Client()).FetchPromotionHistory("example/repo", "base", "head", "agent", "agent-work/")
	if err != nil {
		t.Fatal(err)
	}
	if !history.Complete || fmt.Sprint(comparePages) != "[1 2]" || fmt.Sprint(graphqlBatches) != "[50 50 1]" {
		t.Fatalf("history=%+v comparePages=%v graphqlBatches=%v", history, comparePages, graphqlBatches)
	}
}

func TestFetchPromotionHistoryReportsComparisonCapAsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		json.NewEncoder(w).Encode(map[string]any{"total_commits": PromotionCommitCap + 1, "commits": []any{}})
	}))
	defer server.Close()

	history, err := NewClient(server.URL, "token", server.Client()).FetchPromotionHistory("example/repo", "base", "head", "agent", "agent-work/")
	if err != nil {
		t.Fatal(err)
	}
	if history.Complete || len(history.WorkPullRequests) != 0 {
		t.Fatalf("history = %+v", history)
	}
}

func TestFetchPromotionHistoryReportsMissingCommitAssociationAsIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"total_commits": 2,
				"commits":       []map[string]any{{"node_id": "commit-1"}, {"node_id": "commit-2"}},
			})
			return
		}
		writePromotionCommitNodes(w, []any{promotionCommitPayload("commit-1", nil, false, "")})
	}))
	defer server.Close()

	history, err := NewClient(server.URL, "token", server.Client()).FetchPromotionHistory("example/repo", "base", "head", "agent", "agent-work/")
	if err != nil {
		t.Fatal(err)
	}
	if history.Complete {
		t.Fatalf("history = %+v", history)
	}
}

func TestFetchPromotionHistoryReportsAssociationCapAsIncomplete(t *testing.T) {
	graphqlPage := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"total_commits": 1, "commits": []map[string]any{{"node_id": "commit-1"}}})
			return
		}
		graphqlPage++
		count := 100
		if graphqlPage == 3 {
			count = 50
		}
		pullRequests := make([]map[string]any, count)
		for index := range pullRequests {
			pullRequests[index] = promotionPRPayload(graphqlPage*100+index, "main", "feature", "")
		}
		commit := promotionCommitPayload("commit-1", pullRequests, true, fmt.Sprintf("cursor-%d", graphqlPage))
		if graphqlPage == 1 {
			writePromotionCommitNodes(w, []any{commit})
			return
		}
		writePromotionCommitNode(w, commit)
	}))
	defer server.Close()

	history, err := NewClient(server.URL, "token", server.Client()).FetchPromotionHistory("example/repo", "base", "head", "agent", "agent-work/")
	if err != nil {
		t.Fatal(err)
	}
	if history.Complete || graphqlPage != 3 {
		t.Fatalf("history=%+v graphqlPage=%d", history, graphqlPage)
	}
}

func promotionPRPayload(number int, baseRef, headRef, body string) map[string]any {
	return map[string]any{
		"number": number, "title": fmt.Sprintf("PR %d", number), "body": body,
		"mergedAt": "2026-07-14T00:00:00Z", "baseRefName": baseRef, "headRefName": headRef,
		"baseRepository": map[string]any{"nameWithOwner": "example/repo"},
	}
}

func promotionCommitPayload(id string, pullRequests []map[string]any, hasNext bool, cursor string) map[string]any {
	return map[string]any{
		"id": id, "oid": id,
		"associatedPullRequests": map[string]any{
			"nodes":    pullRequests,
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cursor},
		},
	}
}

func writePromotionCommitNodes(w http.ResponseWriter, nodes []any) {
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"nodes": nodes}})
}

func writePromotionCommitNode(w http.ResponseWriter, node map[string]any) {
	json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"node": node}})
}
