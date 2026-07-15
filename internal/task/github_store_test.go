package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/task"
)

func TestGitHubStoreListsManagedIssuesWithCompletePagination(t *testing.T) {
	t.Parallel()
	var requestsMu sync.Mutex
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.URL.RequestURI())
		requestsMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/repos/example/repo/issues" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("state") != "open" || query.Get("labels") != task.LabelManaged || query.Get("per_page") != "100" {
			t.Fatalf("query = %v", query)
		}
		page := query.Get("page")
		if page == "1" {
			batch := make([]map[string]any, 100)
			for i := range batch {
				batch[i] = githubIssuePayload(i+1, "open", []string{task.LabelManaged, "agent:ready-trivial"})
			}
			batch[99]["pull_request"] = map[string]any{"url": "https://api.github.test/pulls/100"}
			writeJSON(t, w, batch)
			return
		}
		if page == "2" {
			writeJSON(t, w, []map[string]any{githubIssuePayload(101, "open", []string{task.LabelManaged, "agent:ready-bounded"})})
			return
		}
		t.Fatalf("unexpected page %q", page)
	}))
	defer server.Close()

	client := gh.NewClient(server.URL, "secret", server.Client())
	service := task.NewService(task.NewGitHubStore(client))
	tasks, err := service.List(context.Background(), repository, task.ListOpen)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 100 || tasks[0].Number != 1 || tasks[len(tasks)-1].Number != 101 {
		t.Fatalf("tasks = %d, first %d, last %d", len(tasks), tasks[0].Number, tasks[len(tasks)-1].Number)
	}
	next, err := service.Next(context.Background(), repository)
	if err != nil || next == nil || next.Number != 1 {
		t.Fatalf("Next() = %#v, %v", next, err)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("requests = %v", requests)
	}
	for _, request := range requests {
		for _, forbidden := range []string{"comments", "pulls", "branches", "check", "reviews", "commits", "rules", "delivery"} {
			if strings.Contains(request, forbidden) {
				t.Fatalf("Task list requested forbidden resource %q: %s", forbidden, request)
			}
		}
	}
}

func TestGitHubStoreSupportsAllIssueStatesAndRejectsPullRequestDetail(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/example/repo/issues" && r.URL.Query().Get("state") == "all":
			writeJSON(t, w, []map[string]any{
				githubIssuePayload(1, "open", []string{task.LabelManaged, "agent:ready-trivial"}),
				githubIssuePayload(2, "closed", []string{task.LabelManaged, "agent:ready-bounded"}),
			})
		case r.URL.Path == "/repos/example/repo/issues/3":
			payload := githubIssuePayload(3, "open", []string{task.LabelManaged})
			payload["pull_request"] = map[string]any{"url": "https://api.github.test/pulls/3"}
			writeJSON(t, w, payload)
		default:
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	service := task.NewService(task.NewGitHubStore(gh.NewClient(server.URL, "", server.Client())))
	tasks, err := service.List(context.Background(), repository, task.ListAll)
	if err != nil || len(tasks) != 2 || tasks[1].State != task.StateDone {
		t.Fatalf("List(all) = %#v, %v", tasks, err)
	}
	_, err = service.Show(context.Background(), repository, 3, false)
	var taskErr *task.Error
	if !errors.As(err, &taskErr) || taskErr.Code != "not_issue" {
		t.Fatalf("Show(PR) error = %#v", err)
	}
}

