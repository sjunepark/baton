package gh

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (c *Client) ListPullRequestReviews(repo string, number int) ([]PullRequestReview, error) {
	return c.ListPullRequestReviewsContext(context.Background(), repo, number)
}

func parseGitHubTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func (c *Client) ListPullRequestReviewsContext(ctx context.Context, repo string, number int) ([]PullRequestReview, error) {
	reviews := []PullRequestReview{}
	for page := 1; ; page++ {
		var batch []struct {
			ID          int64  `json:"id"`
			State       string `json:"state"`
			CommitSHA   string `json:"commit_id"`
			SubmittedAt string `json:"submitted_at"`
			User        struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"user"`
		}
		path := fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100&page=%d", repo, number, page)
		if err := c.getJSONContext(ctx, path, &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			submittedAt, err := parseGitHubTime(item.SubmittedAt)
			if err != nil {
				return nil, APIError{Method: "GET", Path: path, Cause: err}
			}
			reviews = append(reviews, PullRequestReview{
				ID: item.ID, State: item.State, CommitSHA: item.CommitSHA, SubmittedAt: submittedAt,
				Author: Actor{Login: item.User.Login, Type: item.User.Type},
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	return reviews, nil
}

func (c *Client) GetRequestedReviewers(repo string, number int) ([]ReviewRequest, error) {
	return c.GetRequestedReviewersContext(context.Background(), repo, number)
}

func (c *Client) GetRequestedReviewersContext(ctx context.Context, repo string, number int) ([]ReviewRequest, error) {
	var payload struct {
		Users []struct {
			Login string `json:"login"`
		} `json:"users"`
		Teams []struct {
			Slug string `json:"slug"`
		} `json:"teams"`
	}
	path := fmt.Sprintf("/repos/%s/pulls/%d/requested_reviewers", repo, number)
	if err := c.getJSONContext(ctx, path, &payload); err != nil {
		return nil, err
	}
	requests := make([]ReviewRequest, 0, len(payload.Users)+len(payload.Teams))
	for _, user := range payload.Users {
		requests = append(requests, ReviewRequest{Kind: "user", Login: user.Login})
	}
	for _, team := range payload.Teams {
		requests = append(requests, ReviewRequest{Kind: "team", Team: team.Slug})
	}
	return requests, nil
}

func (c *Client) GetBranch(repo, branch string) (Branch, error) {
	return c.GetBranchContext(context.Background(), repo, branch)
}

func (c *Client) GetBranchContext(ctx context.Context, repo, branch string) (Branch, error) {
	var payload struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Protection struct {
			RequiredStatusChecks struct {
				Contexts []string `json:"contexts"`
			} `json:"required_status_checks"`
		} `json:"protection"`
	}
	path := fmt.Sprintf("/repos/%s/branches/%s", repo, url.PathEscape(branch))
	if err := c.getJSONContext(ctx, path, &payload); err != nil {
		return Branch{}, err
	}
	required := make([]RequiredCheck, 0, len(payload.Protection.RequiredStatusChecks.Contexts))
	for _, check := range payload.Protection.RequiredStatusChecks.Contexts {
		required = append(required, RequiredCheck{Context: check})
	}
	return Branch{Ref: payload.Name, SHA: payload.Commit.SHA, Protected: payload.Protected, LegacyRequiredChecks: required}, nil
}

func (c *Client) GetEffectiveBranchRules(repo, branch string) (BranchRules, error) {
	return c.GetEffectiveBranchRulesContext(context.Background(), repo, branch)
}

func (c *Client) GetClassicBranchRules(repo, branch string) (BranchRules, error) {
	return c.GetClassicBranchRulesContext(context.Background(), repo, branch)
}

func (c *Client) GetClassicBranchRulesContext(ctx context.Context, repo, branch string) (BranchRules, error) {
	var payload struct {
		RequiredStatusChecks *struct {
			Strict   bool     `json:"strict"`
			Contexts []string `json:"contexts"`
			Checks   []struct {
				Context string `json:"context"`
				AppID   int64  `json:"app_id"`
			} `json:"checks"`
		} `json:"required_status_checks"`
		RequiredPullRequestReviews *struct {
			DismissStaleReviews     bool `json:"dismiss_stale_reviews"`
			RequireLastPushApproval bool `json:"require_last_push_approval"`
			RequiredApprovals       int  `json:"required_approving_review_count"`
		} `json:"required_pull_request_reviews"`
	}
	path := fmt.Sprintf("/repos/%s/branches/%s/protection", repo, url.PathEscape(branch))
	if err := c.getJSONContext(ctx, path, &payload); err != nil {
		return BranchRules{}, err
	}
	rules := BranchRules{Branch: branch}
	if status := payload.RequiredStatusChecks; status != nil {
		rules.StrictRequiredChecks = status.Strict
		identityContexts := map[string]struct{}{}
		for _, check := range status.Checks {
			rules.RequiredChecks = append(rules.RequiredChecks, RequiredCheck{Context: check.Context, IntegrationID: check.AppID})
			identityContexts[strings.ToLower(check.Context)] = struct{}{}
		}
		for _, context := range status.Contexts {
			if _, represented := identityContexts[strings.ToLower(context)]; represented {
				continue
			}
			rules.RequiredChecks = append(rules.RequiredChecks, RequiredCheck{Context: context})
		}
	}
	if reviews := payload.RequiredPullRequestReviews; reviews != nil {
		rules.DismissStaleReviews = reviews.DismissStaleReviews
		rules.RequireLastPushApproval = reviews.RequireLastPushApproval
		rules.RequiredApprovingReviewCount = reviews.RequiredApprovals
	}
	return rules, nil
}

func (c *Client) GetEffectiveBranchRulesContext(ctx context.Context, repo, branch string) (BranchRules, error) {
	type branchRulePayload struct {
		Type       string `json:"type"`
		Parameters struct {
			StrictRequiredChecks    bool `json:"strict_required_status_checks_policy"`
			DismissStaleReviews     bool `json:"dismiss_stale_reviews_on_push"`
			RequireLastPushApproval bool `json:"require_last_push_approval"`
			RequiredChecks          []struct {
				Context       string `json:"context"`
				IntegrationID int64  `json:"integration_id"`
			} `json:"required_status_checks"`
			RequiredApprovingReviewCount int `json:"required_approving_review_count"`
		} `json:"parameters"`
	}
	payload := []branchRulePayload{}
	for page := 1; ; page++ {
		var batch []branchRulePayload
		path := fmt.Sprintf("/repos/%s/rules/branches/%s?per_page=100&page=%d", repo, url.PathEscape(branch), page)
		if err := c.getJSONContext(ctx, path, &batch); err != nil {
			return BranchRules{}, err
		}
		payload = append(payload, batch...)
		if len(batch) < 100 {
			break
		}
	}
	result := BranchRules{Branch: branch}
	for _, rule := range payload {
		switch rule.Type {
		case "required_status_checks":
			result.StrictRequiredChecks = result.StrictRequiredChecks || rule.Parameters.StrictRequiredChecks
			for _, check := range rule.Parameters.RequiredChecks {
				result.RequiredChecks = append(result.RequiredChecks, RequiredCheck{Context: check.Context, IntegrationID: check.IntegrationID})
			}
		case "pull_request":
			result.DismissStaleReviews = result.DismissStaleReviews || rule.Parameters.DismissStaleReviews
			result.RequireLastPushApproval = result.RequireLastPushApproval || rule.Parameters.RequireLastPushApproval
			if rule.Parameters.RequiredApprovingReviewCount > result.RequiredApprovingReviewCount {
				result.RequiredApprovingReviewCount = rule.Parameters.RequiredApprovingReviewCount
			}
		}
	}
	return result, nil
}
