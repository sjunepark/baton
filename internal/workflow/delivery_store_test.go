package workflow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
)

type concurrentDeliveryReader struct {
	active atomic.Int32
	max    atomic.Int32
}

func (*concurrentDeliveryReader) GetRepositoryIdentityContext(context.Context, string) (gh.RepositoryIdentity, error) {
	return gh.RepositoryIdentity{}, nil
}

func (*concurrentDeliveryReader) GetIssueContext(context.Context, string, int) (gh.Issue, error) {
	return gh.Issue{}, nil
}

func (reader *concurrentDeliveryReader) GetIssueCommentContext(_ context.Context, _ string, id int64) (gh.IssueComment, error) {
	active := reader.active.Add(1)
	for current := reader.max.Load(); active > current && !reader.max.CompareAndSwap(current, active); current = reader.max.Load() {
	}
	time.Sleep(2 * time.Millisecond)
	reader.active.Add(-1)
	return gh.IssueComment{
		ID: id, NodeID: fmt.Sprintf("IC_%d", id), IssueURL: "https://api.github.com/repos/example/repo/issues/900",
		Body: "record", Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType},
	}, nil
}

func TestAcquireDeliveryRecordCommentsUsesBoundedConcurrency(t *testing.T) {
	references := make([]delivery.RecordReference, deliveryRecordReadConcurrency*2)
	for index := range references {
		references[index].Comment = delivery.CommentIdentity{DatabaseID: int64(index + 1), NodeID: fmt.Sprintf("IC_%d", index+1)}
	}
	reader := &concurrentDeliveryReader{}
	stored, err := acquireDeliveryRecordComments(t.Context(), reader, "example/repo", 900, references)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(references) || reader.max.Load() <= 1 || reader.max.Load() > deliveryRecordReadConcurrency {
		t.Fatalf("records=%d max concurrency=%d", len(stored), reader.max.Load())
	}
	for index := range stored {
		if stored[index].Comment.DatabaseID != int64(index+1) {
			t.Fatalf("record order changed at %d: %+v", index, stored[index].Comment)
		}
	}
}
