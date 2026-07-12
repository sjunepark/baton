package workflow

import (
	"context"
	"time"
)

const defaultOperationTimeout = 30 * time.Second

func boundedContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, defaultOperationTimeout)
}
