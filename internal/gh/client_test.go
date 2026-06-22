package gh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sejunpark/baton/internal/config"
	"github.com/sejunpark/baton/internal/policy"
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
