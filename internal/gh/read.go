package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CheckRollup struct {
	SchemaVersion int          `json:"schemaVersion"`
	Kind          string       `json:"kind"`
	Repo          string       `json:"repo"`
	PRNumber      int          `json:"prNumber"`
	HeadSHA       string       `json:"headSha"`
	State         string       `json:"state"`
	Count         int          `json:"count"`
	Summary       CheckSummary `json:"summary"`
	Checks        []CheckState `json:"checks"`
	Complete      bool         `json:"complete"`
	Warnings      []string     `json:"warnings,omitempty"`
	Help          []string     `json:"help,omitempty"`
}

type CheckSummary struct {
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Skipped   int `json:"skipped"`
	Cancelled int `json:"cancelled"`
	Unknown   int `json:"unknown"`
}

type CheckState struct {
	ID         int64  `json:"id,omitempty"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	AppID      int64  `json:"appId,omitempty"`
}

type ReviewThreadResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Repo          string         `json:"repo"`
	PRNumber      int            `json:"prNumber"`
	Count         int            `json:"count"`
	Summary       ThreadSummary  `json:"summary"`
	Threads       []ReviewThread `json:"threads"`
	Complete      bool           `json:"complete"`
	Warnings      []string       `json:"warnings,omitempty"`
	Help          []string       `json:"help,omitempty"`
}

type ThreadSummary struct {
	Total             int `json:"total"`
	Unresolved        int `json:"unresolved"`
	HumanUnresolved   int `json:"humanUnresolved"`
	BotUnresolved     int `json:"botUnresolved"`
	UnknownUnresolved int `json:"unknownUnresolved,omitempty"`
	Outdated          int `json:"outdated"`
}

type ReviewThread struct {
	IsResolved bool            `json:"isResolved"`
	IsOutdated bool            `json:"isOutdated"`
	Path       string          `json:"path"`
	Line       int             `json:"line"`
	Comments   []ReviewComment `json:"comments"`
}

type ReviewComment struct {
	Author        string `json:"author"`
	AuthorType    string `json:"authorType,omitempty"`
	AuthorKind    string `json:"authorKind"`
	Body          string `json:"body"`
	BodyChars     int    `json:"bodyChars"`
	BodyTruncated bool   `json:"bodyTruncated"`
	BodyPreview   string `json:"bodyPreview,omitempty"`
	FullCommand   string `json:"fullCommand,omitempty"`
	URL           string `json:"url"`
}

func (c *Client) ListOpenIssues(repo string) ([]Issue, error) {
	return c.ListOpenIssuesContext(context.Background(), repo)
}

func (c *Client) GetIssueContext(ctx context.Context, repo string, number int) (Issue, error) {
	var resource struct {
		Number      int       `json:"number"`
		NodeID      string    `json:"node_id"`
		Title       string    `json:"title"`
		HTMLURL     string    `json:"html_url"`
		Body        string    `json:"body"`
		State       string    `json:"state"`
		Locked      bool      `json:"locked"`
		Comments    int       `json:"comments"`
		PullRequest *struct{} `json:"pull_request"`
		Labels      []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d", repo, number), &resource); err != nil {
		return Issue{}, err
	}
	labels := make([]string, 0, len(resource.Labels))
	for _, label := range resource.Labels {
		labels = append(labels, label.Name)
	}
	return Issue{Number: resource.Number, NodeID: resource.NodeID, Title: resource.Title, URL: resource.HTMLURL, Body: resource.Body, Labels: labels, State: resource.State, PullRequest: resource.PullRequest != nil, Locked: resource.Locked, CommentCount: resource.Comments}, nil
}

func (c *Client) ListOpenIssuesContext(ctx context.Context, repo string) ([]Issue, error) {
	out := []Issue{}
	for page := 1; ; page++ {
		var batch []struct {
			Number      int    `json:"number"`
			NodeID      string `json:"node_id"`
			Title       string `json:"title"`
			HTMLURL     string `json:"html_url"`
			Body        string `json:"body"`
			State       string `json:"state"`
			Locked      bool   `json:"locked"`
			Comments    int    `json:"comments"`
			PullRequest *struct {
				URL string `json:"url"`
			} `json:"pull_request"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		path := fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&page=%d", repo, page)
		if err := c.getJSONContext(ctx, path, &batch); err != nil {
			return nil, err
		}
		for _, issue := range batch {
			if issue.PullRequest != nil {
				continue
			}
			labels := make([]string, 0, len(issue.Labels))
			for _, label := range issue.Labels {
				labels = append(labels, label.Name)
			}
			out = append(out, Issue{
				Number:       issue.Number,
				NodeID:       issue.NodeID,
				Title:        issue.Title,
				URL:          issue.HTMLURL,
				Body:         issue.Body,
				Labels:       labels,
				State:        issue.State,
				Locked:       issue.Locked,
				CommentCount: issue.Comments,
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) ListOpenPullRequests(repo string, base string) ([]PullRequest, error) {
	return c.ListOpenPullRequestsContext(context.Background(), repo, base)
}

func (c *Client) ListOpenPullRequestsContext(ctx context.Context, repo string, base string) ([]PullRequest, error) {
	return c.listPullRequestsContext(ctx, repo, base, "open")
}

func (c *Client) listPullRequestsContext(ctx context.Context, repo, base, state string) ([]PullRequest, error) {
	out := []PullRequest{}
	for page := 1; ; page++ {
		query := url.Values{"state": {state}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if base != "" {
			query.Set("base", base)
		}
		var batch []pullRequestPayload
		if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/pulls?%s", repo, query.Encode()), &batch); err != nil {
			return nil, err
		}
		for _, pr := range batch {
			out = append(out, pullRequestFromPayload(pr))
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) GetPullRequest(repo string, number int) (PullRequest, error) {
	return c.GetPullRequestContext(context.Background(), repo, number)
}

func (c *Client) GetPullRequestContext(ctx context.Context, repo string, number int) (PullRequest, error) {
	var pr pullRequestPayload
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), &pr); err != nil {
		return PullRequest{}, err
	}
	return pullRequestFromPayload(pr), nil
}

type pullRequestPayload struct {
	Number        int       `json:"number"`
	NodeID        string    `json:"node_id"`
	Title         string    `json:"title"`
	HTMLURL       string    `json:"html_url"`
	Body          string    `json:"body"`
	Draft         bool      `json:"draft"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	MergedAt      time.Time `json:"merged_at"`
	MergeRevision string    `json:"merge_commit_sha"`
	Mergeable     *bool     `json:"mergeable"`
	MergeState    string    `json:"mergeable_state"`
	User          struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	Base struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

func pullRequestFromPayload(pr pullRequestPayload) PullRequest {
	mergeable := "unknown"
	if pr.Mergeable != nil {
		if *pr.Mergeable {
			mergeable = "mergeable"
		} else {
			mergeable = "conflicting"
		}
	}
	return PullRequest{
		Number: pr.Number, NodeID: pr.NodeID, Title: pr.Title, URL: pr.HTMLURL, Body: pr.Body,
		BaseRef: pr.Base.Ref, BaseSHA: pr.Base.SHA, HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA,
		BaseRepositoryFullName: repositoryFullName(pr.Base.Repo), HeadRepositoryFullName: repositoryFullName(pr.Head.Repo),
		Draft: pr.Draft, Author: Actor{Login: pr.User.Login, Type: pr.User.Type}, Mergeable: mergeable, MergeState: pr.MergeState,
		State: pr.State, Merged: !pr.MergedAt.IsZero(), CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt, MergedAt: pr.MergedAt, MergeRevision: pr.MergeRevision,
	}
}

func repositoryFullName(repository *struct {
	FullName string `json:"full_name"`
}) string {
	if repository == nil {
		return ""
	}
	return repository.FullName
}

func (c *Client) GetCheckRollup(repo string, prNumber int, headSHA string) (CheckRollup, error) {
	return c.GetCheckRollupContext(context.Background(), repo, prNumber, headSHA)
}

func (c *Client) GetCheckRollupContext(ctx context.Context, repo string, prNumber int, headSHA string) (CheckRollup, error) {
	checks := []CheckState{}
	if headSHA == "" {
		result := buildCheckRollup(repo, prNumber, "", checks)
		result.Complete = false
		result.Warnings = []string{"check acquisition requires a head revision"}
		return result, nil
	}
	checkRunsComplete := true
	checkRunTotal := 0
	for page := 1; ; page++ {
		var runs struct {
			TotalCount int `json:"total_count"`
			CheckRuns  []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
				App        struct {
					ID int64 `json:"id"`
				} `json:"app"`
			} `json:"check_runs"`
		}
		path := fmt.Sprintf("/repos/%s/commits/%s/check-runs?filter=latest&per_page=100&page=%d", repo, headSHA, page)
		if err := c.getJSONContext(ctx, path, &runs); err != nil {
			return CheckRollup{}, err
		}
		if runs.TotalCount > checkRunTotal {
			checkRunTotal = runs.TotalCount
		}
		for _, run := range runs.CheckRuns {
			checks = append(checks, CheckState{ID: run.ID, Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: run.HTMLURL, AppID: run.App.ID})
		}
		if len(checks) >= 1000 {
			checkRunsComplete = checkRunTotal > 0 && checkRunTotal <= len(checks)
			break
		}
		if len(runs.CheckRuns) < 100 {
			break
		}
	}
	seenStatuses := map[string]struct{}{}
	for page := 1; ; page++ {
		var statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			TargetURL   string `json:"target_url"`
			Description string `json:"description"`
		}
		path := fmt.Sprintf("/repos/%s/commits/%s/statuses?per_page=100&page=%d", repo, headSHA, page)
		if err := c.getJSONContext(ctx, path, &statuses); err != nil {
			return CheckRollup{}, err
		}
		for _, status := range statuses {
			if _, exists := seenStatuses[status.Context]; exists {
				continue
			}
			seenStatuses[status.Context] = struct{}{}
			checks = append(checks, CheckState{Name: status.Context, Status: status.State, Conclusion: status.State, URL: status.TargetURL})
		}
		if len(statuses) < 100 {
			break
		}
	}
	result := buildCheckRollup(repo, prNumber, headSHA, checks)
	result.Complete = checkRunsComplete
	if !checkRunsComplete {
		result.Warnings = []string{"GitHub limited check-run acquisition to the most recent 1000 check suites"}
	}
	return result, nil
}

func (c *Client) GetBranchHealth(repo string, ref string) (*BranchHealth, error) {
	return c.GetBranchHealthContext(context.Background(), repo, ref)
}

func (c *Client) GetBranchHealthContext(ctx context.Context, repo string, ref string) (*BranchHealth, error) {
	if ref == "" {
		return nil, nil
	}
	var payload struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/git/ref/heads/%s", repo, url.PathEscape(ref)), &payload); err != nil {
		return nil, err
	}
	rollup, err := c.GetCheckRollupContext(ctx, repo, 0, payload.Object.SHA)
	if err != nil {
		return nil, err
	}
	return &BranchHealth{Ref: ref, SHA: payload.Object.SHA, CheckState: rollup.State}, nil
}

func (c *Client) GetReviewThreads(repo string, prNumber int) (ReviewThreadResult, error) {
	return c.GetReviewThreadsContext(context.Background(), repo, prNumber)
}

func (c *Client) GetReviewThreadsContext(ctx context.Context, repo string, prNumber int) (ReviewThreadResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return ReviewThreadResult{}, err
	}
	threads := []ReviewThread{}
	var threadsCursor *string
	for {
		var result reviewThreadsGraphQLResponse
		headers, err := c.postGraphQLContext(ctx, graphQLPayload{
			Query: `query($owner: String!, $name: String!, $number: Int!, $threadsCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $threadsCursor) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          comments(first: 100) {
            nodes {
              body
              url
              author { login __typename }
            }
            pageInfo {
              hasNextPage
              endCursor
            }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`,
			Variables: map[string]any{"owner": owner, "name": name, "number": prNumber, "threadsCursor": threadsCursor},
		}, &result)
		if err != nil {
			return ReviewThreadResult{}, err
		}
		if len(result.Errors) > 0 {
			return ReviewThreadResult{}, graphQLResponseError(result.Errors, headers)
		}
		connection := result.Data.Repository.PullRequest.ReviewThreads
		for _, node := range connection.Nodes {
			comments := reviewCommentsFromNodes(node.Comments.Nodes)
			if node.Comments.PageInfo.HasNextPage {
				more, err := c.getRemainingReviewThreadCommentsContext(ctx, node.ID, node.Comments.PageInfo.EndCursor)
				if err != nil {
					return ReviewThreadResult{}, err
				}
				comments = append(comments, more...)
			}
			threads = append(threads, ReviewThread{IsResolved: node.IsResolved, IsOutdated: node.IsOutdated, Path: node.Path, Line: node.Line, Comments: comments})
		}
		if !connection.PageInfo.HasNextPage {
			break
		}
		threadsCursor = &connection.PageInfo.EndCursor
	}
	result := buildReviewThreadResult(repo, prNumber, threads)
	result.Complete = true
	return result, nil
}

func (c *Client) getRemainingReviewThreadCommentsContext(ctx context.Context, threadID, cursor string) ([]ReviewComment, error) {
	comments := []ReviewComment{}
	commentsCursor := &cursor
	for {
		var result reviewThreadCommentsGraphQLResponse
		headers, err := c.postGraphQLContext(ctx, graphQLPayload{
			Query: `query($id: ID!, $commentsCursor: String) {
  node(id: $id) {
    ... on PullRequestReviewThread {
      comments(first: 100, after: $commentsCursor) {
        nodes {
          body
          url
          author { login __typename }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`,
			Variables: map[string]any{"id": threadID, "commentsCursor": commentsCursor},
		}, &result)
		if err != nil {
			return nil, err
		}
		if len(result.Errors) > 0 {
			return nil, graphQLResponseError(result.Errors, headers)
		}
		connection := result.Data.Node.Comments
		comments = append(comments, reviewCommentsFromNodes(connection.Nodes)...)
		if !connection.PageInfo.HasNextPage {
			break
		}
		commentsCursor = &connection.PageInfo.EndCursor
	}
	return comments, nil
}

type graphQLPayload struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (c *Client) postGraphQLContext(ctx context.Context, payload graphQLPayload, out any) (http.Header, error) {
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphQLEndpoint(c.baseURL), bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, APIError{Method: http.MethodPost, Path: "/graphql", Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := APIError{
			Method: http.MethodPost, Path: "/graphql", Status: resp.Status, StatusCode: resp.StatusCode,
			RequestID: resp.Header.Get("X-GitHub-Request-Id"),
		}
		applyRateLimitMetadata(&apiErr, resp.Header, time.Now())
		return resp.Header.Clone(), apiErr
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.Header.Clone(), APIError{Method: http.MethodPost, Path: "/graphql", Status: resp.Status, StatusCode: resp.StatusCode, RequestID: resp.Header.Get("X-GitHub-Request-Id"), Cause: err}
	}
	return resp.Header.Clone(), nil
}

func graphQLEndpoint(apiBaseURL string) string {
	apiBaseURL = strings.TrimRight(apiBaseURL, "/")
	if strings.HasSuffix(apiBaseURL, "/api/v3") {
		return strings.TrimSuffix(apiBaseURL, "/api/v3") + "/api/graphql"
	}
	return apiBaseURL + "/graphql"
}

type reviewThreadsGraphQLResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads reviewThreadConnection `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type reviewThreadCommentsGraphQLResponse struct {
	Data struct {
		Node struct {
			Comments reviewCommentConnection `json:"comments"`
		} `json:"node"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message    string `json:"message"`
	Type       string `json:"type"`
	Extensions struct {
		Type string `json:"type"`
	} `json:"extensions"`
}

func graphQLResponseError(graphQLErrors []graphQLError, headers http.Header) APIError {
	result := APIError{Method: http.MethodPost, Path: "/graphql", Status: "GraphQL error", StatusCode: http.StatusOK}
	for _, graphQLErr := range graphQLErrors {
		if strings.EqualFold(graphQLErr.Type, "RATE_LIMITED") || strings.EqualFold(graphQLErr.Extensions.Type, "RATE_LIMITED") {
			result.RateLimited = true
			break
		}
	}
	result.RequestID = headers.Get("X-GitHub-Request-Id")
	applyRateLimitMetadata(&result, headers, time.Now())
	return result
}

type reviewThreadConnection struct {
	Nodes    []reviewThreadNode `json:"nodes"`
	PageInfo pageInfo           `json:"pageInfo"`
}

type reviewThreadNode struct {
	ID         string                  `json:"id"`
	IsResolved bool                    `json:"isResolved"`
	IsOutdated bool                    `json:"isOutdated"`
	Path       string                  `json:"path"`
	Line       int                     `json:"line"`
	Comments   reviewCommentConnection `json:"comments"`
}

type reviewCommentConnection struct {
	Nodes    []reviewCommentNode `json:"nodes"`
	PageInfo pageInfo            `json:"pageInfo"`
}

type reviewCommentNode struct {
	Body   string `json:"body"`
	URL    string `json:"url"`
	Author struct {
		Login    string `json:"login"`
		TypeName string `json:"__typename"`
	} `json:"author"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func reviewCommentsFromNodes(nodes []reviewCommentNode) []ReviewComment {
	comments := make([]ReviewComment, 0, len(nodes))
	for _, comment := range nodes {
		comments = append(comments, ReviewComment{
			Author: comment.Author.Login, AuthorType: comment.Author.TypeName,
			AuthorKind: classifyAuthor(comment.Author.Login, comment.Author.TypeName),
			Body:       comment.Body, URL: comment.URL,
		})
	}
	return comments
}

func buildCheckRollup(repo string, prNumber int, headSHA string, checks []CheckState) CheckRollup {
	summary := CheckSummary{}
	for _, check := range checks {
		switch checkBucket(check) {
		case "passed":
			summary.Passed++
		case "failed":
			summary.Failed++
		case "pending":
			summary.Pending++
		case "skipped":
			summary.Skipped++
		case "cancelled":
			summary.Cancelled++
		default:
			summary.Unknown++
		}
	}
	return CheckRollup{
		SchemaVersion: 1,
		Kind:          "checkRollup",
		Repo:          repo,
		PRNumber:      prNumber,
		HeadSHA:       headSHA,
		State:         classifyChecks(checks),
		Count:         len(checks),
		Summary:       summary,
		Checks:        checks,
		Help:          checkHelp(prNumber, summary),
	}
}

func checkBucket(check CheckState) string {
	switch {
	case check.Conclusion == "success" || check.Status == "success":
		return "passed"
	case check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "action_required" || check.Status == "failure" || check.Status == "error":
		return "failed"
	case check.Conclusion == "cancelled":
		return "cancelled"
	case check.Conclusion == "skipped":
		return "skipped"
	case check.Status == "queued" || check.Status == "in_progress" || check.Status == "pending" || check.Conclusion == "":
		return "pending"
	default:
		return "unknown"
	}
}

func checkHelp(prNumber int, summary CheckSummary) []string {
	help := []string{fmt.Sprintf("Run `baton pr %d --json` for PR context.", prNumber)}
	if summary.Failed > 0 {
		help = append(help, "Inspect failed check URLs before editing.")
	}
	if summary.Pending > 0 {
		help = append(help, "Wait for pending checks or rerun after they complete.")
	}
	return help
}

func buildReviewThreadResult(repo string, prNumber int, threads []ReviewThread) ReviewThreadResult {
	summary := ThreadSummary{Total: len(threads)}
	for _, thread := range threads {
		if thread.IsOutdated {
			summary.Outdated++
		}
		if thread.IsResolved {
			continue
		}
		summary.Unresolved++
		switch threadAuthorKind(thread) {
		case "human":
			summary.HumanUnresolved++
		case "bot":
			summary.BotUnresolved++
		default:
			summary.UnknownUnresolved++
		}
	}
	return ReviewThreadResult{
		SchemaVersion: 1,
		Kind:          "reviewThreads",
		Repo:          repo,
		PRNumber:      prNumber,
		Count:         len(threads),
		Summary:       summary,
		Threads:       threads,
		Help:          reviewThreadHelp(prNumber, summary),
	}
}

func threadAuthorKind(thread ReviewThread) string {
	sawBot := false
	for _, comment := range thread.Comments {
		if comment.AuthorKind == "human" {
			return "human"
		}
		switch comment.AuthorKind {
		case "bot", "codex", "coderabbit", "greptile":
			sawBot = true
		}
	}
	if sawBot {
		return "bot"
	}
	return "unknown"
}

func reviewThreadHelp(prNumber int, summary ThreadSummary) []string {
	help := []string{fmt.Sprintf("Run `baton pr %d --json` for PR context.", prNumber)}
	if summary.HumanUnresolved > 0 {
		help = append(help, "Stop and ask the user if unresolved human comments require product judgment.")
	}
	if summary.Unresolved > 0 {
		help = append(help, "Address unresolved review threads before completing the PR.")
	}
	return help
}

func classifyChecks(checks []CheckState) string {
	if len(checks) == 0 {
		return "unknown"
	}
	state := "success"
	for _, check := range checks {
		if check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "cancelled" || check.Status == "failure" || check.Status == "error" {
			return "failure"
		}
		if check.Status == "queued" || check.Status == "in_progress" || check.Status == "pending" || check.Conclusion == "" {
			state = "pending"
		}
	}
	return state
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo must be owner/name")
	}
	return parts[0], parts[1], nil
}

func classifyAuthor(login, actorType string) string {
	if login == "" {
		return "unknown"
	}
	if actorType != "Bot" {
		if actorType == "User" {
			return "human"
		}
		return "unknown"
	}
	switch strings.ToLower(login) {
	case "coderabbitai", "coderabbitai[bot]":
		return "coderabbit"
	case "greptile-app", "greptile-apps[bot]":
		return "greptile"
	case "codex-bot", "codex-connector[bot]", "chatgpt-codex-connector[bot]":
		return "codex"
	default:
		return "bot"
	}
}
