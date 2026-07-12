package snapshot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/queue"
)

type Outcome string

const (
	OutcomeActionable          Outcome = "actionable"
	OutcomeHumanChoiceRequired Outcome = "human_choice_required"
	OutcomeWaiting             Outcome = "waiting"
	OutcomeBlocked             Outcome = "blocked"
	OutcomeIdle                Outcome = "idle"
	OutcomeDegraded            Outcome = "degraded"
)

type Action string

const (
	ActionIssueImplementation Action = "issue_implementation"
	ActionIssueInvestigation  Action = "issue_investigation"
	ActionPullRequestFollowUp Action = "pull_request_follow_up"
	ActionBranchHealth        Action = "branch_health"
)

type CandidateKind string

const (
	CandidateIssue       CandidateKind = "issue"
	CandidatePullRequest CandidateKind = "pull_request"
	CandidateBranch      CandidateKind = "branch"
)

type RepositorySnapshot struct {
	SchemaVersion  int                      `json:"schemaVersion"`
	Kind           string                   `json:"kind"`
	Repository     string                   `json:"repository"`
	Acquisition    AcquisitionWindow        `json:"acquisition"`
	Completeness   Completeness             `json:"completeness"`
	Warnings       []Warning                `json:"warnings"`
	Queue          queue.Snapshot           `json:"queue"`
	Branches       []BranchObservation      `json:"branches"`
	PullRequests   []PullRequestObservation `json:"pullRequests"`
	Recommendation Recommendation           `json:"recommendation"`
	legacyNext     queue.NextCandidates
}

