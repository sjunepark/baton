package gh

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// ListIssuesByLabelContext retrieves GitHub issues using the server-side label
// filter. GitHub's issues endpoint also returns pull requests, which are
// excluded before facts reach the Task adapter.
func (c *Client) ListIssuesByLabelContext(ctx context.Context, repo, state, label string) ([]Issue, error) {
	result := []Issue{}
	for page := 1; ; page++ {
		query := url.Values{
			"state":    {state},
			"labels":   {label},
			"per_page": {"100"},
			"page":     {strconv.Itoa(page)},
		}
		var batch []issueListPayload
		if err := c.getJSONContext(ctx, fmt.Sprintf("/repos/%s/issues?%s", repo, query.Encode()), &batch); err != nil {
			return nil, err
		}
		for _, resource := range batch {
			if resource.PullRequest != nil {
				continue
			}
			result = append(result, issueFromListPayload(resource))
		}
		if len(batch) < 100 {
			break
		}
	}
	return result, nil
}

type issueListPayload struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	State       string    `json:"state"`
	PullRequest *struct{} `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type issuePayload struct {
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

func issueFromPayload(resource issuePayload) Issue {
	labels := make([]string, 0, len(resource.Labels))
	for _, label := range resource.Labels {
		labels = append(labels, label.Name)
	}
	return Issue{
		Number: resource.Number, NodeID: resource.NodeID, Title: resource.Title,
		URL: resource.HTMLURL, Body: resource.Body, Labels: labels, State: resource.State,
		PullRequest: resource.PullRequest != nil, Locked: resource.Locked, CommentCount: resource.Comments,
	}
}

func issueFromListPayload(resource issueListPayload) Issue {
	labels := make([]string, 0, len(resource.Labels))
	for _, label := range resource.Labels {
		labels = append(labels, label.Name)
	}
	return Issue{
		Number: resource.Number, Title: resource.Title, URL: resource.HTMLURL,
		Labels: labels, State: resource.State, PullRequest: resource.PullRequest != nil,
	}
}

// GetLabelContext reads one label without scanning the repository taxonomy.
func (c *Client) GetLabelContext(ctx context.Context, repo, name string) (Label, bool, error) {
	var label Label
	path := fmt.Sprintf("/repos/%s/labels/%s", repo, url.PathEscape(name))
	if err := c.doJSONContext(ctx, http.MethodGet, path, nil, &label, true); err != nil {
		return Label{}, false, err
	}
	if label.Name == "" {
		return Label{}, false, nil
	}
	return label, true, nil
}

// DeleteIssueLabelContext preserves a 404 so the Task adapter can distinguish
// an already-absent label from an unavailable issue or repository.
func (c *Client) DeleteIssueLabelContext(ctx context.Context, repo string, issueNumber int, label string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/labels/%s", repo, issueNumber, url.PathEscape(label))
	return c.requestNoBodyContext(ctx, http.MethodDelete, path, nil, false)
}
