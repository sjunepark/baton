package queue

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/workitem"
)

type Issue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Body   string   `json:"-"`
	Labels []string `json:"labels"`
}

type PullRequest struct {
	Number     int           `json:"number"`
	Title      string        `json:"title"`
	URL        string        `json:"url"`
	Body       string        `json:"-"`
	BaseRef    string        `json:"baseRef"`
	HeadRef    string        `json:"headRef"`
	HeadSHA    string        `json:"headSha"`
	CheckState string        `json:"checkState,omitempty"`
	State      string        `json:"-"`
	Merged     bool          `json:"-"`
	Ownership  policy.PRFlow `json:"-"`
	// ReferencedIssues is populated from durable delivery records for merged
	// work. Nil keeps current-title/body parsing for open diagnostic PRs.
	ReferencedIssues []int `json:"-"`
}

type Snapshot struct {
	SchemaVersion    int                            `json:"schemaVersion"`
	Kind             string                         `json:"kind"`
	Repo             string                         `json:"repo"`
	ReferenceKeyword string                         `json:"-"`
	BaseBranch       string                         `json:"-"`
	StagingBranch    string                         `json:"-"`
	Counts           SnapshotCounts                 `json:"counts"`
	BranchHealth     *BranchHealth                  `json:"branchHealth,omitempty"`
	BaseIntegration  *delivery.BaseIntegrationFacts `json:"baseIntegration,omitempty"`
	Issues           []IssueState                   `json:"issues"`
	PullRequests     []PullState                    `json:"pullRequests"`
	Help             []string                       `json:"help,omitempty"`
}

type SnapshotCounts struct {
	TotalIssues       int            `json:"totalIssues"`
	EligibleIssues    int            `json:"eligibleIssues"`
	EligibleByAction  map[string]int `json:"eligibleByAction,omitempty"`
	SkippedIssues     int            `json:"skippedIssues"`
	OpenPullRequests  int            `json:"openPullRequests"`
	BranchHealthState string         `json:"branchHealthState,omitempty"`
}

type BranchHealth struct {
	Ref        string `json:"ref"`
	SHA        string `json:"sha"`
	CheckState string `json:"checkState"`
}

type IssueState struct {
	Issue         Issue          `json:"issue"`
	State         workitem.State `json:"-"`
	Eligible      bool           `json:"eligible"`
	Action        string         `json:"action,omitempty"`
	PriorityLabel string         `json:"priorityLabel,omitempty"`
	PriorityRank  int            `json:"priorityRank,omitempty"`
	Reasons       []string       `json:"reasons"`
	LinkedPRs     []int          `json:"linkedPrs"`
}

type PullState struct {
	PullRequest      PullRequest `json:"pullRequest"`
	ReferencedIssues []int       `json:"referencedIssues"`
}

type NextCandidates struct {
	SchemaVersion         int             `json:"schemaVersion"`
	Kind                  string          `json:"kind"`
	SelectedAction        string          `json:"selectedAction"`
	Repo                  string          `json:"repo"`
	Reason                string          `json:"reason"`
	SelectionReason       string          `json:"selectionReason"`
	SelectionRequired     bool            `json:"selectionRequired"`
	Candidates            []NextCandidate `json:"candidates"`
	DeferredEligibleItems []NextCandidate `json:"deferredEligibleItems"`
	BlockedItems          []string        `json:"blockedItems"`
	Instructions          []string        `json:"instructions"`
}

type NextCandidate struct {
	Type          string `json:"type"`
	Number        int    `json:"number,omitempty"`
	Title         string `json:"title,omitempty"`
	URL           string `json:"url,omitempty"`
	HeadRef       string `json:"headRef,omitempty"`
	BaseRef       string `json:"baseRef,omitempty"`
	Ref           string `json:"ref,omitempty"`
	SHA           string `json:"sha,omitempty"`
	CheckState    string `json:"checkState,omitempty"`
	PriorityLabel string `json:"priorityLabel,omitempty"`
	action        string
	priorityRank  int
}

