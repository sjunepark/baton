package queue

import (
	"fmt"
	"sort"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/policy"
)

type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Body   string   `json:"-"`
	Labels []string `json:"labels"`
}

type PullRequest struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Body       string `json:"-"`
	BaseRef    string `json:"baseRef"`
	HeadRef    string `json:"headRef"`
	HeadSHA    string `json:"headSha"`
	CheckState string `json:"checkState,omitempty"`
}

type Snapshot struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Repo          string         `json:"repo"`
	Counts        SnapshotCounts `json:"counts"`
	BranchHealth  *BranchHealth  `json:"branchHealth,omitempty"`
	Issues        []IssueState   `json:"issues"`
	PullRequests  []PullState    `json:"pullRequests"`
	Help          []string       `json:"help,omitempty"`
}

type SnapshotCounts struct {
	TotalIssues       int    `json:"totalIssues"`
	EligibleIssues    int    `json:"eligibleIssues"`
	SkippedIssues     int    `json:"skippedIssues"`
	OpenPullRequests  int    `json:"openPullRequests"`
	BranchHealthState string `json:"branchHealthState,omitempty"`
}

type BranchHealth struct {
	Ref        string `json:"ref"`
	SHA        string `json:"sha"`
	CheckState string `json:"checkState"`
}

type IssueState struct {
	Issue     Issue    `json:"issue"`
	Eligible  bool     `json:"eligible"`
	Action    string   `json:"action,omitempty"`
	Reasons   []string `json:"reasons"`
	LinkedPRs []int    `json:"linkedPrs"`
}

type PullState struct {
	PullRequest      PullRequest `json:"pullRequest"`
	ReferencedIssues []int       `json:"referencedIssues"`
}

type NextCandidates struct {
	SchemaVersion     int             `json:"schemaVersion"`
	Kind              string          `json:"kind"`
	Action            string          `json:"action"`
	Repo              string          `json:"repo"`
	Reason            string          `json:"reason"`
	SelectionRequired bool            `json:"selectionRequired"`
	Candidates        []NextCandidate `json:"candidates"`
	BlockedItems      []string        `json:"blockedItems"`
	Instructions      []string        `json:"instructions"`
}

