package queue

import (
	"fmt"

	"github.com/sejunpark/baton/internal/config"
	"github.com/sejunpark/baton/internal/policy"
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
	SchemaVersion int           `json:"schemaVersion"`
	Kind          string        `json:"kind"`
	Repo          string        `json:"repo"`
	BranchHealth  *BranchHealth `json:"branchHealth,omitempty"`
	Issues        []IssueState  `json:"issues"`
	PullRequests  []PullState   `json:"pullRequests"`
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

type NextAction struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	Action        string    `json:"action"`
	Repo          string    `json:"repo"`
	Reason        string    `json:"reason"`
	PR            *PRRef    `json:"pr,omitempty"`
	Issue         *IssueRef `json:"issue,omitempty"`
	BlockedItems  []string  `json:"blockedItems"`
	Instructions  []string  `json:"instructions"`
}

type PRRef struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	HeadRef string `json:"headRef"`
	BaseRef string `json:"baseRef"`
}

type IssueRef struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
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
	for _, issue := range issues {
		state := IssueState{Issue: issue, Eligible: true, Reasons: []string{}, LinkedPRs: prsByIssue[issue.Number]}
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
		issueStates = append(issueStates, state)
	}

	return Snapshot{SchemaVersion: 1, Kind: "queueSnapshot", Repo: repo, BranchHealth: branchHealth, Issues: issueStates, PullRequests: prStates}
}

func RecommendNext(snapshot Snapshot) NextAction {
	if snapshot.BranchHealth != nil && (snapshot.BranchHealth.CheckState == "failure" || snapshot.BranchHealth.CheckState == "pending") {
		return NextAction{
			SchemaVersion: 1,
			Kind:          "nextAction",
			Action:        "branch-health",
			Repo:          snapshot.Repo,
			Reason:        snapshot.BranchHealth.CheckState + "-staging-branch",
			BlockedItems:  []string{},
			Instructions:  []string{"Acquire a lease before editing.", "Fix the shared staging branch before starting new issue work.", "Do not open unrelated issue PRs until branch health is clear."},
		}
	}
	for _, pr := range snapshot.PullRequests {
		state := pr.PullRequest.CheckState
		if state == "failure" || state == "pending" {
			reason := "failing-checks"
			if state == "pending" {
				reason = "pending-checks"
			}
			return NextAction{
				SchemaVersion: 1,
				Kind:          "nextAction",
				Action:        "pr-followup",
				Repo:          snapshot.Repo,
				Reason:        reason,
				PR:            &PRRef{Number: pr.PullRequest.Number, URL: pr.PullRequest.URL, HeadRef: pr.PullRequest.HeadRef, BaseRef: pr.PullRequest.BaseRef},
				BlockedItems:  []string{},
				Instructions:  []string{"Acquire a lease before editing.", "Push to the existing PR branch.", "Do not open a new PR."},
			}
		}
	}
	for _, issue := range snapshot.Issues {
		if issue.Eligible {
			if issue.Action == "issue-investigation" {
				return NextAction{
					SchemaVersion: 1,
					Kind:          "nextAction",
					Action:        "issue-investigation",
					Repo:          snapshot.Repo,
					Reason:        "eligible-investigation",
					Issue:         &IssueRef{Number: issue.Issue.Number, URL: issue.Issue.URL},
					BlockedItems:  []string{},
					Instructions:  []string{"Do not edit files unless the user explicitly changes scope.", "Inspect and comment with findings, evidence, and a recommended next label."},
				}
			}
			return NextAction{
				SchemaVersion: 1,
				Kind:          "nextAction",
				Action:        "issue-implementation",
				Repo:          snapshot.Repo,
				Reason:        "eligible-issue",
				Issue:         &IssueRef{Number: issue.Issue.Number, URL: issue.Issue.URL},
				BlockedItems:  []string{},
				Instructions:  []string{"Acquire a lease before editing.", fmt.Sprintf("Open a PR to the staging branch with Refs #%d.", issue.Issue.Number), "Do not merge."},
			}
		}
	}
	return NextAction{
		SchemaVersion: 1,
		Kind:          "nextAction",
		Action:        "none",
		Repo:          snapshot.Repo,
		Reason:        "no eligible issue or PR follow-up",
		BlockedItems:  []string{},
		Instructions:  []string{},
	}
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
