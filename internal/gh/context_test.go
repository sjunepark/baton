package gh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCommitListingDoesNotReturnPartialFactsAfterDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			commits := make([]map[string]any, 100)
			for index := range commits {
				commits[index] = map[string]any{"commit": map[string]any{"message": "commit"}}
			}
			json.NewEncoder(w).Encode(commits)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	listing, err := NewClient(server.URL, "token", server.Client()).FetchCommitListingContext(ctx, "example/repo", 7)
	if !errors.Is(err, context.DeadlineExceeded) || listing.Count != 0 || len(listing.Messages) != 0 || listing.GitHubCapReached {
		t.Fatalf("listing=%+v err=%v", listing, err)
	}
}

func TestClientDeadlineCancelsRESTRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client := NewClient(server.URL, "token", server.Client())
	_, err := client.ListOpenIssuesContext(ctx, "example/repo")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%T %v", err, err)
	}
	<-started
}

func TestClientCancellationCancelsGraphQLRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	client := NewClient(server.URL, "token", server.Client())
	_, err := client.GetReviewThreadsContext(ctx, "example/repo", 7)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%T %v", err, err)
	}
}

func TestGraphQLEndpointUsesGitHubEnterpriseAPIPath(t *testing.T) {
	tests := map[string]string{
		"https://api.github.com":        "https://api.github.com/graphql",
		"https://github.example/api/v3": "https://github.example/api/graphql",
	}
	for apiURL, want := range tests {
		if got := graphQLEndpoint(apiURL); got != want {
			t.Fatalf("graphQLEndpoint(%q) = %q, want %q", apiURL, got, want)
		}
	}
}
