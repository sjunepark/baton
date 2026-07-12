package apperror

import (
	"errors"
	"testing"
	"time"

	"github.com/sjunepark/baton/internal/operation"
)

type upstreamError struct{}

func (upstreamError) Error() string                      { return "upstream failed" }
func (upstreamError) UpstreamHTTPStatus() int            { return 503 }
func (upstreamError) UpstreamRequestID() string          { return "request-1" }
func (upstreamError) UpstreamRetryAfter() time.Duration  { return 2 * time.Second }
func (upstreamError) UpstreamDetails() map[string]string { return map[string]string{"method": "GET"} }

func TestWrapUpstreamPreservesTypedMetadata(t *testing.T) {
	err := WrapUpstream(upstreamError{})
	if err.Category != GitHub || !err.Retryable || err.HTTPStatus != 503 || err.RequestID != "request-1" || err.RetryAfter != 2*time.Second {
		t.Fatalf("error = %+v", err)
	}
	if !errors.Is(err, upstreamError{}) {
		t.Fatal("wrapped cause was not preserved")
	}
}

func TestWithReportPreservesApplicationErrorAndDoesNotMutateIt(t *testing.T) {
	original := New(GitHub, "mutation failed", "retry")
	report := operation.NewReport([]operation.Result{{ID: "one", Status: operation.StatusFailed}})

	got := As(WithReport(original, report))
	if got == nil || got.Category != GitHub || got.Message != original.Message || got.Report == nil || got.Report.Status != operation.ReportFailed {
		t.Fatalf("WithReport() = %+v", got)
	}
	if original.Report != nil {
		t.Fatal("WithReport mutated the caller-owned application error")
	}
}

func TestErrorNeverFallsBackToUnsafeCause(t *testing.T) {
	err := &Error{Category: Auth, Cause: errors.New("token=super-secret")}
	if got := err.Error(); got != "command failed" {
		t.Fatalf("Error() = %q", got)
	}
}