type AcquisitionWindow struct {
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

type Recommendation struct {
	Outcome            Outcome     `json:"outcome"`
	Action             *Action     `json:"action,omitempty"`
	Reasons            []string    `json:"reasons"`
	SelectionRequired  bool        `json:"selectionRequired"`
	Candidates         []Candidate `json:"candidates"`
	DeferredCandidates []Candidate `json:"deferredCandidates"`
	Instructions       []string    `json:"instructions"`
}

type Candidate struct {
	Identity CandidateIdentity `json:"identity"`
	Title    string            `json:"title,omitempty"`
	URL      string            `json:"url,omitempty"`
	State    string            `json:"state"`
	Reasons  []string          `json:"reasons"`
}

type CandidateIdentity struct {
	Repository string        `json:"repository"`
	Kind       CandidateKind `json:"kind"`
	Number     int           `json:"number,omitempty"`
	Ref        string        `json:"ref,omitempty"`
	BaseRef    string        `json:"baseRef,omitempty"`
	HeadRef    string        `json:"headRef,omitempty"`
	SHA        string        `json:"sha,omitempty"`
	BaseSHA    string        `json:"baseSha,omitempty"`
	HeadSHA    string        `json:"headSha,omitempty"`
}

type BranchObservation struct {
	Identity          CandidateIdentity    `json:"identity"`
	Protected         bool                 `json:"protected"`
	CheckState        string               `json:"checkState"`
	ChecksComplete    bool                 `json:"checksComplete"`
	RequiredChecks    []RequiredCheckState `json:"requiredChecks"`
	RequiredApprovals int                  `json:"requiredApprovals"`
}

type PullRequestObservation struct {
	Identity      CandidateIdentity   `json:"identity"`
	Title         string              `json:"title"`
	URL           string              `json:"url"`
	Draft         bool                `json:"draft"`
	Mergeable     string              `json:"mergeable"`
	MergeState    string              `json:"mergeState"`
	Checks        CheckObservation    `json:"checks"`
	Review        ReviewObservation   `json:"review"`
	ReviewThreads ReviewThreadSummary `json:"reviewThreads"`
}

type CheckObservation struct {
	State                string               `json:"state"`
	Complete             bool                 `json:"complete"`
	StrictRequiredChecks bool                 `json:"strictRequiredChecks"`
	Summary              CheckSummary         `json:"summary"`
	Required             []RequiredCheckState `json:"required"`
}

type CheckSummary struct {
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Skipped   int `json:"skipped"`
	Cancelled int `json:"cancelled"`
	Unknown   int `json:"unknown"`
}

type ReviewThreadSummary struct {
	Total             int `json:"total"`
	Unresolved        int `json:"unresolved"`
	HumanUnresolved   int `json:"humanUnresolved"`
	BotUnresolved     int `json:"botUnresolved"`
	UnknownUnresolved int `json:"unknownUnresolved"`
	Outdated          int `json:"outdated"`
}

type ReviewObservation struct {
	State             string `json:"state"`
	RequiredApprovals int    `json:"requiredApprovals"`
	Approvals         int    `json:"approvals"`
	ChangesRequested  int    `json:"changesRequested"`
	RequestedUsers    int    `json:"requestedUsers"`
	RequestedTeams    int    `json:"requestedTeams"`
}

type BuildInput struct {
	Facts           Acquisition
	Queue           queue.Snapshot
	StartedAt       time.Time
	CompletedAt     time.Time
	RequestedAction string
}

func Build(input BuildInput) (RepositorySnapshot, error) {
	if strings.TrimSpace(input.Facts.Repository) == "" {
		return RepositorySnapshot{}, fmt.Errorf("repository snapshot requires a repository identity")
	}
	if input.Queue.Repo != input.Facts.Repository || input.Queue.Kind != "queueSnapshot" || input.Queue.SchemaVersion != 1 {
		return RepositorySnapshot{}, fmt.Errorf("repository snapshot queue projection does not match its repository facts")
	}
	if input.StartedAt.IsZero() || input.CompletedAt.IsZero() || input.CompletedAt.Before(input.StartedAt) {
		return RepositorySnapshot{}, fmt.Errorf("repository snapshot requires an ordered acquisition window")
	}
	input.StartedAt = input.StartedAt.UTC()
	input.CompletedAt = input.CompletedAt.UTC()
	warnings := append([]Warning{}, input.Facts.Warnings...)
	completeness := input.Facts.Completeness
	for _, branch := range input.Facts.Branches {
		warnings = append(warnings, branch.Warnings...)
		if branch.Completeness == Degraded || len(branch.Warnings) > 0 {
			completeness = Degraded
		}
	}
	for _, pullRequest := range input.Facts.PullRequests {
		warnings = append(warnings, pullRequest.Warnings...)
		if pullRequest.Completeness == Degraded || len(pullRequest.Warnings) > 0 {
			completeness = Degraded
		}
		for _, required := range pullRequest.RequiredChecks {
			if required.State == "unknown" {
				warnings = append(warnings, Warning{Code: "unknown", Scope: fmt.Sprintf("pullRequest:%d:requiredChecks", pullRequest.PullRequest.Number), Message: "required check result is unknown"})
				completeness = Degraded
			}
		}
	}
	warnings = deduplicateWarnings(warnings)
	sortWarnings(warnings)
	if completeness == Degraded && len(warnings) == 0 {
		return RepositorySnapshot{}, fmt.Errorf("degraded repository snapshot requires at least one warning")
	}

	result := RepositorySnapshot{
		SchemaVersion: 1,
		Kind:          "repositorySnapshot",
		Repository:    input.Facts.Repository,
		Acquisition:   AcquisitionWindow{StartedAt: input.StartedAt, CompletedAt: input.CompletedAt},
		Completeness:  completeness,
		Warnings:      warnings,
		Queue:         input.Queue,
		Branches:      branchObservations(input.Facts),
		PullRequests:  pullRequestObservations(input.Facts),
		legacyNext:    legacyRecommendation(input.Queue, input.RequestedAction),
	}
	result.Recommendation = recommend(result, input.RequestedAction)
	normalizeRecommendation(&result.Recommendation)
	if err := validateRecommendation(result.Recommendation, result.Completeness); err != nil {
		return RepositorySnapshot{}, err
	}
	return result, nil
}

func normalizeRecommendation(recommendation *Recommendation) {
	if recommendation.Reasons == nil {
		recommendation.Reasons = []string{}
	}
	if recommendation.Candidates == nil {
		recommendation.Candidates = []Candidate{}
	}
	if recommendation.DeferredCandidates == nil {
		recommendation.DeferredCandidates = []Candidate{}
	}
	if recommendation.Instructions == nil {
		recommendation.Instructions = []string{}
	}
}

func validateRecommendation(recommendation Recommendation, completeness Completeness) error {
	switch recommendation.Outcome {
	case OutcomeActionable:
		if recommendation.Action == nil || len(recommendation.Candidates) != 1 || recommendation.SelectionRequired {
			return fmt.Errorf("actionable recommendation requires one selected candidate and an action")
		}
	case OutcomeHumanChoiceRequired:
		if len(recommendation.Candidates) == 0 || recommendation.SelectionRequired != (len(recommendation.Candidates) > 1) {
			return fmt.Errorf("human-choice recommendation requires candidates and explicit selection only for multiple candidates")
		}
	case OutcomeWaiting, OutcomeBlocked, OutcomeIdle, OutcomeDegraded:
		if recommendation.Action != nil {
			return fmt.Errorf("%s recommendation cannot imply an action", recommendation.Outcome)
		}
	default:
		return fmt.Errorf("repository snapshot has unsupported recommendation outcome %q", recommendation.Outcome)
	}
	if completeness == Degraded && recommendation.Outcome != OutcomeDegraded {
		return fmt.Errorf("degraded repository snapshot requires a degraded recommendation")
	}
	if recommendation.Outcome == OutcomeDegraded && completeness != Degraded {
		return fmt.Errorf("degraded recommendation requires degraded snapshot completeness")
	}
	return nil
}

func (result RepositorySnapshot) QueueV1() queue.Snapshot {
	return result.Queue
}

func (result RepositorySnapshot) NextV2() queue.NextCandidates {
	return result.legacyNext
}

func legacyRecommendation(queueSnapshot queue.Snapshot, requestedAction string) queue.NextCandidates {
	if requestedAction == "issue-investigation" {
		return queue.RecommendNextInvestigation(queueSnapshot)
	}
	return queue.RecommendNext(queueSnapshot)
}

func branchObservations(facts Acquisition) []BranchObservation {
	result := make([]BranchObservation, 0, len(facts.Branches))
	for _, branch := range facts.Branches {
		required := RequiredCheckStates(branch.Checks.Checks, branch.Rules.RequiredChecks)
		result = append(result, BranchObservation{
			Identity:  CandidateIdentity{Repository: facts.Repository, Kind: CandidateBranch, Ref: branch.Branch.Ref, SHA: branch.Branch.SHA},
			Protected: branch.Branch.Protected, CheckState: branch.Checks.State, ChecksComplete: branch.Checks.Complete,
			RequiredChecks: required, RequiredApprovals: branch.Rules.RequiredApprovingReviewCount,
		})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Identity.Ref < result[j].Identity.Ref })
	return result
}

