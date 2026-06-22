package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sejunpark/baton/internal/queue"
)

type CheckRollup struct {
	SchemaVersion int          `json:"schemaVersion"`
	Kind          string       `json:"kind"`
	Repo          string       `json:"repo"`
	PRNumber      int          `json:"prNumber"`
	HeadSHA       string       `json:"headSha"`
	State         string       `json:"state"`
	Checks        []CheckState `json:"checks"`
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
	Threads       []ReviewThread `json:"threads"`
}

type ReviewThread struct {
	IsResolved bool            `json:"isResolved"`
	IsOutdated bool            `json:"isOutdated"`
	Path       string          `json:"path"`
	Line       int             `json:"line"`
	Comments   []ReviewComment `json:"comments"`
}

type ReviewComment struct {
	Author     string `json:"author"`
	AuthorKind string `json:"authorKind"`
	Body       string `json:"body"`
	URL        string `json:"url"`
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
		return CheckRollup{SchemaVersion: 1, Kind: "checkRollup", Repo: repo, PRNumber: pr.Number, State: "unknown", Checks: checks}, nil
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
	return CheckRollup{SchemaVersion: 1, Kind: "checkRollup", Repo: repo, PRNumber: pr.Number, HeadSHA: pr.HeadSHA, State: classifyChecks(checks), Checks: checks}, nil
}

func (c *Client) GetReviewThreads(repo string, prNumber int) (ReviewThreadResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return ReviewThreadResult{}, err
	}
	var payload struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	payload.Query = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          isResolved
          isOutdated
          path
          line
          comments(first: 20) {
            nodes {
              body
              url
              author { login }
            }
          }
        }
      }
    }
  }
}`
	payload.Variables = map[string]any{"owner": owner, "name": name, "number": prNumber}
	content, err := json.Marshal(payload)
	if err != nil {
		return ReviewThreadResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/graphql", bytes.NewReader(content))
	if err != nil {
		return ReviewThreadResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ReviewThreadResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ReviewThreadResult{}, fmt.Errorf("GitHub GraphQL reviewThreads failed: %s", resp.Status)
	}
	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool   `json:"isResolved"`
							IsOutdated bool   `json:"isOutdated"`
							Path       string `json:"path"`
							Line       int    `json:"line"`
							Comments   struct {
								Nodes []struct {
									Body   string `json:"body"`
									URL    string `json:"url"`
									Author struct {
										Login string `json:"login"`
									} `json:"author"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ReviewThreadResult{}, err
	}
	if len(result.Errors) > 0 {
		return ReviewThreadResult{}, fmt.Errorf("GitHub GraphQL reviewThreads failed: %s", result.Errors[0].Message)
	}
	threads := []ReviewThread{}
	for _, node := range result.Data.Repository.PullRequest.ReviewThreads.Nodes {
		comments := []ReviewComment{}
		for _, comment := range node.Comments.Nodes {
			comments = append(comments, ReviewComment{Author: comment.Author.Login, AuthorKind: classifyAuthor(comment.Author.Login), Body: comment.Body, URL: comment.URL})
		}
		threads = append(threads, ReviewThread{IsResolved: node.IsResolved, IsOutdated: node.IsOutdated, Path: node.Path, Line: node.Line, Comments: comments})
	}
	return ReviewThreadResult{SchemaVersion: 1, Kind: "reviewThreads", Repo: repo, PRNumber: prNumber, Threads: threads}, nil
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
