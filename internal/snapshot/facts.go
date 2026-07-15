package snapshot

import (
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
)

type Completeness string

const (
	Complete Completeness = "complete"
	Degraded Completeness = "degraded"
)

type Warning struct {
	Code       string `json:"code"`
	Scope      string `json:"scope"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
}

type Acquisition struct {
	Repository             string
	Completeness           Completeness
	Warnings               []Warning
	Issues                 []IssueFacts
	PullRequests           []PullRequestFacts
	MergedWorkPullRequests []queue.PullRequest
	Branches               []BranchFacts
	BaseIntegration        *delivery.BaseIntegrationFacts
}

type IssueFacts struct {
	Issue     gh.Issue
	Ownership policy.IssueOwnershipDecision
}

type BranchFacts struct {
	Branch       gh.Branch
	Rules        gh.BranchRules
	Checks       gh.CheckRollup
	Completeness Completeness
	Warnings     []Warning
}

type PullRequestFacts struct {
	PullRequest    gh.PullRequest
	Checks         gh.CheckRollup
	ReviewThreads  gh.ReviewThreadResult
	Reviews        []gh.PullRequestReview
	ReviewRequests []gh.ReviewRequest
	Rules          gh.BranchRules
	RequiredChecks []RequiredCheckState
	Review         ReviewState
	Completeness   Completeness
	Warnings       []Warning
}

type RequiredCheckState struct {
	Context       string `json:"context"`
	IntegrationID int64  `json:"integrationId,omitempty"`
	Found         bool   `json:"found"`
	State         string `json:"state"`
}

type ReviewState struct {
	State             string
	RequiredApprovals int
	Approvals         int
	ChangesRequested  int
	RequestedUsers    int
	RequestedTeams    int
}

func (facts *Acquisition) AddWarning(warning Warning) {
	facts.Warnings = append(facts.Warnings, warning)
	facts.Completeness = Degraded
}

func (facts *BranchFacts) AddWarning(warning Warning) {
	facts.Warnings = append(facts.Warnings, warning)
	facts.Completeness = Degraded
}

func (facts *PullRequestFacts) AddWarning(warning Warning) {
	facts.Warnings = append(facts.Warnings, warning)
	facts.Completeness = Degraded
}

func RequiredCheckStates(checks []gh.CheckState, required []gh.RequiredCheck) []RequiredCheckState {
	result := make([]RequiredCheckState, 0, len(required))
	for _, requirement := range deduplicateRequiredChecks(required) {
		state := RequiredCheckState{Context: requirement.Context, IntegrationID: requirement.IntegrationID, State: "missing"}
		for _, check := range checks {
			if check.Name != requirement.Context || (requirement.IntegrationID != 0 && check.AppID != requirement.IntegrationID) {
				continue
			}
			state.Found = true
			state.State = combineCheckResults(state.State, checkResult(check))
		}
		result = append(result, state)
	}
	return result
}

func MergeRequiredChecks(primary, legacy []gh.RequiredCheck) []gh.RequiredCheck {
	merged := deduplicateRequiredChecks(append(append([]gh.RequiredCheck(nil), primary...), legacy...))
	specificContexts := map[string]struct{}{}
	for _, check := range merged {
		if check.IntegrationID != 0 {
			specificContexts[strings.ToLower(check.Context)] = struct{}{}
		}
	}
	result := make([]gh.RequiredCheck, 0, len(merged))
	for _, check := range merged {
		if check.IntegrationID == 0 {
			if _, hasSpecificIdentity := specificContexts[strings.ToLower(check.Context)]; hasSpecificIdentity {
				continue
			}
		}
		result = append(result, check)
	}
	return result
}

func EvaluateReviews(headSHA string, rules gh.BranchRules, reviews []gh.PullRequestReview, requests []gh.ReviewRequest) ReviewState {
	state := ReviewState{RequiredApprovals: rules.RequiredApprovingReviewCount}
	latest := map[string]gh.PullRequestReview{}
	for _, review := range reviews {
		if review.Author.Type != "User" || review.Author.Login == "" {
			continue
		}
		switch strings.ToUpper(review.State) {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
		default:
			continue
		}
		if rules.DismissStaleReviews && review.CommitSHA != "" && review.CommitSHA != headSHA {
			continue
		}
		key := strings.ToLower(review.Author.Login)
		prior, exists := latest[key]
		if !exists || review.SubmittedAt.After(prior.SubmittedAt) || (review.SubmittedAt.Equal(prior.SubmittedAt) && review.ID > prior.ID) {
			latest[key] = review
		}
	}
	for _, review := range latest {
		switch strings.ToUpper(review.State) {
		case "APPROVED":
			state.Approvals++
		case "CHANGES_REQUESTED":
			state.ChangesRequested++
		}
	}
	for _, request := range requests {
		switch request.Kind {
		case "user":
			state.RequestedUsers++
		case "team":
			state.RequestedTeams++
		}
	}
	switch {
	case state.ChangesRequested > 0:
		state.State = "changes_requested"
	case state.Approvals < state.RequiredApprovals || state.RequestedUsers > 0 || state.RequestedTeams > 0:
		state.State = "review_required"
	default:
		state.State = "approved"
	}
	return state
}

func deduplicateRequiredChecks(checks []gh.RequiredCheck) []gh.RequiredCheck {
	result := make([]gh.RequiredCheck, 0, len(checks))
	seen := map[string]struct{}{}
	for _, check := range checks {
		if strings.TrimSpace(check.Context) == "" {
			continue
		}
		key := strings.ToLower(check.Context) + ":" + strconv.FormatInt(check.IntegrationID, 10)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, check)
	}
	return result
}

func checkResult(check gh.CheckState) string {
	switch {
	case check.Conclusion == "success" || check.Conclusion == "neutral" || check.Conclusion == "skipped" || check.Status == "success":
		return "passed"
	case check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "action_required" || check.Conclusion == "cancelled" || check.Conclusion == "startup_failure" || check.Conclusion == "stale" || check.Status == "failure" || check.Status == "error":
		return "failed"
	case check.Status == "queued" || check.Status == "in_progress" || check.Status == "pending" || check.Conclusion == "":
		return "pending"
	default:
		return "unknown"
	}
}

func combineCheckResults(current, next string) string {
	priority := map[string]int{"missing": 0, "passed": 1, "unknown": 2, "pending": 3, "failed": 4}
	if priority[next] > priority[current] {
		return next
	}
	return current
}