func BuildSnapshot(repo string, cfg config.Config, issues []Issue, prs []PullRequest) Snapshot {
	return BuildSnapshotWithBranchHealth(repo, cfg, issues, prs, nil)
}

func BuildSnapshotWithBranchHealth(repo string, cfg config.Config, issues []Issue, prs []PullRequest, branchHealth *BranchHealth) Snapshot {
	return BuildSnapshotWithLifecycle(repo, cfg, issues, prs, nil, branchHealth)
}

func BuildSnapshotWithLifecycle(repo string, cfg config.Config, issues []Issue, prs, mergedWorkPRs []PullRequest, branchHealth *BranchHealth) Snapshot {
	prStates := make([]PullState, 0, len(prs))
	prsByIssue := map[int][]int{}
	mergedPRsByIssue := map[int][]int{}
	for _, pr := range prs {
		if pr.Ownership != "" && pr.Ownership != policy.PRFlowWork && pr.Ownership != policy.PRFlowPromotion {
			continue
		}
		referenced := referencedIssues(pr, cfg.PRPolicy.RequiredReferenceKeyword)
		if pr.Ownership == "" || pr.Ownership == policy.PRFlowWork {
			for _, issueNumber := range referenced {
				prsByIssue[issueNumber] = append(prsByIssue[issueNumber], pr.Number)
			}
		}
		prStates = append(prStates, PullState{PullRequest: pr, ReferencedIssues: referenced})
	}
	for _, pr := range mergedWorkPRs {
		if !pr.Merged || (pr.Ownership != "" && pr.Ownership != policy.PRFlowWork) {
			continue
		}
		for _, issueNumber := range referencedIssues(pr, cfg.PRPolicy.RequiredReferenceKeyword) {
			mergedPRsByIssue[issueNumber] = append(mergedPRsByIssue[issueNumber], pr.Number)
		}
	}

	issueStates := make([]IssueState, 0, len(issues))
	eligibleIssues := 0
	eligibleByAction := map[string]int{}
	for _, issue := range issues {
		linkedPRs := prsByIssue[issue.Number]
		if linkedPRs == nil {
			linkedPRs = []int{}
		}
		state := IssueState{Issue: issue, Reasons: []string{}, LinkedPRs: linkedPRs}
		labels := stringSet(issue.Labels)
		state.PriorityLabel, state.PriorityRank = issuePriority(cfg.IssuePolicy, labels)
		readiness := workitem.ClassifyIssue(workitem.IssueFacts{Open: true, Labels: issue.Labels, LinkedWorkPRs: linkedPRs, MergedWorkPRs: mergedPRsByIssue[issue.Number]}, cfg.IssuePolicy)
		state.State, state.Eligible, state.Action, state.Reasons = readiness.State, readiness.Eligible, readiness.Action, readiness.Reasons
		if state.Eligible {
			eligibleIssues++
			eligibleByAction[state.Action]++
		}
		issueStates = append(issueStates, state)
	}

	counts := SnapshotCounts{
		TotalIssues:      len(issueStates),
		EligibleIssues:   eligibleIssues,
		EligibleByAction: eligibleByAction,
		SkippedIssues:    len(issueStates) - eligibleIssues,
		OpenPullRequests: len(prStates),
	}
	if branchHealth != nil {
		counts.BranchHealthState = branchHealth.CheckState
	}
	return Snapshot{
		SchemaVersion:    2,
		Kind:             "queueSnapshot",
		Repo:             repo,
		ReferenceKeyword: cfg.PRPolicy.RequiredReferenceKeyword,
		BaseBranch:       cfg.Repository.BaseBranch, StagingBranch: cfg.Repository.StagingBranch,
		Counts:       counts,
		BranchHealth: branchHealth,
		Issues:       issueStates,
		PullRequests: prStates,
		Help:         snapshotHelp(issueStates, prStates),
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
			deferredEligibleItems(snapshot, "branch-health", snapshot.BranchHealth.CheckState+"-staging-branch"),
			[]string{"Work in a caller-provided isolated checkout.", "Fix the shared staging branch before starting new issue work.", "Do not open unrelated issue PRs until branch health is clear."},
		)
	}
	if snapshot.BaseIntegration != nil && snapshot.BaseIntegration.State == delivery.BaseDirectWorkPending {
		return nextCandidates(snapshot.Repo, "sync-staging", "direct-base-work-pending",
			[]NextCandidate{{Type: "repository", BaseRef: snapshot.StagingBranch, HeadRef: snapshot.BaseBranch, Ref: snapshot.StagingBranch, SHA: snapshot.BaseIntegration.ObservedStagingSHA}},
			deferredEligibleItems(snapshot, "sync-staging", "direct-base-work-pending"),
			[]string{"Open a normal human-reviewed pull request from the configured base branch into staging.", "Merge it with a merge commit so both histories remain ancestors of the result.", "Do not push, merge, rebase, squash, or rewrite staging automatically."},
		)
	}

	for _, tier := range []string{"failing-checks", "pending-checks", "ready-for-review", "open-pr"} {
		candidates := []NextCandidate{}
		for _, pr := range snapshot.PullRequests {
			if prFollowupReason(pr.PullRequest.CheckState) != tier {
				continue
			}
			candidates = append(candidates, pullRequestCandidate(pr.PullRequest))
		}
		if len(candidates) > 0 {
			sortCandidates(candidates)
			return nextCandidates(snapshot.Repo, "pr-followup", tier, candidates,
				deferredEligibleItems(snapshot, "pr-followup", tier),
				[]string{"Choose exactly one candidate.", "Work in a caller-provided isolated checkout.", "Push to the existing PR branch.", "Do not open a new PR."},
			)
		}
	}

	implementationCandidates := issueCandidates(snapshot.Issues, "issue-implementation")
	if len(implementationCandidates) > 0 {
		sortCandidates(implementationCandidates)
		selectedCandidates, lowerPriorityCandidates := highestPriorityCandidates(implementationCandidates)
		deferred := append([]NextCandidate{}, lowerPriorityCandidates...)
		deferred = append(deferred, deferredEligibleItems(snapshot, "issue-implementation", "eligible-issue")...)
		sortCandidates(deferred)
		return nextCandidates(snapshot.Repo, "issue-implementation", "eligible-issue", selectedCandidates,
			deferred,
			[]string{"Choose exactly one candidate.", "Work in a caller-provided isolated checkout.", "Open a PR to the staging branch with " + referenceInstruction(snapshot.ReferenceKeyword) + ".", "Do not merge."},
		)
	}

	investigationCandidates := issueCandidates(snapshot.Issues, "issue-investigation")
	if len(investigationCandidates) > 0 {
		sortCandidates(investigationCandidates)
		selectedCandidates, lowerPriorityCandidates := highestPriorityCandidates(investigationCandidates)
		sortCandidates(lowerPriorityCandidates)
		return nextCandidates(snapshot.Repo, "issue-investigation", "eligible-investigation", selectedCandidates,
			lowerPriorityCandidates,
			[]string{"Choose exactly one candidate.", "Do not edit files unless the user explicitly changes scope.", "Inspect and comment with findings, evidence, and a recommended next label."},
		)
	}

	return nextCandidates(snapshot.Repo, "none", "no eligible issue or PR follow-up", []NextCandidate{}, []NextCandidate{}, []string{})
}

