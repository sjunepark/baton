package gh

import (
	"encoding/json"
	"fmt"
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

func ParsePullRequestEvent(content []byte) (PullRequestEvent, error) {
	var event struct {
		Action      string `json:"action"`
		PullRequest *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			State  string `json:"state"`
			Merged bool   `json:"merged"`
			Base   struct {
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
		} `json:"pull_request"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(content, &event); err != nil {
		return PullRequestEvent{}, fmt.Errorf("parse pull request event: %w", err)
	}
	if event.PullRequest == nil {
		return PullRequestEvent{}, fmt.Errorf("event payload does not contain a pull request")
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
	return PullRequestEvent{
		Action:                 event.Action,
		Number:                 event.PullRequest.Number,
		Title:                  event.PullRequest.Title,
		Body:                   event.PullRequest.Body,
		BaseRef:                event.PullRequest.Base.Ref,
		HeadRef:                event.PullRequest.Head.Ref,
		BaseRepositoryFullName: baseRepo,
		HeadRepositoryFullName: headRepo,
		BaseSHA:                event.PullRequest.Base.SHA,
		HeadSHA:                event.PullRequest.Head.SHA,
		State:                  event.PullRequest.State,
		Merged:                 event.PullRequest.Merged,
	}, nil
}
