package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/auth"
)

const defaultAPIBase = "https://api.github.com"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type APIError struct {
	Method         string
	Path           string
	Status         string
	StatusCode     int
	RequestID      string
	RetryAfter     time.Duration
	RateLimited    bool
	RateLimitReset time.Time
	Cause          error

	branchRulesUnavailableByPlan bool
}

func (err APIError) Error() string {
	return err.SafeMessage()
}

func (err APIError) SafeMessage() string {
	if err.Status != "" {
		return fmt.Sprintf("GitHub API %s %s failed: %s", err.Method, err.Path, err.Status)
	}
	return fmt.Sprintf("GitHub API %s %s could not be completed", err.Method, err.Path)
}

func (err APIError) Unwrap() error                     { return err.Cause }
func (err APIError) UpstreamHTTPStatus() int           { return err.StatusCode }
func (err APIError) UpstreamRequestID() string         { return err.RequestID }
func (err APIError) UpstreamRetryAfter() time.Duration { return err.RetryAfter }
func (err APIError) UpstreamDetails() map[string]string {
	details := map[string]string{"method": err.Method, "path": err.Path, "status": err.Status}
	if err.RateLimited {
		details["rateLimited"] = "true"
	}
	if !err.RateLimitReset.IsZero() {
		details["rateLimitReset"] = err.RateLimitReset.UTC().Format(time.RFC3339)
	}
	return details
}
func (err APIError) UpstreamRetryable() bool {
	return err.StatusCode == 0 || err.RateLimited || err.StatusCode >= 500 || err.RetryAfter > 0
}

func IsNotFound(err error) bool {
	var apiErr APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func isBranchRulesUnavailableByPlan(err error) bool {
	var apiErr APIError
	return errors.As(err, &apiErr) && apiErr.branchRulesUnavailableByPlan
}

func NewClientFromEnv() (*Client, error) {
	credentials, err := auth.Discover(auth.Inputs{GitHubToken: os.Getenv("GITHUB_TOKEN"), GHToken: os.Getenv("GH_TOKEN")})
	if err != nil {
		return nil, err
	}
	return NewClient(os.Getenv("GITHUB_API_URL"), credentials.Token(), http.DefaultClient), nil
}

func NewClientWithCredentials(baseURL string, credentials auth.Credentials, httpClient *http.Client) *Client {
	return NewClient(baseURL, credentials.Token(), httpClient)
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultAPIBase
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: httpClient}
}

func (c *Client) FetchCommitListing(repo string, prNumber int) (CommitListing, error) {
	return c.FetchCommitListingContext(context.Background(), repo, prNumber)
}

func (c *Client) FetchCommitListingContext(ctx context.Context, repo string, prNumber int) (CommitListing, error) {
	var commits []struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	for page := 1; ; page++ {
		var batch []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		path := fmt.Sprintf("/repos/%s/pulls/%d/commits?per_page=100&page=%d", repo, prNumber, page)
		if err := c.getJSONContext(ctx, path, &batch); err != nil {
			return CommitListing{}, err
		}
		commits = append(commits, batch...)
		if len(batch) < 100 {
			break
		}
	}
	messages := make([]string, len(commits))
	for i, commit := range commits {
		messages[i] = commit.Commit.Message
	}
	return CommitListing{Messages: messages, Count: len(commits), GitHubCapReached: len(commits) == 250}, nil
}

func (c *Client) AddIssueLabels(repo string, issueNumber int, labelNames []string) error {
	return c.AddIssueLabelsContext(context.Background(), repo, issueNumber, labelNames)
}

func (c *Client) AddIssueLabelsContext(ctx context.Context, repo string, issueNumber int, labelNames []string) error {
	return c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d/labels", repo, issueNumber), map[string]any{"labels": labelNames}, nil)
}

func (c *Client) RemoveIssueLabel(repo string, issueNumber int, label string) error {
	return c.RemoveIssueLabelContext(context.Background(), repo, issueNumber, label)
}

func (c *Client) RemoveIssueLabelContext(ctx context.Context, repo string, issueNumber int, label string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/labels/%s", repo, issueNumber, url.PathEscape(label))
	return c.requestNoBodyContext(ctx, http.MethodDelete, path, nil, true)
}

func (c *Client) CloseIssue(repo string, issueNumber int) error {
	return c.CloseIssueContext(context.Background(), repo, issueNumber)
}

func (c *Client) CloseIssueContext(ctx context.Context, repo string, issueNumber int) error {
	return c.patchJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d", repo, issueNumber), map[string]any{"state": "closed"}, nil)
}

func (c *Client) ListIssueComments(repo string, issueNumber int) ([]IssueComment, error) {
	return c.ListIssueCommentsContext(context.Background(), repo, issueNumber)
}

