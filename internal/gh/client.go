package gh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/labels"
	"github.com/sjunepark/baton/internal/policy"
)

const defaultAPIBase = "https://api.github.com"

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type APIError struct {
	Method     string
	Path       string
	Status     string
	StatusCode int
}

func (err APIError) Error() string {
	return fmt.Sprintf("GitHub API %s %s failed: %s", err.Method, err.Path, err.Status)
}

func IsNotFound(err error) bool {
	var apiErr APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func NewClientFromEnv() (*Client, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		out, err := exec.Command("gh", "auth", "token").Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN, GH_TOKEN, or gh auth token is required")
	}
	return NewClient(os.Getenv("GITHUB_API_URL"), token, http.DefaultClient), nil
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

func (c *Client) FetchIssueLabels(repo string, issueNumbers []int) ([]policy.ReferencedIssue, error) {
	issues := make([]policy.ReferencedIssue, 0, len(issueNumbers))
	for _, issueNumber := range issueNumbers {
		var payload struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		if err := c.getJSON(fmt.Sprintf("/repos/%s/issues/%d", repo, issueNumber), &payload); err != nil {
			return nil, err
		}
		names := make([]string, 0, len(payload.Labels))
		for _, label := range payload.Labels {
			names = append(names, label.Name)
		}
		issues = append(issues, policy.ReferencedIssue{Number: issueNumber, Labels: names})
	}
	return issues, nil
}

func (c *Client) FetchCommitListing(repo string, prNumber int) ([]string, bool, error) {
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
		if err := c.getJSON(path, &batch); err != nil {
			return nil, false, err
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
	return messages, len(commits) >= policy.PRCommitListingCap, nil
}

func (c *Client) ApplyIssueDecision(repo string, issueNumber int, decision policy.IssuePolicyDecision, marker string, qualityGateLabel string) error {
	if !decision.IsFormIssue {
		return nil
	}
	if len(decision.LabelsToAdd) > 0 {
		if err := c.AddIssueLabels(repo, issueNumber, decision.LabelsToAdd); err != nil {
			return err
		}
	}
	for _, label := range decision.LabelsToRemove {
		if err := c.RemoveIssueLabel(repo, issueNumber, label); err != nil {
			return err
		}
	}
	return c.applyPolicyComment(repo, issueNumber, decision.PolicyCommentBody, marker, qualityGateLabel)
}

func (c *Client) AddIssueLabels(repo string, issueNumber int, labelNames []string) error {
	return c.postJSON(fmt.Sprintf("/repos/%s/issues/%d/labels", repo, issueNumber), map[string]any{"labels": labelNames}, nil)
}

func (c *Client) RemoveIssueLabel(repo string, issueNumber int, label string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/labels/%s", repo, issueNumber, url.PathEscape(label))
	return c.requestNoBody(http.MethodDelete, path, nil, true)
}

func (c *Client) applyPolicyComment(repo string, issueNumber int, commentBody *string, marker string, qualityGateLabel string) error {
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := c.getJSON(fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, issueNumber), &comments); err != nil {
		return err
	}
	var existingID int64
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			existingID = comment.ID
			break
		}
	}
	if commentBody == nil {
		if existingID == 0 {
			return nil
		}
		clearBody := policy.ClearIssuePolicyComment(marker, qualityGateLabel)
		return c.patchJSON(fmt.Sprintf("/repos/%s/issues/comments/%d", repo, existingID), map[string]any{"body": clearBody}, nil)
	}
	if existingID != 0 {
		return c.patchJSON(fmt.Sprintf("/repos/%s/issues/comments/%d", repo, existingID), map[string]any{"body": *commentBody}, nil)
	}
	return c.postJSON(fmt.Sprintf("/repos/%s/issues/%d/comments", repo, issueNumber), map[string]any{"body": *commentBody}, nil)
}

func (c *Client) ListLabels(repo string) ([]labels.Label, error) {
	out := []labels.Label{}
	for page := 1; ; page++ {
		var batch []labels.Label
		if err := c.getJSON(fmt.Sprintf("/repos/%s/labels?per_page=100&page=%d", repo, page), &batch); err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return out, nil
}

func (c *Client) CreateLabel(repo string, label labels.Label) error {
	return c.postJSON(fmt.Sprintf("/repos/%s/labels", repo), label, nil)
}

func (c *Client) UpdateLabel(repo string, label labels.Label) error {
	return c.patchJSON(fmt.Sprintf("/repos/%s/labels/%s", repo, url.PathEscape(label.Name)), label, nil)
}

func (c *Client) CreateIssueComment(repo string, number int, body string) error {
	return c.postJSON(fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]any{"body": body}, nil)
}

func (c *Client) getJSON(path string, out any) error {
	return c.doJSON(http.MethodGet, path, nil, out, false)
}

func (c *Client) postJSON(path string, in any, out any) error {
	return c.doJSON(http.MethodPost, path, in, out, false)
}

func (c *Client) patchJSON(path string, in any, out any) error {
	return c.doJSON(http.MethodPatch, path, in, out, false)
}

func (c *Client) requestNoBody(method, path string, in any, allowNotFound bool) error {
	return c.doJSON(method, path, in, nil, allowNotFound)
}

func (c *Client) doJSON(method, path string, in any, out any, allowNotFound bool) error {
	var body io.Reader
	if in != nil {
		content, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(content)
	}
	req, err := http.NewRequest(method, c.baseURL+path, body)
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
		return err
	}
	defer resp.Body.Close()
	if allowNotFound && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return APIError{Method: method, Path: path, Status: resp.Status, StatusCode: resp.StatusCode}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func IssueNumbersForPR(pr policy.PullRequest) []int {
	values := append(policy.ExtractReferenceIssueNumbers(pr.Title), policy.ExtractReferenceIssueNumbers(pr.Body)...)
	values = append(values, policy.ExtractClosingIssueNumbers(pr.Title)...)
	values = append(values, policy.ExtractClosingIssueNumbers(pr.Body)...)
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