func referenceInstruction(keyword string) string {
	if keyword = strings.TrimSpace(keyword); keyword == "" {
		keyword = "Refs"
	}
	return keyword + " #<issue-number>"
}

func RecommendNextInvestigation(snapshot Snapshot) NextCandidates {
	candidates := issueCandidates(snapshot.Issues, "issue-investigation")
	sortCandidates(candidates)
	selectedCandidates, lowerPriorityCandidates := highestPriorityCandidates(candidates)
	sortCandidates(lowerPriorityCandidates)
	instructions := []string{"Choose exactly one candidate from the requested action."}
	if len(candidates) == 0 {
		instructions = []string{"Run `baton queue --format toon` to inspect eligible and skipped issues."}
	}
	if len(candidates) > 0 {
		instructions = append(instructions, "Do not edit files unless the user explicitly changes scope.", "Inspect and comment with findings, evidence, and a recommended next label.")
	}
	reason := "requested-action"
	if len(candidates) == 0 {
		reason = "no eligible candidates for requested action"
	}
	return nextCandidates(snapshot.Repo, "issue-investigation", reason, selectedCandidates, lowerPriorityCandidates, instructions)
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
			Type:          "issue",
			Number:        issue.Issue.Number,
			Title:         issue.Issue.Title,
			URL:           issue.Issue.URL,
			PriorityLabel: issue.PriorityLabel,
			action:        issueAction,
			priorityRank:  issuePriorityRank(issue),
		})
	}
	return candidates
}

