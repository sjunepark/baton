package workflow

import (
	"context"
	"testing"
	"time"
)

func TestBoundedContextCapsLongParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer parentCancel()
	ctx, cancel := boundedContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("bounded context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > defaultOperationTimeout || remaining < defaultOperationTimeout-time.Second {
		t.Fatalf("remaining deadline = %s", remaining)
	}
}
