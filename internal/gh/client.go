package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
}

func (e APIError) Error() string { return e.SafeMessage() }

func (e APIError) SafeMessage() string {
	if e.Status != "" {
		return fmt.Sprintf("GitHub API %s %s failed: %s", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("GitHub API %s %s could not be completed", e.Method, e.Path)
}

func (e APIError) Unwrap() error { return e.Cause }

func IsNotFound(err error) bool {
	var apiErr APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func NewClientWithCredentials(baseURL string, credentials auth.Credentials, httpClient *http.Client) *Client {
	return NewClient(baseURL, credentials.Token(), httpClient)
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBase
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: httpClient}
}

func (c *Client) AddIssueLabelsContext(ctx context.Context, repo string, issueNumber int, labels []string) error {
	return c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d/labels", repo, issueNumber), map[string]any{"labels": labels}, nil)
}

func (c *Client) CloseIssueContext(ctx context.Context, repo string, issueNumber int) error {
	return c.patchJSONContext(ctx, fmt.Sprintf("/repos/%s/issues/%d", repo, issueNumber), map[string]any{"state": "closed"}, nil)
}

func (c *Client) CreateLabelContext(ctx context.Context, repo string, label Label) error {
	return c.postJSONContext(ctx, fmt.Sprintf("/repos/%s/labels", repo), label, nil)
}

func (c *Client) getJSONContext(ctx context.Context, path string, out any) error {
	return c.doJSONContext(ctx, http.MethodGet, path, nil, out, false)
}

func (c *Client) postJSONContext(ctx context.Context, path string, in, out any) error {
	return c.doJSONContext(ctx, http.MethodPost, path, in, out, false)
}

func (c *Client) patchJSONContext(ctx context.Context, path string, in, out any) error {
	return c.doJSONContext(ctx, http.MethodPatch, path, in, out, false)
}

func (c *Client) requestNoBodyContext(ctx context.Context, method, path string, in any, allowNotFound bool) error {
	return c.doJSONContext(ctx, method, path, in, nil, allowNotFound)
}

func (c *Client) doJSONContext(ctx context.Context, method, path string, in, out any, allowNotFound bool) error {
	var body io.Reader
	if in != nil {
		content, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(content)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return APIError{Method: method, Path: path, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if allowNotFound && response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		apiErr := APIError{
			Method: method, Path: path, Status: response.Status, StatusCode: response.StatusCode,
			RequestID: response.Header.Get("X-GitHub-Request-Id"),
		}
		applyRateLimitMetadata(&apiErr, response.Header, time.Now())
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return apiErr
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return APIError{
			Method: method, Path: path, Status: response.Status, StatusCode: response.StatusCode,
			RequestID: response.Header.Get("X-GitHub-Request-Id"), Cause: err,
		}
	}
	return nil
}

func applyRateLimitMetadata(apiErr *APIError, header http.Header, now time.Time) {
	apiErr.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	apiErr.RateLimited = apiErr.StatusCode == http.StatusTooManyRequests || apiErr.RetryAfter > 0 || header.Get("X-RateLimit-Remaining") == "0"
	resetUnix, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	if apiErr.RateLimited && err == nil && resetUnix > 0 {
		apiErr.RateLimitReset = time.Unix(resetUnix, 0).UTC()
		if apiErr.RetryAfter == 0 && apiErr.RateLimitReset.After(now) {
			apiErr.RetryAfter = apiErr.RateLimitReset.Sub(now)
		}
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}
