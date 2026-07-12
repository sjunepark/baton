package workitem

import (
	"strings"

	"github.com/sjunepark/baton/internal/config"
)

type State string

const (
	StateReady            State = "ready"
	StateActiveWorkPR     State = "active_work_pr"
	StateAwaitingReview   State = "awaiting_review"
	StateBlocked          State = "blocked"
	StatePromotedOrClosed State = "promoted_or_closed"
)

type IssueFacts struct {
	Open          bool
	Labels        []string
	LinkedWorkPRs []int
	MergedWorkPRs []int
}

type Readiness struct {
	State    State
	Eligible bool
	Action   string
	Reasons  []string
}

func ClassifyIssue(facts IssueFacts, policy config.IssuePolicy) Readiness {
	if !facts.Open {
		return Readiness{State: StatePromotedOrClosed, Reasons: []string{"issue is closed"}}
	}
	labels := normalizedSet(facts.Labels)
	action := ""
	switch {
	case hasAny(labels, policy.ImplementationLabels):
		action = "issue-implementation"
	case hasAny(labels, policy.CommentOnlyLabels):
		action = "issue-investigation"
	}
	if _, awaiting := labels[normalize(policy.AwaitingReviewLabel)]; awaiting || len(facts.MergedWorkPRs) > 0 {
		reason := "awaiting review on staging"
		if !awaiting {
			reason = "merged work PR awaiting review on staging"
		}
		return Readiness{State: StateAwaitingReview, Action: action, Reasons: []string{reason}}
	}
	reasons := []string{}
	if action == "" {
		reasons = append(reasons, "missing implementation or investigation label")
	}
	for _, skip := range policy.SkipLabels {
		if _, blocked := labels[normalize(skip)]; blocked {
			reasons = append(reasons, "skip label "+skip)
		}
	}
	if len(reasons) > 0 {
		return Readiness{State: StateBlocked, Action: action, Reasons: reasons}
	}
	if action == "issue-implementation" && len(facts.LinkedWorkPRs) > 0 {
		return Readiness{State: StateActiveWorkPR, Action: action, Reasons: []string{"active linked work PR"}}
	}
	return Readiness{State: StateReady, Eligible: true, Action: action, Reasons: []string{"eligible"}}
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[normalize(value)] = struct{}{}
	}
	return result
}

func hasAny(values map[string]struct{}, candidates []string) bool {
	for _, candidate := range candidates {
		if _, exists := values[normalize(candidate)]; exists {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