func deferredEligibleItems(snapshot Snapshot, action, reason string) []NextCandidate {
	deferred := []NextCandidate{}
	if action == "branch-health" {
		for _, pr := range snapshot.PullRequests {
			deferred = append(deferred, pullRequestCandidate(pr.PullRequest))
		}
		deferred = append(deferred, issueCandidates(snapshot.Issues, "issue-implementation")...)
		deferred = append(deferred, issueCandidates(snapshot.Issues, "issue-investigation")...)
		sortCandidates(deferred)
		return deferred
	}
	if action == "pr-followup" {
		for _, pr := range snapshot.PullRequests {
			if prFollowupRank(prFollowupReason(pr.PullRequest.CheckState)) <= prFollowupRank(reason) {
				continue
			}
			deferred = append(deferred, pullRequestCandidate(pr.PullRequest))
		}
		deferred = append(deferred, issueCandidates(snapshot.Issues, "issue-implementation")...)
		deferred = append(deferred, issueCandidates(snapshot.Issues, "issue-investigation")...)
		sortCandidates(deferred)
		return deferred
	}
	if action == "issue-implementation" {
		deferred = append(deferred, issueCandidates(snapshot.Issues, "issue-investigation")...)
		sortCandidates(deferred)
		return deferred
	}
	return deferred
}

func pullRequestCandidate(pr PullRequest) NextCandidate {
	return NextCandidate{
		Type:       "pullRequest",
		Number:     pr.Number,
		Title:      pr.Title,
		URL:        pr.URL,
		HeadRef:    pr.HeadRef,
		BaseRef:    pr.BaseRef,
		CheckState: pr.CheckState,
	}
}

func prFollowupRank(reason string) int {
	switch reason {
	case "failing-checks":
		return 1
	case "pending-checks":
		return 2
	case "ready-for-review":
		return 3
	case "open-pr":
		return 4
	default:
		return 0
	}
}

func selectionReason(action, reason string, candidates, deferred []NextCandidate) string {
	if len(deferred) == 0 {
		return reason
	}
	switch action {
	case "branch-health":
		return "staging-branch-health-precedes-queue-work"
	case "pr-followup":
		return reason + "-precedes-lower-priority-work"
	case "issue-implementation":
		if hasLowerPrioritySameTier(candidates, deferred) {
			return "issue-priority-precedes-lower-priority-work"
		}
		return "implementation-work-precedes-investigation"
	case "issue-investigation":
		if hasLowerPrioritySameTier(candidates, deferred) {
			return "issue-priority-precedes-lower-priority-work"
		}
		return reason
	default:
		return reason
	}
}

func nextCandidates(repo, action, reason string, candidates, deferred []NextCandidate, instructions []string) NextCandidates {
	return NextCandidates{
		SchemaVersion:         3,
		Kind:                  "nextCandidates",
		SelectedAction:        action,
		Repo:                  repo,
		Reason:                reason,
		SelectionReason:       selectionReason(action, reason, candidates, deferred),
		SelectionRequired:     len(candidates) > 1,
		Candidates:            candidates,
		DeferredEligibleItems: deferred,
		BlockedItems:          []string{},
		Instructions:          instructions,
	}
}

