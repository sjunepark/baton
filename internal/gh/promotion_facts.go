package gh

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

func (c *Client) FetchPromotionHistory(repo, baseSHA, headSHA, stagingBranch, workBranchPrefix string) (PromotionHistory, error) {
	return c.FetchPromotionHistoryContext(context.Background(), repo, baseSHA, headSHA, stagingBranch, workBranchPrefix)
}

func (c *Client) FetchPromotionHistoryContext(ctx context.Context, repo, baseSHA, headSHA, stagingBranch, workBranchPrefix string) (PromotionHistory, error) {
	if strings.TrimSpace(baseSHA) == "" || strings.TrimSpace(headSHA) == "" {
		return PromotionHistory{WorkPullRequests: []PromotionWorkPullRequest{}}, nil
	}
	if baseSHA == headSHA {
		return PromotionHistory{WorkPullRequests: []PromotionWorkPullRequest{}, Complete: true}, nil
	}

	commitNodeIDs, complete, err := c.compareCommitNodeIDs(ctx, repo, baseSHA, headSHA)
	if err != nil || !complete {
		return PromotionHistory{WorkPullRequests: []PromotionWorkPullRequest{}, Complete: complete}, err
	}
	associations, complete, err := c.promotionAssociations(ctx, commitNodeIDs)
	if err != nil {
		return PromotionHistory{}, err
	}

	workByNumber := map[int]PromotionWorkPullRequest{}
	for _, pullRequest := range associations {
		isWork, factComplete := isPromotionWorkPullRequest(pullRequest, repo, stagingBranch, workBranchPrefix)
		if !factComplete {
			complete = false
		}
		if !isWork {
			continue
		}
		workByNumber[pullRequest.Number] = PromotionWorkPullRequest{
			Number: pullRequest.Number,
			Title:  pullRequest.Title,
			Body:   pullRequest.Body,
		}
	}

	work := make([]PromotionWorkPullRequest, 0, len(workByNumber))
	for _, pullRequest := range workByNumber {
		work = append(work, pullRequest)
	}
	sort.Slice(work, func(i, j int) bool { return work[i].Number < work[j].Number })
	return PromotionHistory{WorkPullRequests: work, Complete: complete}, nil
}

func (c *Client) compareCommitNodeIDs(ctx context.Context, repo, baseSHA, headSHA string) ([]string, bool, error) {
	total := -1
	nodeIDs := []string{}
	seen := map[string]struct{}{}
	comparison := url.PathEscape(baseSHA + "..." + headSHA)
	for page := 1; ; page++ {
		var payload struct {
			TotalCommits int `json:"total_commits"`
			Commits      []struct {
				NodeID string `json:"node_id"`
			} `json:"commits"`
		}
		path := fmt.Sprintf("/repos/%s/compare/%s?per_page=100&page=%d", repo, comparison, page)
		if err := c.getJSONContext(ctx, path, &payload); err != nil {
			return nil, false, err
		}
		if page == 1 {
			total = payload.TotalCommits
			if total > PromotionCommitCap {
				return []string{}, false, nil
			}
		}
		for _, commit := range payload.Commits {
			if commit.NodeID == "" {
				return []string{}, false, nil
			}
			if _, duplicate := seen[commit.NodeID]; duplicate {
				return []string{}, false, nil
			}
			seen[commit.NodeID] = struct{}{}
			nodeIDs = append(nodeIDs, commit.NodeID)
		}
		if len(nodeIDs) >= total || len(payload.Commits) == 0 {
			break
		}
	}
	if total < 0 || len(nodeIDs) != total {
		return []string{}, false, nil
	}
	return nodeIDs, true, nil
}

