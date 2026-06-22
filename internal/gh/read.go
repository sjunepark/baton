package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/queue"
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
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

type ReviewThreadResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Repo          string         `json:"repo"`
	PRNumber      int            `json:"prNumber"`
	Count         int            `json:"count"`
	Summary       ThreadSummary  `json:"summary"`
	Threads       []ReviewThread `json:"threads"`
	Help          []string       `json:"help,omitempty"`
}

type ThreadSummary struct {
	Total           int `json:"total"`
	Unresolved      int `json:"unresolved"`
	HumanUnresolved int `json:"humanUnresolved"`
	BotUnresolved   int `json:"botUnresolved"`
	Outdated        int `json:"outdated"`
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
	AuthorKind    string `json:"authorKind"`
	Body          string `json:"body"`
	BodyChars     int    `json:"bodyChars"`
	BodyTruncated bool   `json:"bodyTruncated"`
	BodyPreview   string `json:"bodyPreview,omitempty"`
	FullCommand   string `json:"fullCommand,omitempty"`
	URL           string `json:"url"`
}

func (c *Client) ListOpenIssues(repo string) ([]queue.Issue, error) {
	out := []queue.Issue{}
	for page := 1; ; page++ {
		var batch []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			HTMLURL     string `json:"html_url"`
			Body        string `json:"body"`
			PullRequest *struct {
				URL string `json:"url"`
			} `json:"pull_request"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		path := fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&page=%d", repo, page)
		if err := c.getJSON(path, &batch); err != nil {
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
			out = append(out, queue.Issue{
				Number: issue.Number,
				Title:  issue.Title,
				URL:    issue.HTMLURL,
				Body:   issue.Body,
				Labels: labels,
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) ListOpenPullRequests(repo string, base string) ([]queue.PullRequest, error) {
	out := []queue.PullRequest{}
	for page := 1; ; page++ {
		query := url.Values{"state": {"open"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if base != "" {
			query.Set("base", base)
		}
		var batch []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			Body    string `json:"body"`
			Base    struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Head struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
		}
		if err := c.getJSON(fmt.Sprintf("/repos/%s/pulls?%s", repo, query.Encode()), &batch); err != nil {
			return nil, err
		}
		for _, pr := range batch {
			out = append(out, queue.PullRequest{
				Number:  pr.Number,
				Title:   pr.Title,
				URL:     pr.HTMLURL,
				Body:    pr.Body,
				BaseRef: pr.Base.Ref,
				HeadRef: pr.Head.Ref,
				HeadSHA: pr.Head.SHA,
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) GetPullRequest(repo string, number int) (queue.PullRequest, error) {
	var pr struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Base    struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.getJSON(fmt.Sprintf("/repos/%s/pulls/%d", repo, number), &pr); err != nil {
		return queue.PullRequest{}, err
	}
	return queue.PullRequest{
		Number:  pr.Number,
		Title:   pr.Title,
		URL:     pr.HTMLURL,
		Body:    pr.Body,
		BaseRef: pr.Base.Ref,
		HeadRef: pr.Head.Ref,
		HeadSHA: pr.Head.SHA,
	}, nil
}

func (c *Client) GetCheckRollup(repo string, pr queue.PullRequest) (CheckRollup, error) {
	checks := []CheckState{}
	if pr.HeadSHA == "" {
		return buildCheckRollup(repo, pr.Number, "", checks), nil
	}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	if err := c.getJSON(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100", repo, pr.HeadSHA), &runs); err != nil {
		return CheckRollup{}, err
	}
	for _, run := range runs.CheckRuns {
		checks = append(checks, CheckState{Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: run.HTMLURL})
	}
	var statuses struct {
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			TargetURL   string `json:"target_url"`
			Description string `json:"description"`
		} `json:"statuses"`
	}
	if err := c.getJSON(fmt.Sprintf("/repos/%s/commits/%s/status", repo, pr.HeadSHA), &statuses); err != nil {
		return CheckRollup{}, err
	}
	for _, status := range statuses.Statuses {
		checks = append(checks, CheckState{Name: status.Context, Status: status.State, Conclusion: status.State, URL: status.TargetURL})
	}
	return buildCheckRollup(repo, pr.Number, pr.HeadSHA, checks), nil
}

func (c *Client) GetBranchHealth(repo string, ref string) (*queue.BranchHealth, error) {
	if ref == "" {
		return nil, nil
	}
	var payload struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSON(fmt.Sprintf("/repos/%s/git/ref/heads/%s", repo, url.PathEscape(ref)), &payload); err != nil {
		return nil, err
	}
	rollup, err := c.GetCheckRollup(repo, queue.PullRequest{HeadSHA: payload.Object.SHA})
	if err != nil {
		return nil, err
	}
	return &queue.BranchHealth{Ref: ref, SHA: payload.Object.SHA, CheckState: rollup.State}, nil
}

func (c *Client) GetReviewThreads(repo string, prNumber int) (ReviewThreadResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return ReviewThreadResult{}, err
	}
	threads := []ReviewThread{}
	var threadsCursor *string
	for {
		var result reviewThreadsGraphQLResponse
		if err := c.postGraphQL(graphQLPayload{
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
              author { login }
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
		}, &result); err != nil {
			return ReviewThreadResult{}, err
		}
		if len(result.Errors) > 0 {
			return ReviewThreadResult{}, fmt.Errorf("GitHub GraphQL reviewThreads failed: %s", result.Errors[0].Message)
		}
		connection := result.Data.Repository.PullRequest.ReviewThreads
		for _, node := range connection.Nodes {
			comments := reviewCommentsFromNodes(node.Comments.Nodes)
			if node.Comments.PageInfo.HasNextPage {
				more, err := c.getRemainingReviewThreadComments(node.ID, node.Comments.PageInfo.EndCursor)
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
	return buildReviewThreadResult(repo, prNumber, threads), nil
}

func (c *Client) getRemainingReviewThreadComments(threadID, cursor string) ([]ReviewComment, error) {
	comments := []ReviewComment{}
	commentsCursor := &cursor
	for {
		var result reviewThreadCommentsGraphQLResponse
		if err := c.postGraphQL(graphQLPayload{
			Query: `query($id: ID!, $commentsCursor: String) {
  node(id: $id) {
    ... on PullRequestReviewThread {
      comments(first: 100, after: $commentsCursor) {
        nodes {
          body
          url
          author { login }
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
		}, &result); err != nil {
			return nil, err
		}
		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("GitHub GraphQL reviewThreads failed: %s", result.Errors[0].Message)
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

func (c *Client) postGraphQL(payload graphQLPayload, out any) error {
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/graphql", bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GitHub GraphQL reviewThreads failed: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
	Message string `json:"message"`
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
		Login string `json:"login"`
	} `json:"author"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func reviewCommentsFromNodes(nodes []reviewCommentNode) []ReviewComment {
	comments := make([]ReviewComment, 0, len(nodes))
	for _, comment := range nodes {
		comments = append(comments, ReviewComment{Author: comment.Author.Login, AuthorKind: classifyAuthor(comment.Author.Login), Body: comment.Body, URL: comment.URL})
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
		if threadHasHumanComment(thread) {
			summary.HumanUnresolved++
		} else {
			summary.BotUnresolved++
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

func threadHasHumanComment(thread ReviewThread) bool {
	for _, comment := range thread.Comments {
		if comment.AuthorKind == "human" {
			return true
		}
	}
	return false
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

func classifyAuthor(login string) string {
	normalized := strings.ToLower(login)
	switch {
	case strings.Contains(normalized, "coderabbit"):
		return "coderabbit"
	case strings.Contains(normalized, "greptile"):
		return "greptile"
	case strings.Contains(normalized, "codex"):
		return "codex"
	case strings.HasSuffix(normalized, "[bot]") || strings.Contains(normalized, "bot"):
		return "bot"
	case login == "":
		return "unknown"
	default:
		return "human"
	}
}
