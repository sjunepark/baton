package apperror

import (
	"errors"
	"time"

	"github.com/sjunepark/baton/internal/operation"
)

type Category string

const (
	Policy   Category = "policy"
	Usage    Category = "usage"
	Config   Category = "config"
	Auth     Category = "auth"
	GitHub   Category = "github"
	LocalGit Category = "localGit"
)

type Error struct {
	Category   Category
	Message    string
	Hint       string
	Retryable  bool
	HTTPStatus int
	RequestID  string
	RetryAfter time.Duration
	Details    map[string]string
	Report     *operation.Report
	Cause      error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "command failed"
}

func (e *Error) Unwrap() error { return e.Cause }

func New(category Category, message, hint string) *Error {
	return &Error{Category: category, Message: message, Hint: hint}
}

func Wrap(category Category, message string, err error, hint string) *Error {
	if err == nil {
		err = errors.New("command failed")
	}
	return &Error{Category: category, Message: message, Hint: hint, Cause: err}
}

func As(err error) *Error {
	var applicationError *Error
	if errors.As(err, &applicationError) {
		return applicationError
	}
	return nil
}

// WithReport preserves the mutation outcome on the single structured error
// returned by a failed or partially completed workflow.
func WithReport(err error, report operation.Report) error {
	if err == nil {
		return nil
	}
	if applicationError := As(err); applicationError != nil {
		copy := *applicationError
		copy.Report = &report
		return &copy
	}
	applicationError := Wrap(Usage, err.Error(), err, "")
	applicationError.Report = &report
	return applicationError
}

func (e *Error) ExitCode() int {
	switch e.Category {
	case Policy:
		return 1
	case Usage:
		return 2
	case Config:
		return 3
	case Auth:
		return 4
	case GitHub:
		return 5
	case LocalGit:
		return 6
	default:
		return 2
	}
}

type UpstreamMetadata interface {
	UpstreamHTTPStatus() int
	UpstreamRequestID() string
	UpstreamRetryAfter() time.Duration
	UpstreamDetails() map[string]string
}

func WrapUpstream(err error) *Error {
	applicationError := Wrap(GitHub, "GitHub request failed", err, "")
	var metadata UpstreamMetadata
	if errors.As(err, &metadata) {
		applicationError.HTTPStatus = metadata.UpstreamHTTPStatus()
		applicationError.RequestID = metadata.UpstreamRequestID()
		applicationError.RetryAfter = metadata.UpstreamRetryAfter()
		applicationError.Details = metadata.UpstreamDetails()
		applicationError.Retryable = applicationError.HTTPStatus == 429 || applicationError.HTTPStatus >= 500 || applicationError.RetryAfter > 0
	}
	var retryable interface{ UpstreamRetryable() bool }
	if errors.As(err, &retryable) {
		applicationError.Retryable = retryable.UpstreamRetryable()
	}
	var safe interface{ SafeMessage() string }
	if errors.As(err, &safe) && safe.SafeMessage() != "" {
		applicationError.Message = safe.SafeMessage()
	}
	return applicationError
}