func (c *Client) promotionAssociations(ctx context.Context, commitNodeIDs []string) ([]promotionPullRequestNode, bool, error) {
	associations := []promotionPullRequestNode{}
	complete := true
	for start := 0; start < len(commitNodeIDs); start += promotionCommitNodeBatchSize {
		end := min(start+promotionCommitNodeBatchSize, len(commitNodeIDs))
		requested := map[string]struct{}{}
		for _, nodeID := range commitNodeIDs[start:end] {
			requested[nodeID] = struct{}{}
		}
		var response promotionCommitNodesResponse
		headers, err := c.postGraphQLContext(ctx, graphQLPayload{
			Query: `query($ids: [ID!]!) {
  nodes(ids: $ids) {
    ... on Commit {
      id
      associatedPullRequests(first: 100) {
        nodes { number title body mergedAt baseRefName headRefName baseRepository { nameWithOwner } }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`,
			Variables: map[string]any{"ids": commitNodeIDs[start:end]},
		}, &response)
		if err != nil {
			return nil, false, err
		}
		if len(response.Errors) > 0 {
			return nil, false, graphQLResponseError(response.Errors, headers)
		}
		seen := map[string]struct{}{}
		for _, commit := range response.Data.Nodes {
			if commit == nil || commit.ID == "" {
				complete = false
				continue
			}
			if _, expected := requested[commit.ID]; !expected {
				complete = false
				continue
			}
			if _, duplicate := seen[commit.ID]; duplicate {
				complete = false
				continue
			}
			seen[commit.ID] = struct{}{}
			connection := commit.AssociatedPullRequests
			associations = append(associations, connection.Nodes...)
			if !connection.PageInfo.HasNextPage {
				continue
			}
			remaining, pageComplete, err := c.remainingPromotionAssociations(ctx, commit.ID, connection.PageInfo.EndCursor, len(connection.Nodes))
			if err != nil {
				return nil, false, err
			}
			associations = append(associations, remaining...)
			complete = complete && pageComplete
		}
		if len(seen) != len(requested) {
			complete = false
		}
	}
	return associations, complete, nil
}

func (c *Client) remainingPromotionAssociations(ctx context.Context, commitID, cursor string, count int) ([]promotionPullRequestNode, bool, error) {
	result := []promotionPullRequestNode{}
	for count < PromotionAssociationCap {
		pageSize := min(100, PromotionAssociationCap-count)
		var response promotionCommitNodeResponse
		headers, err := c.postGraphQLContext(ctx, graphQLPayload{
			Query: `query($id: ID!, $cursor: String!, $first: Int!) {
  node(id: $id) {
    ... on Commit {
      id
      associatedPullRequests(first: $first, after: $cursor) {
        nodes { number title body mergedAt baseRefName headRefName baseRepository { nameWithOwner } }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`,
			Variables: map[string]any{"id": commitID, "cursor": cursor, "first": pageSize},
		}, &response)
		if err != nil {
			return nil, false, err
		}
		if len(response.Errors) > 0 {
			return nil, false, graphQLResponseError(response.Errors, headers)
		}
		if response.Data.Node == nil || response.Data.Node.ID == "" {
			return result, false, nil
		}
		connection := response.Data.Node.AssociatedPullRequests
		result = append(result, connection.Nodes...)
		count += len(connection.Nodes)
		if !connection.PageInfo.HasNextPage {
			return result, true, nil
		}
		if len(connection.Nodes) == 0 || connection.PageInfo.EndCursor == cursor {
			return result, false, nil
		}
		cursor = connection.PageInfo.EndCursor
	}
	return result, false, nil
}

func isPromotionWorkPullRequest(pullRequest promotionPullRequestNode, repo, stagingBranch, workBranchPrefix string) (bool, bool) {
	if pullRequest.MergedAt == nil || pullRequest.BaseRefName != stagingBranch {
		return false, true
	}
	if pullRequest.BaseRepository == nil {
		return false, false
	}
	if !strings.EqualFold(pullRequest.BaseRepository.NameWithOwner, repo) {
		return false, true
	}
	if pullRequest.Number <= 0 {
		return false, false
	}
	if workBranchPrefix == "" {
		return true, true
	}
	if pullRequest.HeadRefName == "" {
		return false, false
	}
	return strings.HasPrefix(pullRequest.HeadRefName, workBranchPrefix), true
}

type promotionPullRequestNode struct {
	Number         int     `json:"number"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	MergedAt       *string `json:"mergedAt"`
	BaseRefName    string  `json:"baseRefName"`
	HeadRefName    string  `json:"headRefName"`
	BaseRepository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"baseRepository"`
}

type promotionPullRequestConnection struct {
	Nodes    []promotionPullRequestNode `json:"nodes"`
	PageInfo pageInfo                   `json:"pageInfo"`
}

type promotionCommitNode struct {
	ID                     string                         `json:"id"`
	AssociatedPullRequests promotionPullRequestConnection `json:"associatedPullRequests"`
}

type promotionCommitNodesResponse struct {
	Data struct {
		Nodes []*promotionCommitNode `json:"nodes"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type promotionCommitNodeResponse struct {
	Data struct {
		Node *promotionCommitNode `json:"node"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}
