package gh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestListOpenIssuesPaginatesPastOneHundred(t *testing.T) {
	pages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		count := 100
		if page == 2 {
			count = 1
		}
		items := make([]map[string]any, count)
		for i := range items {
			number := (page-1)*100 + i + 1
			items[i] = map[string]any{"number": number, "title": fmt.Sprintf("Issue %d", number), "labels": []any{}}
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	issues, err := NewClient(server.URL, "token", server.Client()).ListOpenIssues("example/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 101 || issues[100].Number != 101 || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("issues=%d last=%+v pages=%v", len(issues), issues[100], pages)
	}
}

func TestGetIssueContextDecodesPullRequestDiscriminator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"number": 7, "state": "open", "locked": true, "comments": 17, "pull_request": map[string]any{"url": "https://api.example/pulls/7"}, "labels": []any{}})
	}))
	defer server.Close()
	issue, err := NewClient(server.URL, "token", server.Client()).GetIssueContext(t.Context(), "example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !issue.PullRequest || !issue.Locked || issue.CommentCount != 17 {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestListOpenPullRequestsPaginatesPastOneHundred(t *testing.T) {
	pages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		count := 100
		if page == 2 {
			count = 1
		}
		items := make([]map[string]any, count)
		for i := range items {
			number := (page-1)*100 + i + 1
			items[i] = map[string]any{
				"number": number, "title": fmt.Sprintf("PR %d", number),
				"base": map[string]any{"ref": "agent"}, "head": map[string]any{"ref": fmt.Sprintf("work-%d", number), "sha": fmt.Sprintf("sha-%d", number)},
			}
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	prs, err := NewClient(server.URL, "token", server.Client()).ListOpenPullRequests("example/repo", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 101 || prs[100].Number != 101 || fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("prs=%d last=%+v pages=%v", len(prs), prs[100], pages)
	}
}

func TestGetCheckRollupPaginatesCheckRunsAndStatuses(t *testing.T) {
	checkPages, statusPages := []int{}, []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		switch r.URL.Path {
		case "/repos/example/repo/commits/sha/check-runs":
			checkPages = append(checkPages, page)
			count := 100
			if page == 2 {
				count = 1
			}
			runs := make([]map[string]any, count)
			for i := range runs {
				conclusion := "success"
				if page == 2 {
					conclusion = "failure"
				}
				runs[i] = map[string]any{"id": (page-1)*100 + i + 1, "name": fmt.Sprintf("run-%d-%d", page, i), "status": "completed", "conclusion": conclusion}
			}
			json.NewEncoder(w).Encode(map[string]any{"total_count": 101, "check_runs": runs})
		case "/repos/example/repo/commits/sha/statuses":
			statusPages = append(statusPages, page)
			count := 100
			if page == 2 {
				count = 1
			}
			statuses := make([]map[string]any, count)
			for i := range statuses {
				statuses[i] = map[string]any{"context": fmt.Sprintf("status-%d-%d", page, i), "state": "success"}
			}
			json.NewEncoder(w).Encode(statuses)
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	rollup, err := NewClient(server.URL, "token", server.Client()).GetCheckRollup("example/repo", 7, "sha")
	if err != nil {
		t.Fatal(err)
	}
	if rollup.State != "failure" || rollup.Count != 202 || rollup.Checks[0].ID != 1 || rollup.Checks[100].ID != 101 || !rollup.Complete || fmt.Sprint(checkPages) != "[1 2]" || fmt.Sprint(statusPages) != "[1 2]" {
		t.Fatalf("rollup=%+v checkPages=%v statusPages=%v", rollup, checkPages, statusPages)
	}
}

func TestGetCheckRollupDistinguishesExactGitHubLimitFromTruncation(t *testing.T) {
	for _, test := range []struct {
		name     string
		total    int
		complete bool
	}{{name: "below", total: 999, complete: true}, {name: "exact", total: 1000, complete: true}, {name: "above", total: 1001, complete: false}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					remaining := test.total - (page-1)*100
					count := min(100, max(0, remaining))
					if test.total > 1000 && page == 10 {
						count = 100
					}
					runs := make([]map[string]any, count)
					for index := range runs {
						runs[index] = map[string]any{"name": fmt.Sprintf("check-%d-%d", page, index), "status": "completed", "conclusion": "success"}
					}
					json.NewEncoder(w).Encode(map[string]any{"total_count": test.total, "check_runs": runs})
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					w.Write([]byte(`[]`))
				default:
					t.Fatalf("path = %s", r.URL.String())
				}
			}))
			defer server.Close()

			rollup, err := NewClient(server.URL, "token", server.Client()).GetCheckRollup("example/repo", 7, "sha")
			if err != nil {
				t.Fatal(err)
			}
			if rollup.Complete != test.complete {
				t.Fatalf("complete = %v, want %v; count=%d warnings=%v", rollup.Complete, test.complete, rollup.Count, rollup.Warnings)
			}
		})
	}
}

func TestListIssueCommentsPaginatesPastPolicyMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		count := 100
		if page == 2 {
			count = 1
		}
		comments := make([]map[string]any, count)
		for i := range comments {
			comments[i] = map[string]any{"id": (page-1)*100 + i + 1, "body": "ordinary comment"}
		}
		if page == 2 {
			comments[0]["body"] = "<!-- baton-policy -->"
		}
		json.NewEncoder(w).Encode(comments)
	}))
	defer server.Close()

	comments, err := NewClient(server.URL, "token", server.Client()).ListIssueComments("example/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 101 || comments[100].ID != 101 || comments[100].Body != "<!-- baton-policy -->" {
		t.Fatalf("comments=%d last=%+v", len(comments), comments[100])
	}
}

func TestListLabelsPaginatesPastOneHundred(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		count := 100
		if page == 2 {
			count = 1
		}
		labels := make([]Label, count)
		for i := range labels {
			labels[i] = Label{Name: fmt.Sprintf("label-%d", (page-1)*100+i+1)}
		}
		json.NewEncoder(w).Encode(labels)
	}))
	defer server.Close()

	labels, err := NewClient(server.URL, "token", server.Client()).ListLabels("example/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 101 || labels[100].Name != "label-101" {
		t.Fatalf("labels=%d last=%+v", len(labels), labels[100])
	}
}