type NextCandidate struct {
	Type       string `json:"type"`
	Number     int    `json:"number,omitempty"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
	HeadRef    string `json:"headRef,omitempty"`
	BaseRef    string `json:"baseRef,omitempty"`
	Ref        string `json:"ref,omitempty"`
	SHA        string `json:"sha,omitempty"`
	CheckState string `json:"checkState,omitempty"`
}

func BuildSnapshot(repo string, cfg config.Config, issues []Issue, prs []PullRequest) Snapshot {
	return BuildSnapshotWithBranchHealth(repo, cfg, issues, prs, nil)
}

func BuildSnapshotWithBranchHealth(repo string, cfg config.Config, issues []Issue, prs []PullRequest, branchHealth *BranchHealth) Snapshot {
	prStates := make([]PullState, 0, len(prs))
	prsByIssue := map[int][]int{}
	for _, pr := range prs {
		referenced := referencedIssues(pr)
		for _, issueNumber := range referenced {
			prsByIssue[issueNumber] = append(prsByIssue[issueNumber], pr.Number)
		}
		prStates = append(prStates, PullState{PullRequest: pr, ReferencedIssues: referenced})
	}

	issueStates := make([]IssueState, 0, len(issues))
	eligibleIssues := 0
	for _, issue := range issues {
		linkedPRs := prsByIssue[issue.Number]
		if linkedPRs == nil {
			linkedPRs = []int{}
		}
		state := IssueState{Issue: issue, Eligible: true, Reasons: []string{}, LinkedPRs: linkedPRs}
		labels := stringSet(issue.Labels)
		if hasAny(labels, cfg.IssuePolicy.ImplementationLabels) {
			state.Action = "issue-implementation"
		} else if hasAny(labels, cfg.IssuePolicy.CommentOnlyLabels) {
			state.Action = "issue-investigation"
		} else {
			state.Eligible = false
			state.Reasons = append(state.Reasons, "missing implementation or investigation label")
		}
		for _, skip := range cfg.IssuePolicy.SkipLabels {
			if _, ok := labels[skip]; ok {
				state.Eligible = false
				state.Reasons = append(state.Reasons, "skip label "+skip)
			}
		}
		if len(state.LinkedPRs) > 0 && state.Action == "issue-implementation" {
			state.Eligible = false
			state.Reasons = append(state.Reasons, "active linked PR")
		}
		if len(state.Reasons) == 0 {
			state.Reasons = append(state.Reasons, "eligible")
		}
		if state.Eligible {
			eligibleIssues++
		}
		issueStates = append(issueStates, state)
	}

	counts := SnapshotCounts{
		TotalIssues:      len(issueStates),
		EligibleIssues:   eligibleIssues,
		SkippedIssues:    len(issueStates) - eligibleIssues,
		OpenPullRequests: len(prStates),
	}
	if branchHealth != nil {
		counts.BranchHealthState = branchHealth.CheckState
	}
	return Snapshot{
		SchemaVersion: 1,
		Kind:          "queueSnapshot",
		Repo:          repo,
		Counts:        counts,
		BranchHealth:  branchHealth,
		Issues:        issueStates,
		PullRequests:  prStates,
		Help:          snapshotHelp(issueStates, prStates),
	}
}

func RecommendNext(snapshot Snapshot) NextCandidates {
	if snapshot.BranchHealth != nil && (snapshot.BranchHealth.CheckState == "failure" || snapshot.BranchHealth.CheckState == "pending") {
		return nextCandidates(snapshot.Repo, "branch-health", snapshot.BranchHealth.CheckState+"-staging-branch",
			[]NextCandidate{{
				Type:       "branch",
				Ref:        snapshot.BranchHealth.Ref,
				SHA:        snapshot.BranchHealth.SHA,
				CheckState: snapshot.BranchHealth.CheckState,
			}},
			[]string{"Work in a caller-provided isolated checkout.", "Fix the shared staging branch before starting new issue work.", "Do not open unrelated issue PRs until branch health is clear."},
		)
	}

	for _, tier := range []string{"failing-checks", "pending-checks", "ready-for-review", "open-pr"} {
		candidates := []NextCandidate{}
		for _, pr := range snapshot.PullRequests {
			if prFollowupReason(pr.PullRequest.CheckState) != tier {
				continue
			}
			candidates = append(candidates, NextCandidate{
				Type:    "pullRequest",
				Number:  pr.PullRequest.Number,
				Title:   pr.PullRequest.Title,
				URL:     pr.PullRequest.URL,
				HeadRef: pr.PullRequest.HeadRef,
				BaseRef: pr.PullRequest.BaseRef,
			})
		}
		if len(candidates) > 0 {
			sortCandidates(candidates)
			return nextCandidates(snapshot.Repo, "pr-followup", tier, candidates,
				[]string{"Choose exactly one candidate.", "Work in a caller-provided isolated checkout.", "Push to the existing PR branch.", "Do not open a new PR."},
			)
		}
	}

	implementationCandidates := issueCandidates(snapshot.Issues, "issue-implementation")
	if len(implementationCandidates) > 0 {
		sortCandidates(implementationCandidates)
		return nextCandidates(snapshot.Repo, "issue-implementation", "eligible-issue", implementationCandidates,
			[]string{"Choose exactly one candidate.", "Work in a caller-provided isolated checkout.", "Open a PR to the staging branch with Refs #<issue-number>.", "Do not merge."},
		)
	}

	investigationCandidates := issueCandidates(snapshot.Issues, "issue-investigation")
	if len(investigationCandidates) > 0 {
		sortCandidates(investigationCandidates)
		return nextCandidates(snapshot.Repo, "issue-investigation", "eligible-investigation", investigationCandidates,
			[]string{"Choose exactly one candidate.", "Do not edit files unless the user explicitly changes scope.", "Inspect and comment with findings, evidence, and a recommended next label."},
		)
	}

	return nextCandidates(snapshot.Repo, "none", "no eligible issue or PR follow-up", []NextCandidate{}, []string{})
}

func prFollowupReason(state string) string {
	switch state {
	case "failure":
		return "failing-checks"
	case "pending":
		return "pending-checks"
	case "success":
		return "ready-for-review"
	default:
		return "open-pr"
	}
}

func issueCandidates(issues []IssueState, action string) []NextCandidate {
	candidates := []NextCandidate{}
	for _, issue := range issues {
		if !issue.Eligible {
			continue
		}
		issueAction := issue.Action
		if issueAction == "" {
			issueAction = "issue-implementation"
		}
		if issueAction != action {
			continue
		}
		candidates = append(candidates, NextCandidate{
			Type:   "issue",
			Number: issue.Issue.Number,
			Title:  issue.Issue.Title,
			URL:    issue.Issue.URL,
		})
	}
	return candidates
}

func nextCandidates(repo, action, reason string, candidates []NextCandidate, instructions []string) NextCandidates {
	return NextCandidates{
		SchemaVersion:     2,
		Kind:              "nextCandidates",
		Action:            action,
		Repo:              repo,
		Reason:            reason,
		SelectionRequired: len(candidates) > 1,
		Candidates:        candidates,
		BlockedItems:      []string{},
		Instructions:      instructions,
	}
}

func sortCandidates(candidates []NextCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Number < candidates[j].Number
	})
}

func referencedIssues(pr PullRequest) []int {
	values := append(policy.ExtractReferenceIssueNumbers(pr.Title), policy.ExtractReferenceIssueNumbers(pr.Body)...)
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func hasAny(labels map[string]struct{}, candidates []string) bool {
	for _, label := range candidates {
		if _, ok := labels[label]; ok {
			return true
		}
	}
	return false
}

func snapshotHelp(issues []IssueState, prs []PullState) []string {
	help := []string{"Run `baton next --json`."}
	if len(prs) > 0 {
		help = append(help, "Run `baton pr <number> --json` or `baton checks <number> --json` for PR details.")
	}
	for _, issue := range issues {
		if issue.Eligible {
			help = append(help, fmt.Sprintf("Prepare an isolated checkout, then create a work branch for issue %d from the configured staging branch.", issue.Issue.Number))
			return help
		}
	}
	if len(prs) == 0 {
		help = append(help, "Run `baton doctor --json` if no eligible work appears.")
	}
	return help
}
