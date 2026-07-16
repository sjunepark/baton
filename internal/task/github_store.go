package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sjunepark/baton/internal/gh"
)

type GitHubStore struct {
	client *gh.Client
}

func NewGitHubStore(client *gh.Client) *GitHubStore {
	return &GitHubStore{client: client}
}

func (s *GitHubStore) ListIssues(ctx context.Context, repository string, state ListState) ([]Issue, error) {
	resources, err := s.client.ListIssuesByLabelContext(ctx, repository, string(state), LabelManaged)
	if err != nil {
		return nil, translateGitHubError("list repository issues", err)
	}
	issues := make([]Issue, 0, len(resources))
	for _, resource := range resources {
		issues = append(issues, issueFromGitHub(resource))
	}
	return issues, nil
}

func (s *GitHubStore) GetIssue(ctx context.Context, repository string, number int) (Issue, error) {
	resource, err := s.client.GetIssueContext(ctx, repository, number)
	if err != nil {
		return Issue{}, translateGitHubError(fmt.Sprintf("read issue #%d", number), err)
	}
	if resource.PullRequest {
		return Issue{}, &Error{
			Code:    "not_issue",
			Message: fmt.Sprintf("#%d is a pull request, not an issue", number),
			Hint:    "Choose a GitHub issue number.",
		}
	}
	return issueFromGitHub(resource), nil
}

func (s *GitHubStore) EnsureLabel(ctx context.Context, repository string, definition LabelDefinition) (bool, error) {
	exists, err := s.labelExists(ctx, repository, definition.Name)
	if err != nil || exists {
		return false, err
	}
	err = s.client.CreateLabelContext(ctx, repository, gh.Label{
		Name: definition.Name, Color: definition.Color, Description: definition.Description,
	})
	if err == nil {
		return true, nil
	}
	var apiErr gh.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		return false, translateGitHubError(fmt.Sprintf("create label %q", definition.Name), err)
	}
	// Another actor may have created the case-insensitive label after our read.
	// Confirm that outcome rather than treating the safe race as a failure.
	exists, confirmErr := s.labelExists(ctx, repository, definition.Name)
	if confirmErr != nil {
		return false, confirmErr
	}
	if exists {
		return false, nil
	}
	return false, translateGitHubError(fmt.Sprintf("create label %q", definition.Name), err)
}

func (s *GitHubStore) AddLabel(ctx context.Context, repository string, number int, label string) error {
	err := s.client.AddIssueLabelsContext(ctx, repository, number, []string{label})
	return translateGitHubError(fmt.Sprintf("add label %q to issue #%d", label, number), err)
}

func (s *GitHubStore) RemoveLabel(ctx context.Context, repository string, number int, label string) error {
	err := s.client.DeleteIssueLabelContext(ctx, repository, number, label)
	if !gh.IsNotFound(err) {
		return translateGitHubError(fmt.Sprintf("remove label %q from issue #%d", label, number), err)
	}
	resource, readErr := s.client.GetIssueContext(ctx, repository, number)
	if readErr != nil {
		return translateGitHubError(fmt.Sprintf("remove label %q from issue #%d", label, number), readErr)
	}
	for _, existing := range resource.Labels {
		if strings.EqualFold(existing, label) {
			return translateGitHubError(fmt.Sprintf("remove label %q from issue #%d", label, number), err)
		}
	}
	return nil
}

func (s *GitHubStore) CloseIssue(ctx context.Context, repository string, number int) error {
	err := s.client.CloseIssueContext(ctx, repository, number)
	return translateGitHubError(fmt.Sprintf("close issue #%d", number), err)
}

func (s *GitHubStore) labelExists(ctx context.Context, repository, name string) (bool, error) {
	_, exists, err := s.client.GetLabelContext(ctx, repository, name)
	if err != nil {
		return false, translateGitHubError(fmt.Sprintf("read label %q", name), err)
	}
	return exists, nil
}

func issueFromGitHub(issue gh.Issue) Issue {
	state := IssueState(issue.State)
	return Issue{
		Number: issue.Number, Title: issue.Title, URL: issue.URL, Body: issue.Body,
		State: state, Labels: append([]string(nil), issue.Labels...),
	}
}

func translateGitHubError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var apiErr gh.APIError
	if !errors.As(err, &apiErr) {
		return &Error{Code: "github_error", Message: operation + " failed", Hint: "Retry the command.", Cause: err}
	}
	code, hint := "github_error", "Retry the command."
	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		code, hint = "not_found", "Check the repository and issue number."
	case apiErr.RateLimited || apiErr.StatusCode == http.StatusTooManyRequests:
		code, hint = "rate_limited", "Retry after GitHub's rate limit resets."
	case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
		code, hint = "access_denied", "Check GitHub credentials and repository permissions."
	}
	return &Error{Code: code, Message: operation + " failed", Hint: hint, Cause: err}
}