func pullRequestObservations(facts Acquisition) []PullRequestObservation {
	result := make([]PullRequestObservation, 0, len(facts.PullRequests))
	for _, pullRequest := range facts.PullRequests {
		pr := pullRequest.PullRequest
		result = append(result, PullRequestObservation{
			Identity: CandidateIdentity{Repository: facts.Repository, Kind: CandidatePullRequest, Number: pr.Number, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef, BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA},
			Title:    pr.Title, URL: pr.URL, Draft: pr.Draft, Mergeable: pr.Mergeable, MergeState: pr.MergeState,
			Checks: CheckObservation{
				State: pullRequest.Checks.State, Complete: pullRequest.Checks.Complete, StrictRequiredChecks: pullRequest.Rules.StrictRequiredChecks,
				Summary:  CheckSummary{Passed: pullRequest.Checks.Summary.Passed, Failed: pullRequest.Checks.Summary.Failed, Pending: pullRequest.Checks.Summary.Pending, Skipped: pullRequest.Checks.Summary.Skipped, Cancelled: pullRequest.Checks.Summary.Cancelled, Unknown: pullRequest.Checks.Summary.Unknown},
				Required: append([]RequiredCheckState{}, pullRequest.RequiredChecks...),
			},
			Review:        ReviewObservation{State: pullRequest.Review.State, RequiredApprovals: pullRequest.Review.RequiredApprovals, Approvals: pullRequest.Review.Approvals, ChangesRequested: pullRequest.Review.ChangesRequested, RequestedUsers: pullRequest.Review.RequestedUsers, RequestedTeams: pullRequest.Review.RequestedTeams},
			ReviewThreads: ReviewThreadSummary{Total: pullRequest.ReviewThreads.Summary.Total, Unresolved: pullRequest.ReviewThreads.Summary.Unresolved, HumanUnresolved: pullRequest.ReviewThreads.Summary.HumanUnresolved, BotUnresolved: pullRequest.ReviewThreads.Summary.BotUnresolved, UnknownUnresolved: pullRequest.ReviewThreads.Summary.UnknownUnresolved, Outdated: pullRequest.ReviewThreads.Summary.Outdated},
		})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Identity.Number < result[j].Identity.Number })
	return result
}

func sortWarnings(warnings []Warning) {
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Scope != warnings[j].Scope {
			return warnings[i].Scope < warnings[j].Scope
		}
		return warnings[i].Code < warnings[j].Code
	})
}

func deduplicateWarnings(warnings []Warning) []Warning {
	result := make([]Warning, 0, len(warnings))
	seen := map[string]struct{}{}
	for _, warning := range warnings {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%t\x00%d\x00%s", warning.Code, warning.Scope, warning.Message, warning.Retryable, warning.HTTPStatus, warning.RequestID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, warning)
	}
	return result
}