func TestGitHubStoreMutationCreatesLabelsWithoutUpdatingExistingMetadata(t *testing.T) {
	t.Parallel()
	type issueState struct {
		labels []string
		state  string
	}
	current := issueState{labels: []string{"bug"}, state: "open"}
	definitions := map[string]map[string]any{}
	requests := []string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/issues/7":
			writeJSON(t, w, githubIssuePayload(7, current.state, current.labels))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/example/repo/labels/"):
			name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/labels/"))
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			definition, exists := definitions[strings.ToLower(name)]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(t, w, map[string]any{"message": "Not Found"})
				return
			}
			writeJSON(t, w, definition)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/labels":
			var definition map[string]any
			if err := json.NewDecoder(r.Body).Decode(&definition); err != nil {
				t.Fatal(err)
			}
			definitions[strings.ToLower(definition["name"].(string))] = definition
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, definition)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/issues/7/labels":
			var input struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if len(input.Labels) != 1 || input.Labels[0] == "" {
				t.Errorf("add-label body = %#v", input)
			}
			current.labels = append(current.labels, input.Labels...)
			writeJSON(t, w, []map[string]any{})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/example/repo/issues/7/labels/"):
			name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/repos/example/repo/issues/7/labels/"))
			if err != nil {
				t.Fatal(err)
			}
			filtered := current.labels[:0]
			for _, label := range current.labels {
				if !strings.EqualFold(label, name) {
					filtered = append(filtered, label)
				}
			}
			current.labels = filtered
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/example/repo/issues/7":
			var input struct {
				State string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if input.State != "closed" {
				t.Errorf("close body = %#v", input)
			}
			current.state = "closed"
			w.WriteHeader(http.StatusOK)
			writeJSON(t, w, githubIssuePayload(7, current.state, current.labels))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()
	service := task.NewService(task.NewGitHubStore(gh.NewClient(server.URL, "", server.Client())))
	trivial, p1 := task.ModeTrivial, task.PriorityP1
	result, err := service.Mutate(context.Background(), repository, 7, task.Mutation{
		Kind: task.MutationEnroll, ModeSet: true, Mode: &trivial, PrioritySet: true, Priority: &p1,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Task == nil || result.Task.State != task.StateReady || len(result.Changes) != 3 || len(definitions) != 3 {
		t.Fatalf("enroll = %#v, definitions = %#v", result, definitions)
	}
	result, err = service.Mutate(context.Background(), repository, 7, task.Mutation{Kind: task.MutationStart}, false)
	if err != nil || result.Task == nil || result.Task.State != task.StateInProgress {
		t.Fatalf("start = %#v, %v", result, err)
	}
	result, err = service.Mutate(context.Background(), repository, 7, task.Mutation{Kind: task.MutationClose}, false)
	if err != nil || result.Task == nil || result.Task.State != task.StateDone || result.Task.InProgress {
		t.Fatalf("close = %#v, %v", result, err)
	}
	for _, request := range requests {
		if strings.HasPrefix(request, http.MethodPatch+" /repos/example/repo/labels/") {
			t.Fatalf("existing label metadata was updated: %s", request)
		}
	}
}

func TestGitHubStoreConfirmsConcurrentLabelCreation(t *testing.T) {
	t.Parallel()
	listCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/labels/baton:managed":
			listCount++
			if listCount == 1 {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(t, w, map[string]any{"message": "Not Found"})
			} else {
				writeJSON(t, w, map[string]any{"name": task.LabelManaged, "color": "ffffff"})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/labels":
			w.WriteHeader(http.StatusUnprocessableEntity)
			writeJSON(t, w, map[string]any{"message": "Validation Failed", "secret": "do-not-leak"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()
	created, err := task.NewGitHubStore(gh.NewClient(server.URL, "", server.Client())).EnsureLabel(context.Background(), repository, task.LabelDefinition{Name: task.LabelManaged})
	if err != nil || created || listCount != 2 {
		t.Fatalf("EnsureLabel() = %v, %v, lists %d", created, err, listCount)
	}
}

func TestGitHubStoreRedactsErrorResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(t, w, map[string]any{"message": "token super-secret exploded"})
	}))
	defer server.Close()
	_, err := task.NewGitHubStore(gh.NewClient(server.URL, "super-secret", server.Client())).ListIssues(context.Background(), repository, task.ListOpen)
	var taskErr *task.Error
	if err == nil || !errors.As(err, &taskErr) || taskErr.Code != "github_error" || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "exploded") {
		t.Fatalf("error leaked response or credential: %v", err)
	}
}

func TestGitHubStoreConfirmsOnlyAlreadyAbsentLabelDeletion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		issueExists bool
		wantCode    string
	}{
		{name: "issue exists and label is absent", issueExists: true},
		{name: "issue is unavailable", wantCode: "not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodDelete && r.URL.Path == "/repos/example/repo/issues/8/labels/baton:in-progress":
					w.WriteHeader(http.StatusNotFound)
					writeJSON(t, w, map[string]any{"message": "Not Found"})
				case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/issues/8" && test.issueExists:
					writeJSON(t, w, githubIssuePayload(8, "open", []string{task.LabelManaged, "agent:ready-trivial"}))
				case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/issues/8":
					w.WriteHeader(http.StatusNotFound)
					writeJSON(t, w, map[string]any{"message": "Not Found"})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			err := task.NewGitHubStore(gh.NewClient(server.URL, "", server.Client())).RemoveLabel(context.Background(), repository, 8, task.LabelInProgress)
			if test.wantCode == "" {
				if err != nil {
					t.Fatalf("RemoveLabel() error = %v", err)
				}
				return
			}
			var taskErr *task.Error
			if !errors.As(err, &taskErr) || taskErr.Code != test.wantCode {
				t.Fatalf("RemoveLabel() error = %#v", err)
			}
		})
	}
}

func TestGitHubStoreMutationTranslatesPartialFailure(t *testing.T) {
	t.Parallel()
	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/issues/9":
			getCount++
			writeJSON(t, w, githubIssuePayload(9, "open", []string{task.LabelManaged}))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/example/repo/labels/agent:ready-trivial":
			writeJSON(t, w, map[string]any{"name": "agent:ready-trivial", "color": "0e8a16"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/example/repo/issues/9/labels":
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(t, w, map[string]any{"message": "internal detail"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	trivial := task.ModeTrivial
	_, err := task.NewService(task.NewGitHubStore(gh.NewClient(server.URL, "", server.Client()))).Mutate(
		context.Background(), repository, 9, task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &trivial}, false,
	)
	var mutationErr *task.MutationError
	var taskErr *task.Error
	if !errors.As(err, &mutationErr) || !errors.As(err, &taskErr) || taskErr.Code != "github_error" || mutationErr.Task == nil || getCount != 2 {
		t.Fatalf("Mutate() error = %#v", err)
	}
}

func githubIssuePayload(number int, state string, labels []string) map[string]any {
	labelPayload := make([]map[string]string, len(labels))
	for i, label := range labels {
		labelPayload[i] = map[string]string{"name": label}
	}
	return map[string]any{
		"number": number, "node_id": fmt.Sprintf("I_%d", number), "title": fmt.Sprintf("Issue %d", number),
		"html_url": fmt.Sprintf("https://github.test/example/repo/issues/%d", number), "body": "body",
		"state": state, "labels": labelPayload,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
