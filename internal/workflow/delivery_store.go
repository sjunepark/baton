package workflow

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
)

type deliveryStoreReader interface {
	GetRepositoryIdentityContext(context.Context, string) (gh.RepositoryIdentity, error)
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	GetIssueCommentContext(context.Context, string, int64) (gh.IssueComment, error)
}

type acquiredDeliveryStore struct {
	Snapshot          delivery.Snapshot
	CheckpointComment gh.IssueComment
	LedgerIssue       gh.Issue
}

func deliveryLocatorFromConfig(cfg config.Config) (delivery.DeliveryStoreLocator, error) {
	if cfg.Delivery == nil {
		return delivery.DeliveryStoreLocator{}, fmt.Errorf("repository policy has no pinned delivery locator")
	}
	value := cfg.Delivery
	return delivery.DeliveryStoreLocator{
		Repository: delivery.RepositoryIdentity{Host: value.Host, FullName: value.Repository.FullName, NodeID: value.Repository.NodeID},
		Issue:      delivery.ResourceIdentity{Number: value.Issue.Number, NodeID: value.Issue.NodeID},
		Checkpoint: delivery.CommentIdentity{DatabaseID: value.Checkpoint.DatabaseID, NodeID: value.Checkpoint.NodeID},
	}, nil
}

func acquireDeliveryStore(ctx context.Context, client deliveryStoreReader, repo string, locator delivery.DeliveryStoreLocator) (acquiredDeliveryStore, error) {
	repository, err := client.GetRepositoryIdentityContext(ctx, repo)
	if err != nil {
		return acquiredDeliveryStore{}, err
	}
	if !strings.EqualFold(repository.Host, locator.Repository.Host) || repository.FullName != locator.Repository.FullName || repository.NodeID != locator.Repository.NodeID {
		return acquiredDeliveryStore{}, fmt.Errorf("pinned delivery repository identity is stale")
	}
	issue, err := client.GetIssueContext(ctx, repo, locator.Issue.Number)
	if err != nil {
		return acquiredDeliveryStore{}, err
	}
	if issue.PullRequest || issue.NodeID != locator.Issue.NodeID {
		return acquiredDeliveryStore{}, fmt.Errorf("pinned delivery issue identity is stale")
	}
	if !issue.Locked {
		return acquiredDeliveryStore{}, fmt.Errorf("pinned delivery issue is not locked")
	}
	checkpointComment, err := client.GetIssueCommentContext(ctx, repo, locator.Checkpoint.DatabaseID)
	if err != nil {
		return acquiredDeliveryStore{}, err
	}
	if !deliveryCommentBelongsToIssue(checkpointComment, repo, locator.Issue.Number) {
		return acquiredDeliveryStore{}, fmt.Errorf("pinned delivery checkpoint does not belong to the pinned ledger issue")
	}
	checkpointStored := storedDeliveryComment(checkpointComment)
	checkpoint, err := delivery.ParseCheckpointIndex(locator, checkpointStored)
	if err != nil {
		return acquiredDeliveryStore{}, err
	}
	references := deliveryCheckpointReferences(checkpoint)
	stored := make([]delivery.StoredComment, 0, len(references))
	for _, reference := range references {
		comment, err := client.GetIssueCommentContext(ctx, repo, reference.Comment.DatabaseID)
		if err != nil {
			return acquiredDeliveryStore{}, err
		}
		if !deliveryCommentBelongsToIssue(comment, repo, locator.Issue.Number) {
			return acquiredDeliveryStore{}, fmt.Errorf("delivery record comment %d does not belong to the pinned ledger issue", comment.ID)
		}
		stored = append(stored, storedDeliveryComment(comment))
	}
	snapshot, err := delivery.ParseStoreSnapshot(delivery.StoreSnapshot{
		Locator: locator, Checkpoint: checkpointStored, Records: stored, Complete: true,
	})
	if err != nil {
		return acquiredDeliveryStore{}, err
	}
	return acquiredDeliveryStore{Snapshot: snapshot, CheckpointComment: checkpointComment, LedgerIssue: issue}, nil
}

func deliveryCommentBelongsToIssue(comment gh.IssueComment, repo string, issueNumber int) bool {
	parsed, err := url.Parse(comment.IssueURL)
	if err != nil || parsed.Path == "" {
		return false
	}
	want := "/repos/" + strings.Trim(repo, "/") + "/issues/" + strconv.Itoa(issueNumber)
	return strings.HasSuffix(strings.TrimSuffix(parsed.Path, "/"), want)
}

func validateTrustedDeliveryWrite(comment gh.IssueComment, repo string, issueNumber int) error {
	if comment.ID <= 0 || strings.TrimSpace(comment.NodeID) == "" {
		return fmt.Errorf("GitHub returned no stable delivery comment identity")
	}
	if !strings.EqualFold(strings.TrimSpace(comment.Author.Login), delivery.TrustedAuthorLogin) || !strings.EqualFold(strings.TrimSpace(comment.Author.Type), delivery.TrustedAuthorType) {
		return fmt.Errorf("delivery comment author is not the trusted GitHub Actions bot")
	}
	if !deliveryCommentBelongsToIssue(comment, repo, issueNumber) {
		return fmt.Errorf("delivery comment does not belong to the expected issue")
	}
	return nil
}

func storedDeliveryComment(comment gh.IssueComment) delivery.StoredComment {
	return delivery.StoredComment{
		Comment: delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID},
		Body:    comment.Body, AuthorLogin: comment.Author.Login, AuthorType: comment.Author.Type,
	}
}

func deliveryCheckpointReferences(checkpoint delivery.DeliveryCheckpoint) []delivery.RecordReference {
	values := append([]delivery.RecordReference(nil), checkpoint.ActiveRecords...)
	if checkpoint.CursorBoundary != nil {
		values = append(values, *checkpoint.CursorBoundary)
	}
	if checkpoint.BaseIntegration != nil {
		values = append(values, *checkpoint.BaseIntegration)
	}
	if checkpoint.PromotionIntegration != nil {
		values = append(values, *checkpoint.PromotionIntegration)
	}
	seen := map[int64]struct{}{}
	result := make([]delivery.RecordReference, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value.Comment.DatabaseID]; exists {
			continue
		}
		seen[value.Comment.DatabaseID] = struct{}{}
		result = append(result, value)
	}
	return result
}
