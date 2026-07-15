package gh

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	newestIssueCommentLimit = 100
	retryCommentPageLimit   = 10
	pullRequestListingLimit = 100
	pullRequestPageLimit    = 10
)

type issueCommentPayload struct {
	ID        int64     `json:"id"`
	NodeID    string    `json:"node_id"`
	IssueURL  string    `json:"issue_url"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

func (payload issueCommentPayload) issueComment() IssueComment {
	return IssueComment{
		ID:        payload.ID,
		NodeID:    payload.NodeID,
		IssueURL:  payload.IssueURL,
		Body:      payload.Body,
		Author:    Actor{Login: payload.User.Login, Type: payload.User.Type},
		CreatedAt: payload.CreatedAt,
		UpdatedAt: payload.UpdatedAt,
	}
}

func (c *Client) GetRepositoryIdentity(repo string) (RepositoryIdentity, error) {
	return c.GetRepositoryIdentityContext(context.Background(), repo)
}

func (c *Client) GetRepositoryIdentityContext(ctx context.Context, repo string) (RepositoryIdentity, error) {
	var payload struct {
		NodeID   string `json:"node_id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s", repo), &payload); err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{Host: repositoryHost(c.baseURL, payload.HTMLURL), FullName: payload.FullName, NodeID: payload.NodeID}, nil
}

