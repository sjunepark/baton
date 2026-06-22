package gh

import (
	"encoding/json"
	"fmt"

	"github.com/sjunepark/baton/internal/policy"
)

type IssueEvent struct {
	Number     int      `json:"number"`
	Body       string   `json:"body"`
	Labels     []string `json:"labels"`
	Repository string   `json:"repository"`
}

func ParseIssueEvent(content []byte) (IssueEvent, error) {
	var event struct {
		Issue *struct {
			Number int    `json:"number"`
			Body   string `json:"body"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(content, &event); err != nil {
		return IssueEvent{}, fmt.Errorf("parse issue event: %w", err)
	}
	if event.Issue == nil {
		return IssueEvent{}, fmt.Errorf("event payload does not contain an issue")
	}
	labels := make([]string, 0, len(event.Issue.Labels))
	for _, label := range event.Issue.Labels {
		if label.Name != "" {
			labels = append(labels, label.Name)
		}
	}
	repo := ""
	if event.Repository != nil {
		repo = event.Repository.FullName
	}
	return IssueEvent{
		Number:     event.Issue.Number,
		Body:       event.Issue.Body,
		Labels:     labels,
		Repository: repo,
	}, nil
}

func ParsePullRequestEvent(content []byte) (policy.PullRequest, error) {
	var event struct {
		PullRequest *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			Base   struct {
				Ref  string `json:"ref"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"base"`
			Head struct {
				Ref  string `json:"ref"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(content, &event); err != nil {
		return policy.PullRequest{}, fmt.Errorf("parse pull request event: %w", err)
	}
	if event.PullRequest == nil {
		return policy.PullRequest{}, fmt.Errorf("event payload does not contain a pull request")
	}
	baseRepo := ""
	if event.PullRequest.Base.Repo != nil {
		baseRepo = event.PullRequest.Base.Repo.FullName
	} else if event.Repository != nil {
		baseRepo = event.Repository.FullName
	}
	headRepo := ""
	if event.PullRequest.Head.Repo != nil {
		headRepo = event.PullRequest.Head.Repo.FullName
	}
	return policy.PullRequest{
		Number:                 event.PullRequest.Number,
		Title:                  event.PullRequest.Title,
		Body:                   event.PullRequest.Body,
		BaseRef:                event.PullRequest.Base.Ref,
		HeadRef:                event.PullRequest.Head.Ref,
		BaseRepositoryFullName: baseRepo,
		HeadRepositoryFullName: headRepo,
	}, nil
}
