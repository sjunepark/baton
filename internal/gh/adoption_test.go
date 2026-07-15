package gh

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAdoptionFactsTransport(t *testing.T) {
	content := []byte("name: PR Policy\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps/github-actions":
			json.NewEncoder(w).Encode(map[string]any{"id": 15368, "slug": "github-actions"})
		case "/repos/example/repo":
			json.NewEncoder(w).Encode(map[string]any{
				"node_id": "R_1", "full_name": "example/repo", "html_url": "https://github.com/example/repo", "default_branch": "main",
				"visibility": "private", "allow_merge_commit": true, "allow_squash_merge": true, "allow_rebase_merge": false, "owner": map[string]any{"type": "Organization"},
			})
		case "/repos/example/repo/actions/permissions":
			json.NewEncoder(w).Encode(map[string]any{"enabled": true, "allowed_actions": "selected", "sha_pinning_required": false})
		case "/orgs/example/actions/permissions":
			json.NewEncoder(w).Encode(map[string]any{"enabled_repositories": "all", "allowed_actions": "selected", "sha_pinning_required": false})
		case "/repos/example/repo/actions/permissions/selected-actions", "/orgs/example/actions/permissions/selected-actions":
			json.NewEncoder(w).Encode(map[string]any{"github_owned_allowed": true, "verified_allowed": false, "patterns_allowed": []string{"example/*"}})
		case "/repos/example/repo/actions/workflows/pr-policy.yml":
			json.NewEncoder(w).Encode(map[string]any{"name": "PR Policy", "path": ".github/workflows/pr-policy.yml", "state": "active"})
		case "/repos/example/repo/contents/.github/workflows/pr-policy.yml":
			if r.URL.Query().Get("ref") != "default-sha" {
				t.Fatalf("ref = %q", r.URL.Query().Get("ref"))
			}
			json.NewEncoder(w).Encode(map[string]any{"path": ".github/workflows/pr-policy.yml", "sha": "file-sha", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)})
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	app, err := client.GetAppContext(context.Background(), "github-actions")
	if err != nil || app.ID != 15368 || app.Slug != "github-actions" {
		t.Fatalf("app = %+v, err = %v", app, err)
	}
	details, err := client.GetRepositoryDetailsContext(context.Background(), "example/repo")
	if err != nil || details.Identity.NodeID != "R_1" || details.DefaultBranch != "main" || details.OwnerType != "Organization" || details.Visibility != "private" || !details.Settings.AllowMergeCommit || details.Settings.AllowRebaseMerge {
		t.Fatalf("details = %+v, err = %v", details, err)
	}
	repositoryActions, err := client.GetRepositoryActionsPermissionsContext(context.Background(), "example/repo")
	if err != nil || !repositoryActions.Enabled || repositoryActions.AllowedActions != "selected" {
		t.Fatalf("repository actions = %+v, err = %v", repositoryActions, err)
	}
	organizationActions, err := client.GetOrganizationActionsPermissionsContext(context.Background(), "example")
	if err != nil || organizationActions.EnabledRepositories != "all" {
		t.Fatalf("organization actions = %+v, err = %v", organizationActions, err)
	}
	selected, err := client.GetRepositorySelectedActionsContext(context.Background(), "example/repo")
	if err != nil || !selected.GitHubOwnedAllowed || !reflect.DeepEqual(selected.PatternsAllowed, []string{"example/*"}) {
		t.Fatalf("selected actions = %+v, err = %v", selected, err)
	}
	organizationSelected, err := client.GetOrganizationSelectedActionsContext(context.Background(), "example")
	if err != nil || !organizationSelected.GitHubOwnedAllowed {
		t.Fatalf("organization selected actions = %+v, err = %v", organizationSelected, err)
	}
	workflow, err := client.GetWorkflowContext(context.Background(), "example/repo", ".github/workflows/pr-policy.yml")
	if err != nil || workflow.State != "active" {
		t.Fatalf("workflow = %+v, err = %v", workflow, err)
	}
	file, err := client.GetRepositoryFileContext(context.Background(), "example/repo", ".github/workflows/pr-policy.yml", "default-sha")
	if err != nil || !reflect.DeepEqual(file.Content, content) {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
}