func (c *Client) ListIssueCommentsContext(ctx context.Context, repo string, issueNumber int) ([]IssueComment, error) {
	comments := []IssueComment{}
	for page := 1; ; page++ {
		var batch []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"user"`
		}
		path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", repo, issueNumber, page)
		if err := c.getJSONContext(ctx, path, &batch); err != nil {
			return nil, err
		}
		for _, comment := range batch {
			comments = append(comments, IssueComment{ID: comment.ID, Body: comment.Body, Author: Actor{Login: comment.User.Login, Type: comment.User.Type}})
		}
		if len(batch) < 100 {
			break
		}
	}
	return comments, nil
}

func (c *Client) UpdateIssueComment(repo string, commentID int64, body string) error {
	return c.UpdateIssueCommentContext(context.Background(), repo, commentID, body)
}

func (c *Client) UpdateIssueCommentContext(ctx context.Context, repo string, commentID int64, body string) error {
	return c.patchJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, commentID), map[string]any{"body": body}, nil)
}

func (c *Client) ListLabels(repo string) ([]Label, error) {
	return c.ListLabelsContext(context.Background(), repo)
}

func (c *Client) ListLabelsContext(ctx context.Context, repo string) ([]Label, error) {
	out := []Label{}
	for page := 1; ; page++ {
		var batch []Label
		if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/labels?per_page=100&page=%d", repo, page), &batch); err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) CreateLabel(repo string, label Label) error {
	return c.CreateLabelContext(context.Background(), repo, label)
}

func (c *Client) CreateLabelContext(ctx context.Context, repo string, label Label) error {
	return c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/labels", repo), label, nil)
}

func (c *Client) UpdateLabel(repo string, label Label) error {
	return c.UpdateLabelContext(context.Background(), repo, label)
}

func (c *Client) UpdateLabelContext(ctx context.Context, repo string, label Label) error {
	return c.patchJSONContext(ctx, fmt.Sprintf("/repos/%s/labels/%s", repo, url.PathEscape(label.Name)), label, nil)
}

func (c *Client) CreateIssueComment(repo string, number int, body string) error {
	return c.CreateIssueCommentContext(context.Background(), repo, number, body)
}

func (c *Client) CreateIssueCommentContext(ctx context.Context, repo string, number int, body string) error {
	return c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]any{"body": body}, nil)
}

func (c *Client) getJSONContext(ctx context.Context, path string, out any) error {
	return c.doJSONContext(ctx, http.MethodGet, path, nil, out, false)
}

func (c *Client) postJSONContext(ctx context.Context, path string, in any, out any) error {
	return c.doJSONContext(ctx, http.MethodPost, path, in, out, false)
}

func (c *Client) patchJSONContext(ctx context.Context, path string, in any, out any) error {
	return c.doJSONContext(ctx, http.MethodPatch, path, in, out, false)
}

func (c *Client) requestNoBodyContext(ctx context.Context, method, path string, in any, allowNotFound bool) error {
	return c.doJSONContext(ctx, method, path, in, nil, allowNotFound)
}

func (c *Client) doJSONContext(ctx context.Context, method, path string, in any, out any, allowNotFound bool) error {
	var body io.Reader
	if in != nil {
		content, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(content)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return APIError{Method: method, Path: path, Cause: err}
	}
	defer resp.Body.Close()
	if allowNotFound && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := APIError{
			Method: method, Path: path, Status: resp.Status, StatusCode: resp.StatusCode,
			RequestID: resp.Header.Get("X-GitHub-Request-Id"),
		}
		applyRateLimitMetadata(&apiErr, resp.Header, time.Now())
		applyErrorResponseMetadata(&apiErr, resp.Body)
		return apiErr
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return APIError{
			Method: method, Path: path, Status: resp.Status, StatusCode: resp.StatusCode,
			RequestID: resp.Header.Get("X-GitHub-Request-Id"), Cause: err,
		}
	}
	return nil
}

func applyErrorResponseMetadata(apiErr *APIError, body io.Reader) {
	if apiErr.StatusCode != http.StatusForbidden || apiErr.RateLimited {
		return
	}
	var payload struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&payload); err != nil {
		return
	}
	if payload.Message != "Upgrade to GitHub Pro or make this repository public to enable this feature." {
		return
	}
	path := strings.SplitN(apiErr.Path, "?", 2)[0]
	switch {
	case strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/branches/") && strings.HasSuffix(path, "/protection"):
		apiErr.branchRulesUnavailableByPlan = payload.DocumentationURL == "https://docs.github.com/rest/branches/branch-protection#get-branch-protection"
	case strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/rules/branches/"):
		apiErr.branchRulesUnavailableByPlan = payload.DocumentationURL == "https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"
	}
}

func applyRateLimitMetadata(apiErr *APIError, header http.Header, now time.Time) {
	apiErr.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	apiErr.RateLimited = apiErr.RateLimited || apiErr.StatusCode == http.StatusTooManyRequests || apiErr.RetryAfter > 0 || header.Get("X-RateLimit-Remaining") == "0"
	resetUnix, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	if apiErr.RateLimited && err == nil && resetUnix > 0 {
		apiErr.RateLimitReset = time.Unix(resetUnix, 0).UTC()
		if apiErr.RetryAfter == 0 && apiErr.RateLimitReset.After(now) {
			apiErr.RetryAfter = apiErr.RateLimitReset.Sub(now)
		}
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func RepoFromEnvOrFlag(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" {
		return repo, nil
	}
	return "", fmt.Errorf("--repo or GITHUB_REPOSITORY is required")
}

func IssueNumberFromString(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("issue number is required")
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return number, nil
}