func (c *Client) GetRepositorySettingsContext(ctx context.Context, repo string) (RepositorySettings, error) {
	var payload struct {
		AllowMergeCommit bool `json:"allow_merge_commit"`
		AllowSquashMerge bool `json:"allow_squash_merge"`
		AllowRebaseMerge bool `json:"allow_rebase_merge"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s", repo), &payload); err != nil {
		return RepositorySettings{}, err
	}
	return RepositorySettings{AllowMergeCommit: payload.AllowMergeCommit, AllowSquashMerge: payload.AllowSquashMerge, AllowRebaseMerge: payload.AllowRebaseMerge}, nil
}

func repositoryHost(apiBaseURL, htmlURL string) string {
	if parsed, err := url.Parse(htmlURL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	parsed, err := url.Parse(apiBaseURL)
	if err != nil {
		return ""
	}
	if strings.EqualFold(parsed.Hostname(), "api.github.com") {
		return "github.com"
	}
	return parsed.Hostname()
}

func (c *Client) GetIssueComment(repo string, commentID int64) (IssueComment, error) {
	return c.GetIssueCommentContext(context.Background(), repo, commentID)
}

func (c *Client) GetIssueCommentContext(ctx context.Context, repo string, commentID int64) (IssueComment, error) {
	var payload issueCommentPayload
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, commentID), &payload); err != nil {
		return IssueComment{}, err
	}
	return payload.issueComment(), nil
}

// CreateIssueCommentReturning adds response acquisition without changing the
// error-only CreateIssueComment contract used by existing workflow interfaces.
func (c *Client) CreateIssueCommentReturning(repo string, issueNumber int, body string) (IssueComment, error) {
	return c.CreateIssueCommentReturningContext(context.Background(), repo, issueNumber, body)
}

func (c *Client) CreateIssueCommentReturningContext(ctx context.Context, repo string, issueNumber int, body string) (IssueComment, error) {
	var payload issueCommentPayload
	if err := c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, issueNumber), map[string]any{"body": body}, &payload); err != nil {
		return IssueComment{}, err
	}
	return payload.issueComment(), nil
}

// UpdateIssueCommentReturning adds response acquisition without changing the
// error-only UpdateIssueComment contract used by existing workflow interfaces.
func (c *Client) UpdateIssueCommentReturning(repo string, commentID int64, body string) (IssueComment, error) {
	return c.UpdateIssueCommentReturningContext(context.Background(), repo, commentID, body)
}

func (c *Client) UpdateIssueCommentReturningContext(ctx context.Context, repo string, commentID int64, body string) (IssueComment, error) {
	var payload issueCommentPayload
	if err := c.patchJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, commentID), map[string]any{"body": body}, &payload); err != nil {
		return IssueComment{}, err
	}
	return payload.issueComment(), nil
}

func (c *Client) ListNewestIssueComments(repo string, issueNumber int) (IssueCommentListing, error) {
	return c.ListNewestIssueCommentsContext(context.Background(), repo, issueNumber)
}

// ListNewestIssueCommentsContext reads exactly one newest-first bounded window.
// Complete is false when GitHub reports comments before that window.
func (c *Client) ListNewestIssueCommentsContext(ctx context.Context, repo string, issueNumber int) (IssueCommentListing, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return IssueCommentListing{}, err
	}
	var result issueCommentsGraphQLResponse
	headers, err := c.postGraphQLContext(ctx, graphQLPayload{
		Query: `query($owner: String!, $name: String!, $number: Int!, $limit: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      comments(last: $limit) {
        nodes {
          databaseId
          id
          body
          createdAt
          updatedAt
          author { login __typename }
        }
        pageInfo { hasPreviousPage }
      }
    }
  }
}`,
		Variables: map[string]any{"owner": owner, "name": name, "number": issueNumber, "limit": newestIssueCommentLimit},
	}, &result)
	if err != nil {
		return IssueCommentListing{}, err
	}
	if len(result.Errors) > 0 {
		return IssueCommentListing{}, graphQLResponseError(result.Errors, headers)
	}
	if result.Data.Repository == nil || result.Data.Repository.Issue == nil {
		return IssueCommentListing{}, fmt.Errorf("repository issue %s#%d was not found", repo, issueNumber)
	}
	connection := result.Data.Repository.Issue.Comments
	comments := make([]IssueComment, 0, len(connection.Nodes))
	for _, node := range connection.Nodes {
		comments = append(comments, IssueComment{
			ID: node.DatabaseID, NodeID: node.NodeID, Body: node.Body,
			Author:    Actor{Login: node.Author.Login, Type: node.Author.TypeName},
			CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
		})
	}
	return IssueCommentListing{Comments: comments, Complete: !connection.PageInfo.HasPreviousPage}, nil
}

// ListNewestPullRequestCommentsContext reads the bounded append-only policy
// evidence window on one pull request.
func (c *Client) ListNewestPullRequestCommentsContext(ctx context.Context, repo string, pullRequestNumber int) (IssueCommentListing, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return IssueCommentListing{}, err
	}
	var result issueCommentsGraphQLResponse
	headers, err := c.postGraphQLContext(ctx, graphQLPayload{
		Query: `query($owner: String!, $name: String!, $number: Int!, $limit: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      comments(last: $limit) {
        nodes {
          databaseId
          id
          body
          createdAt
          updatedAt
          author { login __typename }
        }
        pageInfo { hasPreviousPage }
      }
    }
  }
}`,
		Variables: map[string]any{"owner": owner, "name": name, "number": pullRequestNumber, "limit": newestIssueCommentLimit},
	}, &result)
	if err != nil {
		return IssueCommentListing{}, err
	}
	if len(result.Errors) > 0 {
		return IssueCommentListing{}, graphQLResponseError(result.Errors, headers)
	}
	if result.Data.Repository == nil || result.Data.Repository.PullRequest == nil {
		return IssueCommentListing{}, fmt.Errorf("repository pull request %s#%d was not found", repo, pullRequestNumber)
	}
	connection := result.Data.Repository.PullRequest.Comments
	comments := make([]IssueComment, 0, len(connection.Nodes))
	for _, node := range connection.Nodes {
		comments = append(comments, IssueComment{
			ID: node.DatabaseID, NodeID: node.NodeID, Body: node.Body,
			Author: Actor{Login: node.Author.Login, Type: node.Author.TypeName}, CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
		})
	}
	return IssueCommentListing{Comments: comments, Complete: !connection.PageInfo.HasPreviousPage}, nil
}

// ListIssueCommentsAfterContext walks newest comments backwards only until it
// crosses the last committed checkpoint update. This proves whether an
// uncommitted retry exists without treating older ledger history as relevant.
func (c *Client) ListIssueCommentsAfterContext(ctx context.Context, repo string, issueNumber int, after time.Time) (IssueCommentListing, error) {
	if after.IsZero() {
		return IssueCommentListing{}, fmt.Errorf("retry comment boundary is missing")
	}
	owner, name, err := splitRepo(repo)
	if err != nil {
		return IssueCommentListing{}, err
	}
	comments := []IssueComment{}
	var before any
	for page := 0; page < retryCommentPageLimit; page++ {
		var result issueCommentsGraphQLResponse
		headers, err := c.postGraphQLContext(ctx, graphQLPayload{
			Query: `query($owner: String!, $name: String!, $number: Int!, $limit: Int!, $before: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      comments(last: $limit, before: $before) {
        nodes {
          databaseId
          id
          body
          createdAt
          updatedAt
          author { login __typename }
        }
        pageInfo { hasPreviousPage startCursor }
      }
    }
  }
}`,
			Variables: map[string]any{"owner": owner, "name": name, "number": issueNumber, "limit": newestIssueCommentLimit, "before": before},
		}, &result)
		if err != nil {
			return IssueCommentListing{}, err
		}
		if len(result.Errors) > 0 {
			return IssueCommentListing{}, graphQLResponseError(result.Errors, headers)
		}
		if result.Data.Repository == nil || result.Data.Repository.Issue == nil {
			return IssueCommentListing{}, fmt.Errorf("repository issue %s#%d was not found", repo, issueNumber)
		}
		connection := result.Data.Repository.Issue.Comments
		reachedBoundary := false
		for _, node := range connection.Nodes {
			if node.CreatedAt.Before(after) {
				reachedBoundary = true
				continue
			}
			comments = append(comments, IssueComment{
				ID: node.DatabaseID, NodeID: node.NodeID, Body: node.Body,
				Author: Actor{Login: node.Author.Login, Type: node.Author.TypeName}, CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
			})
		}
		if reachedBoundary || !connection.PageInfo.HasPreviousPage {
			return IssueCommentListing{Comments: comments, Complete: true}, nil
		}
		if strings.TrimSpace(connection.PageInfo.StartCursor) == "" {
			return IssueCommentListing{Comments: comments, Complete: false}, nil
		}
		before = connection.PageInfo.StartCursor
	}
	return IssueCommentListing{Comments: comments, Complete: false}, nil
}

func (c *Client) ListOpenPullRequestsBounded(repo, base string) (PullRequestListing, error) {
	return c.ListOpenPullRequestsBoundedContext(context.Background(), repo, base)
}

func (c *Client) ListOpenPullRequestsBoundedContext(ctx context.Context, repo, base string) (PullRequestListing, error) {
	return c.listPullRequestsBoundedContext(ctx, repo, base, "open")
}

func (c *Client) ListClosedPullRequestsBounded(repo, base string) (PullRequestListing, error) {
	return c.ListClosedPullRequestsBoundedContext(context.Background(), repo, base)
}

func (c *Client) ListClosedPullRequestsBoundedContext(ctx context.Context, repo, base string) (PullRequestListing, error) {
	return c.listPullRequestsBoundedContext(ctx, repo, base, "closed")
}

// ListClosedPullRequestsUpdatedSinceContext walks closed pull requests in
// descending update order until it crosses the checkpoint observation time.
// Equal timestamps are retained so second-resolution boundaries cannot hide a
// merge observed concurrently with the checkpoint update.
func (c *Client) ListClosedPullRequestsUpdatedSinceContext(ctx context.Context, repo, base string, after time.Time) (PullRequestListing, error) {
	if after.IsZero() {
		return PullRequestListing{}, fmt.Errorf("closed pull request boundary is missing")
	}
	// GitHub's REST pull-request timestamps are second-resolution. Floor an
	// operator-supplied fractional boundary so the shared second remains in the
	// reconciliation window.
	after = after.Truncate(time.Second)
	pullRequests := []PullRequest{}
	var previous time.Time
	var firstPage []pullRequestPayload
	for page := 1; page <= pullRequestPageLimit; page++ {
		batch, err := c.listClosedPullRequestsUpdatedPageContext(ctx, repo, base, page)
		if err != nil {
			return PullRequestListing{}, err
		}
		if page == 1 {
			firstPage = append([]pullRequestPayload(nil), batch...)
		}
		reachedBoundary := false
		for _, payload := range batch {
			if payload.UpdatedAt.IsZero() {
				return PullRequestListing{PullRequests: pullRequests, Complete: false}, nil
			}
			if !previous.IsZero() && payload.UpdatedAt.After(previous) {
				return PullRequestListing{PullRequests: pullRequests, Complete: false}, nil
			}
			previous = payload.UpdatedAt
			if payload.UpdatedAt.Before(after) {
				reachedBoundary = true
				continue
			}
			pullRequests = append(pullRequests, pullRequestFromPayload(payload))
		}
		if reachedBoundary || len(batch) < pullRequestListingLimit {
			stable, err := c.listClosedPullRequestsUpdatedPageContext(ctx, repo, base, 1)
			if err != nil {
				return PullRequestListing{}, err
			}
			if !samePullRequestUpdatePage(firstPage, stable) {
				return PullRequestListing{PullRequests: pullRequests, Complete: false}, nil
			}
			return PullRequestListing{PullRequests: pullRequests, Complete: true}, nil
		}
	}
	return PullRequestListing{PullRequests: pullRequests, Complete: false}, nil
}

func (c *Client) listClosedPullRequestsUpdatedPageContext(ctx context.Context, repo, base string, page int) ([]pullRequestPayload, error) {
	query := url.Values{
		"state": {"closed"}, "sort": {"updated"}, "direction": {"desc"},
		"per_page": {fmt.Sprint(pullRequestListingLimit)}, "page": {fmt.Sprint(page)},
	}
	if base != "" {
		query.Set("base", base)
	}
	var batch []pullRequestPayload
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/pulls?%s", repo, query.Encode()), &batch); err != nil {
		return nil, err
	}
	return batch, nil
}

func samePullRequestUpdatePage(left, right []pullRequestPayload) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Number != right[index].Number || left[index].NodeID != right[index].NodeID || !left[index].UpdatedAt.Equal(right[index].UpdatedAt) {
			return false
		}
	}
	return true
}

func (c *Client) listPullRequestsBoundedContext(ctx context.Context, repo, base, state string) (PullRequestListing, error) {
	query := url.Values{"state": {state}, "per_page": {fmt.Sprint(pullRequestListingLimit)}, "page": {"1"}}
	if base != "" {
		query.Set("base", base)
	}
	var batch []pullRequestPayload
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/pulls?%s", repo, query.Encode()), &batch); err != nil {
		return PullRequestListing{}, err
	}
	pullRequests := make([]PullRequest, 0, len(batch))
	for _, payload := range batch {
		pullRequests = append(pullRequests, pullRequestFromPayload(payload))
	}
	return PullRequestListing{PullRequests: pullRequests, Complete: len(batch) < pullRequestListingLimit}, nil
}

func (c *Client) CompareCommits(repo, base, head string) (CommitComparison, error) {
	return c.CompareCommitsContext(context.Background(), repo, base, head)
}

func (c *Client) CompareCommitsContext(ctx context.Context, repo, base, head string) (CommitComparison, error) {
	var payload struct {
		Status       string `json:"status"`
		AheadBy      int    `json:"ahead_by"`
		BehindBy     int    `json:"behind_by"`
		TotalCommits int    `json:"total_commits"`
		MergeBase    struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	path := fmt.Sprintf("/repos/%s/compare/%s...%s", repo, url.PathEscape(base), url.PathEscape(head))
	if err := c.getJSONContext(ctx, path, &payload); err != nil {
		return CommitComparison{}, err
	}
	return CommitComparison{Status: payload.Status, AheadBy: payload.AheadBy, BehindBy: payload.BehindBy, TotalCommits: payload.TotalCommits, MergeBaseSHA: payload.MergeBase.SHA}, nil
}

func (c *Client) RerequestCheckRun(repo string, checkRunID int64) error {
	return c.RerequestCheckRunContext(context.Background(), repo, checkRunID)
}

func (c *Client) RerequestCheckRunContext(ctx context.Context, repo string, checkRunID int64) error {
	return c.requestNoBodyContext(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/check-runs/%d/rerequest", repo, checkRunID), nil, false)
}

type issueCommentsGraphQLResponse struct {
	Data struct {
		Repository *struct {
			Issue *struct {
				Comments issueCommentConnection `json:"comments"`
			} `json:"issue"`
			PullRequest *struct {
				Comments issueCommentConnection `json:"comments"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type issueCommentConnection struct {
	Nodes    []issueCommentNode `json:"nodes"`
	PageInfo struct {
		HasPreviousPage bool   `json:"hasPreviousPage"`
		StartCursor     string `json:"startCursor"`
	} `json:"pageInfo"`
}

type issueCommentNode struct {
	DatabaseID int64     `json:"databaseId"`
	NodeID     string    `json:"id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Author     struct {
		Login    string `json:"login"`
		TypeName string `json:"__typename"`
	} `json:"author"`
}
