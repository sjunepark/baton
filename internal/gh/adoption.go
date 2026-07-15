package gh

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strings"
)

type RepositoryDetails struct {
	Identity      RepositoryIdentity `json:"identity"`
	DefaultBranch string             `json:"defaultBranch"`
	OwnerType     string             `json:"ownerType"`
	Visibility    string             `json:"visibility"`
	Settings      RepositorySettings `json:"settings"`
}

type ActionsPermissions struct {
	Enabled             bool   `json:"enabled"`
	EnabledRepositories string `json:"enabledRepositories,omitempty"`
	AllowedActions      string `json:"allowedActions"`
	SHAPinningRequired  bool   `json:"shaPinningRequired"`
}

type SelectedActions struct {
	GitHubOwnedAllowed bool     `json:"githubOwnedAllowed"`
	VerifiedAllowed    bool     `json:"verifiedAllowed"`
	PatternsAllowed    []string `json:"patternsAllowed"`
}

type Workflow struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type RepositoryFile struct {
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Content []byte `json:"content"`
}

type App struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
}

func (c *Client) GetRepositoryDetailsContext(ctx context.Context, repo string) (RepositoryDetails, error) {
	var payload struct {
		NodeID           string `json:"node_id"`
		FullName         string `json:"full_name"`
		HTMLURL          string `json:"html_url"`
		DefaultBranch    string `json:"default_branch"`
		Visibility       string `json:"visibility"`
		AllowMergeCommit bool   `json:"allow_merge_commit"`
		AllowSquashMerge bool   `json:"allow_squash_merge"`
		AllowRebaseMerge bool   `json:"allow_rebase_merge"`
		Owner            struct {
			Type string `json:"type"`
		} `json:"owner"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s", repo), &payload); err != nil {
		return RepositoryDetails{}, err
	}
	return RepositoryDetails{
		Identity:      RepositoryIdentity{Host: repositoryHost(c.baseURL, payload.HTMLURL), FullName: payload.FullName, NodeID: payload.NodeID},
		DefaultBranch: payload.DefaultBranch,
		OwnerType:     payload.Owner.Type,
		Visibility:    payload.Visibility,
		Settings: RepositorySettings{
			AllowMergeCommit: payload.AllowMergeCommit,
			AllowSquashMerge: payload.AllowSquashMerge,
			AllowRebaseMerge: payload.AllowRebaseMerge,
		},
	}, nil
}

func (c *Client) GetAppContext(ctx context.Context, slug string) (App, error) {
	var payload struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := c.getJSONContext(ctx, "/apps/"+url.PathEscape(slug), &payload); err != nil {
		return App{}, err
	}
	return App{ID: payload.ID, Slug: payload.Slug}, nil
}

func (c *Client) GetRepositoryActionsPermissionsContext(ctx context.Context, repo string) (ActionsPermissions, error) {
	var payload struct {
		Enabled            bool   `json:"enabled"`
		AllowedActions     string `json:"allowed_actions"`
		SHAPinningRequired bool   `json:"sha_pinning_required"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/actions/permissions", repo), &payload); err != nil {
		return ActionsPermissions{}, err
	}
	return ActionsPermissions{Enabled: payload.Enabled, AllowedActions: payload.AllowedActions, SHAPinningRequired: payload.SHAPinningRequired}, nil
}

func (c *Client) GetOrganizationActionsPermissionsContext(ctx context.Context, organization string) (ActionsPermissions, error) {
	var payload struct {
		EnabledRepositories string `json:"enabled_repositories"`
		AllowedActions      string `json:"allowed_actions"`
		SHAPinningRequired  bool   `json:"sha_pinning_required"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/orgs/%s/actions/permissions", url.PathEscape(organization)), &payload); err != nil {
		return ActionsPermissions{}, err
	}
	return ActionsPermissions{Enabled: payload.EnabledRepositories != "none", EnabledRepositories: payload.EnabledRepositories, AllowedActions: payload.AllowedActions, SHAPinningRequired: payload.SHAPinningRequired}, nil
}

func (c *Client) GetRepositorySelectedActionsContext(ctx context.Context, repo string) (SelectedActions, error) {
	return c.getSelectedActionsContext(ctx, fmt.Sprintf("/repos/%s/actions/permissions/selected-actions", repo))
}

func (c *Client) GetOrganizationSelectedActionsContext(ctx context.Context, organization string) (SelectedActions, error) {
	return c.getSelectedActionsContext(ctx, fmt.Sprintf("/orgs/%s/actions/permissions/selected-actions", url.PathEscape(organization)))
}

func (c *Client) getSelectedActionsContext(ctx context.Context, endpoint string) (SelectedActions, error) {
	var payload struct {
		GitHubOwnedAllowed bool     `json:"github_owned_allowed"`
		VerifiedAllowed    bool     `json:"verified_allowed"`
		PatternsAllowed    []string `json:"patterns_allowed"`
	}
	if err := c.getJSONContext(ctx, endpoint, &payload); err != nil {
		return SelectedActions{}, err
	}
	return SelectedActions{GitHubOwnedAllowed: payload.GitHubOwnedAllowed, VerifiedAllowed: payload.VerifiedAllowed, PatternsAllowed: append([]string(nil), payload.PatternsAllowed...)}, nil
}

func (c *Client) GetWorkflowContext(ctx context.Context, repo, workflowPath string) (Workflow, error) {
	var payload struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	workflowID := path.Base(workflowPath)
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/actions/workflows/%s", repo, url.PathEscape(workflowID)), &payload); err != nil {
		return Workflow{}, err
	}
	return Workflow{Name: payload.Name, Path: payload.Path, State: payload.State}, nil
}

func (c *Client) GetRepositoryFileContext(ctx context.Context, repo, filePath, ref string) (RepositoryFile, error) {
	var payload struct {
		Path     string `json:"path"`
		SHA      string `json:"sha"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	endpoint := fmt.Sprintf("/repos/%s/contents/%s", repo, escapeRepositoryPath(filePath))
	if strings.TrimSpace(ref) != "" {
		endpoint += "?ref=" + url.QueryEscape(ref)
	}
	if err := c.getJSONContext(ctx, endpoint, &payload); err != nil {
		return RepositoryFile{}, err
	}
	if payload.Encoding != "base64" {
		return RepositoryFile{}, fmt.Errorf("repository content %s uses unsupported encoding %q", filePath, payload.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	if err != nil {
		return RepositoryFile{}, fmt.Errorf("decode repository content %s: %w", filePath, err)
	}
	return RepositoryFile{Path: payload.Path, SHA: payload.SHA, Content: content}, nil
}

func escapeRepositoryPath(value string) string {
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}