func sortCandidates(candidates []NextCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Type == "issue" && candidates[j].Type == "issue" {
			if prioritySortRank(candidates[i]) != prioritySortRank(candidates[j]) {
				return prioritySortRank(candidates[i]) < prioritySortRank(candidates[j])
			}
		}
		return candidates[i].Number < candidates[j].Number
	})
}

func highestPriorityCandidates(candidates []NextCandidate) ([]NextCandidate, []NextCandidate) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	bestRank := prioritySortRank(candidates[0])
	if bestRank == math.MaxInt {
		return candidates, nil
	}
	selected := []NextCandidate{}
	lowerPriority := []NextCandidate{}
	for _, candidate := range candidates {
		if prioritySortRank(candidate) == bestRank {
			selected = append(selected, candidate)
			continue
		}
		lowerPriority = append(lowerPriority, candidate)
	}
	return selected, lowerPriority
}

func prioritySortRank(candidate NextCandidate) int {
	if candidate.priorityRank > 0 {
		return candidate.priorityRank
	}
	if rank := priorityLabelRank(candidate.PriorityLabel); rank > 0 {
		return rank
	}
	return math.MaxInt
}

func issuePriorityRank(issue IssueState) int {
	if issue.PriorityRank > 0 {
		return issue.PriorityRank
	}
	return priorityLabelRank(issue.PriorityLabel)
}

func priorityLabelRank(label string) int {
	if !strings.HasPrefix(label, "priority:p") {
		return 0
	}
	priority, err := strconv.Atoi(strings.TrimPrefix(label, "priority:p"))
	if err != nil {
		return 0
	}
	return priority + 1
}

func hasLowerPrioritySameTier(candidates, deferred []NextCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if candidate.action == "" {
			continue
		}
		for _, item := range deferred {
			if item.Type == "issue" && item.action == candidate.action && prioritySortRank(item) > prioritySortRank(candidate) {
				return true
			}
		}
	}
	return false
}

func issuePriority(issuePolicy config.IssuePolicy, labels map[string]struct{}) (string, int) {
	if len(issuePolicy.PriorityLabels) == 0 {
		return "", 0
	}
	for index, label := range issuePolicy.ControlledLabelGroups["priority"] {
		if _, ok := labels[label]; ok {
			return label, index + 1
		}
	}
	return "", 0
}

func referencedIssues(pr PullRequest, keyword string) []int {
	if pr.ReferencedIssues != nil {
		values := append([]int(nil), pr.ReferencedIssues...)
		sort.Ints(values)
		out := values[:0]
		for _, value := range values {
			if value <= 0 || (len(out) > 0 && out[len(out)-1] == value) {
				continue
			}
			out = append(out, value)
		}
		return out
	}
	values := append(policy.ExtractReferenceIssueNumbersForPolicy(pr.Title, keyword), policy.ExtractReferenceIssueNumbersForPolicy(pr.Body, keyword)...)
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

func snapshotHelp(issues []IssueState, prs []PullState) []string {
	help := []string{"Run `baton next --json`."}
	if len(prs) > 0 {
		help = append(help, "Run `baton pr <number> --json` or `baton checks <number> --json` for PR details.")
	}
	for _, issue := range issues {
		if issue.Eligible && issue.Action == "issue-implementation" {
			help = append(help, fmt.Sprintf("Prepare an isolated checkout, then create a work branch for issue %d from the configured staging branch.", issue.Issue.Number))
			return help
		}
	}
	for _, issue := range issues {
		if issue.Eligible && issue.Action == "issue-investigation" {
			help = append(help, "Run `baton next --action issue-investigation --format toon` to select investigation work.")
			return help
		}
	}
	if len(prs) == 0 {
		help = append(help, "Run `baton doctor --json` if no eligible work appears.")
	}
	return help
}
