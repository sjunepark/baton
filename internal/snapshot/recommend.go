package snapshot

import (
	"math"
	"sort"
)

func recommend(repository RepositorySnapshot, requestedAction string) Recommendation {
	if repository.Completeness != Complete {
		return Recommendation{Outcome: OutcomeDegraded, Reasons: []string{"incomplete_facts"}, Candidates: []Candidate{}, DeferredCandidates: []Candidate{}, Instructions: []string{"Retry after the reported facts are complete and stable."}}
	}
	if requestedAction == "issue-investigation" {
		if issues := recommendIssues(repository, ActionIssueInvestigation); issues != nil {
			return *issues
		}
		return idleRecommendation("no eligible candidates for requested action")
	}
	if branch := stagingBranchRecommendation(repository); branch != nil {
		return *branch
	}
	if pullRequests := pullRequestRecommendation(repository, false); pullRequests != nil {
		return *pullRequests
	}
	if issues := recommendIssues(repository, ActionIssueImplementation); issues != nil {
		return *issues
	}
	if issues := recommendIssues(repository, ActionIssueInvestigation); issues != nil {
		return *issues
	}
	if pullRequests := pullRequestRecommendation(repository, true); pullRequests != nil {
		return *pullRequests
	}
	return idleRecommendation("no eligible issue or PR follow-up")
}

func stagingBranchRecommendation(repository RepositorySnapshot) *Recommendation {
	if repository.Queue.BranchHealth == nil {
		return nil
	}
	branch := repository.Queue.BranchHealth
	candidate := Candidate{
		Identity: CandidateIdentity{Repository: repository.Repository, Kind: CandidateBranch, Ref: branch.Ref, SHA: branch.SHA},
		State:    branch.CheckState, Reasons: []string{branch.CheckState + "_staging_branch"},
	}
	switch branch.CheckState {
	case "failure":
		action := ActionBranchHealth
		return &Recommendation{Outcome: OutcomeActionable, Action: &action, Reasons: []string{"failing_staging_branch"}, Candidates: []Candidate{candidate}, DeferredCandidates: deferredCandidates(repository, candidate.Identity), Instructions: []string{"Repair the staging branch in a caller-provided isolated checkout."}}
	case "pending":
		return &Recommendation{Outcome: OutcomeWaiting, Reasons: []string{"pending_staging_branch"}, Candidates: []Candidate{candidate}, DeferredCandidates: deferredCandidates(repository, candidate.Identity), Instructions: []string{"Wait for staging branch checks to complete."}}
	default:
		return nil
	}
}

func pullRequestRecommendation(repository RepositorySnapshot, includeNonActionable bool) *Recommendation {
	actionable, humanChoice, blocked, waiting := []Candidate{}, []Candidate{}, []Candidate{}, []Candidate{}
	for _, pullRequest := range repository.PullRequests {
		candidate, state := classifyPullRequest(pullRequest)
		switch state {
		case "actionable":
			actionable = append(actionable, candidate)
		case "human_choice":
			humanChoice = append(humanChoice, candidate)
		case "blocked":
			blocked = append(blocked, candidate)
		case "waiting":
			waiting = append(waiting, candidate)
		}
	}
	sortCandidates(actionable)
	sortCandidates(humanChoice)
	sortCandidates(blocked)
	sortCandidates(waiting)
	if len(actionable) > 0 {
		action := ActionPullRequestFollowUp
		outcome := OutcomeActionable
		if len(actionable) > 1 {
			outcome = OutcomeHumanChoiceRequired
		}
		deferred := append(append([]Candidate{}, blocked...), waiting...)
		deferred = append(deferred, eligibleIssueCandidates(repository)...)
		sortCandidates(deferred)
		return &Recommendation{Outcome: outcome, Action: &action, Reasons: []string{"pull_request_follow_up_available"}, SelectionRequired: len(actionable) > 1, Candidates: actionable, DeferredCandidates: deferred, Instructions: []string{"Choose a pull request before editing when selection is required.", "Work only in a caller-provided isolated checkout.", "Push only to the existing pull request branch.", "Do not merge."}}
	}
	if !includeNonActionable {
		return nil
	}
	if len(humanChoice) > 0 {
		deferred := append(append([]Candidate{}, blocked...), waiting...)
		sortCandidates(deferred)
		return &Recommendation{Outcome: OutcomeHumanChoiceRequired, Reasons: []string{"pull_request_ready_for_human_disposition"}, SelectionRequired: len(humanChoice) > 1, Candidates: humanChoice, DeferredCandidates: deferred, Instructions: []string{"Review the ready pull request and decide whether to merge, close, or leave it open; Baton will not act."}}
	}
	if len(blocked) > 0 {
		deferred := append(append([]Candidate{}, waiting...), eligibleIssueCandidates(repository)...)
		sortCandidates(deferred)
		return &Recommendation{Outcome: OutcomeBlocked, Reasons: []string{"pull_request_blocked"}, Candidates: blocked, DeferredCandidates: deferred, Instructions: []string{"Resolve the reported external or policy blocker before editing."}}
	}
	if len(waiting) > 0 {
		return &Recommendation{Outcome: OutcomeWaiting, Reasons: []string{"pull_request_waiting"}, Candidates: waiting, DeferredCandidates: eligibleIssueCandidates(repository), Instructions: []string{"Wait for checks or human review; no agent mutation is currently useful."}}
	}
	return nil
}

func classifyPullRequest(pullRequest PullRequestObservation) (Candidate, string) {
	reasons := []string{}
	for _, required := range pullRequest.Checks.Required {
		switch required.State {
		case "failed":
			reasons = append(reasons, "failed_required_check")
		case "missing":
			reasons = append(reasons, "missing_required_check")
		case "pending":
			reasons = append(reasons, "pending_required_check")
		}
	}
	if pullRequest.Checks.State == "failure" {
		reasons = append(reasons, "failing_checks")
	}
	if pullRequest.Review.State == "changes_requested" || pullRequest.ReviewThreads.HumanUnresolved > 0 || pullRequest.ReviewThreads.BotUnresolved > 0 || pullRequest.ReviewThreads.UnknownUnresolved > 0 {
		reasons = append(reasons, "review_feedback")
	}
	if pullRequest.Mergeable == "conflicting" || pullRequest.MergeState == "dirty" || (pullRequest.MergeState == "behind" && pullRequest.Checks.StrictRequiredChecks) {
		reasons = append(reasons, "merge_update_required")
	}
	candidate := Candidate{Identity: pullRequest.Identity, Title: pullRequest.Title, URL: pullRequest.URL, Reasons: uniqueStrings(reasons)}
	if containsAny(candidate.Reasons, "failed_required_check", "failing_checks", "review_feedback", "merge_update_required") {
		candidate.State = "agent_follow_up"
		return candidate, "actionable"
	}
	if containsAny(candidate.Reasons, "missing_required_check") {
		candidate.State = "blocked"
		return candidate, "blocked"
	}
	if pullRequest.MergeState == "blocked" && pullRequest.Review.State != "review_required" {
		candidate.State = "blocked"
		candidate.Reasons = append(candidate.Reasons, "merge_blocked")
		candidate.Reasons = uniqueStrings(candidate.Reasons)
		return candidate, "blocked"
	}
	if pullRequest.Checks.State == "pending" {
		candidate.Reasons = append(candidate.Reasons, "pending_checks")
	}
	if pullRequest.Draft {
		candidate.Reasons = append(candidate.Reasons, "draft")
	}
	if pullRequest.Review.State == "review_required" {
		candidate.Reasons = append(candidate.Reasons, "awaiting_review")
	}
	if !pullRequest.Draft && pullRequest.Checks.State == "success" && pullRequest.Review.State == "approved" && (pullRequest.MergeState == "clean" || pullRequest.MergeState == "unstable" || pullRequest.MergeState == "behind") {
		candidate.State = "human_decision"
		candidate.Reasons = append(candidate.Reasons, "ready_for_human_disposition")
		candidate.Reasons = uniqueStrings(candidate.Reasons)
		return candidate, "human_choice"
	}
	if len(candidate.Reasons) == 0 {
		candidate.Reasons = append(candidate.Reasons, "no_useful_agent_mutation")
	}
	candidate.State = "waiting"
	candidate.Reasons = uniqueStrings(candidate.Reasons)
	return candidate, "waiting"
}

func recommendIssues(repository RepositorySnapshot, action Action) *Recommendation {
	candidates := []Candidate{}
	for _, issue := range repository.Queue.Issues {
		if !issue.Eligible || actionFromLegacy(issue.Action) != action {
			continue
		}
		candidates = append(candidates, Candidate{
			Identity: CandidateIdentity{Repository: repository.Repository, Kind: CandidateIssue, Number: issue.Issue.Number},
			Title:    issue.Issue.Title, URL: issue.Issue.URL, State: "eligible", Reasons: append([]string{}, issue.Reasons...),
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := issueCandidateRank(repository, candidates[i]), issueCandidateRank(repository, candidates[j])
		if left != right {
			return left < right
		}
		return candidates[i].Identity.Number < candidates[j].Identity.Number
	})
	bestRank := issueCandidateRank(repository, candidates[0])
	selected, deferred := []Candidate{}, []Candidate{}
	for _, candidate := range candidates {
		if issueCandidateRank(repository, candidate) == bestRank {
			selected = append(selected, candidate)
		} else {
			deferred = append(deferred, candidate)
		}
	}
	for _, otherAction := range []Action{ActionIssueImplementation, ActionIssueInvestigation} {
		if otherAction == action {
			continue
		}
		deferred = append(deferred, eligibleIssueCandidatesForAction(repository, otherAction)...)
	}
	for _, pullRequest := range repository.PullRequests {
		candidate, _ := classifyPullRequest(pullRequest)
		deferred = append(deferred, candidate)
	}
	sortCandidates(deferred)
	outcome := OutcomeActionable
	if len(selected) > 1 {
		outcome = OutcomeHumanChoiceRequired
	}
	reason, instructions := "eligible-issue", []string{"Choose exactly one candidate.", "Work in a caller-provided isolated checkout.", "Open a PR to the staging branch with Refs #<issue-number>.", "Do not merge."}
	if action == ActionIssueInvestigation {
		reason = "eligible-investigation"
		instructions = []string{"Choose exactly one candidate.", "Do not edit files unless the user explicitly changes scope.", "Inspect and comment with findings, evidence, and a recommended next label."}
	}
	return &Recommendation{Outcome: outcome, Action: &action, Reasons: []string{reason}, SelectionRequired: outcome == OutcomeHumanChoiceRequired, Candidates: selected, DeferredCandidates: deferred, Instructions: instructions}
}

func idleRecommendation(reason string) Recommendation {
	return Recommendation{Outcome: OutcomeIdle, Reasons: []string{reason}, Candidates: []Candidate{}, DeferredCandidates: []Candidate{}, Instructions: []string{}}
}

func actionFromLegacy(action string) Action {
	switch action {
	case "issue-implementation":
		return ActionIssueImplementation
	case "issue-investigation":
		return ActionIssueInvestigation
	case "pr-followup":
		return ActionPullRequestFollowUp
	case "branch-health":
		return ActionBranchHealth
	default:
		return ""
	}
}

func issueCandidateRank(repository RepositorySnapshot, candidate Candidate) int {
	for _, issue := range repository.Queue.Issues {
		if issue.Issue.Number != candidate.Identity.Number {
			continue
		}
		if issue.PriorityRank > 0 {
			return issue.PriorityRank
		}
		return math.MaxInt
	}
	return math.MaxInt
}

func deferredCandidates(repository RepositorySnapshot, selected CandidateIdentity) []Candidate {
	result := []Candidate{}
	for _, pullRequest := range repository.PullRequests {
		if pullRequest.Identity == selected {
			continue
		}
		candidate, _ := classifyPullRequest(pullRequest)
		result = append(result, candidate)
	}
	for _, issue := range repository.Queue.Issues {
		if !issue.Eligible {
			continue
		}
		result = append(result, Candidate{Identity: CandidateIdentity{Repository: repository.Repository, Kind: CandidateIssue, Number: issue.Issue.Number}, Title: issue.Issue.Title, URL: issue.Issue.URL, State: "eligible", Reasons: append([]string(nil), issue.Reasons...)})
	}
	sortCandidates(result)
	return result
}

func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Identity.Kind != candidates[j].Identity.Kind {
			return candidates[i].Identity.Kind < candidates[j].Identity.Kind
		}
		if candidates[i].Identity.Number != candidates[j].Identity.Number {
			return candidates[i].Identity.Number < candidates[j].Identity.Number
		}
		return candidates[i].Identity.Ref < candidates[j].Identity.Ref
	})
}

func eligibleIssueCandidates(repository RepositorySnapshot) []Candidate {
	result := append(eligibleIssueCandidatesForAction(repository, ActionIssueImplementation), eligibleIssueCandidatesForAction(repository, ActionIssueInvestigation)...)
	sortCandidates(result)
	return result
}

func eligibleIssueCandidatesForAction(repository RepositorySnapshot, action Action) []Candidate {
	result := []Candidate{}
	for _, issue := range repository.Queue.Issues {
		if !issue.Eligible || actionFromLegacy(issue.Action) != action {
			continue
		}
		result = append(result, Candidate{
			Identity: CandidateIdentity{Repository: repository.Repository, Kind: CandidateIssue, Number: issue.Issue.Number},
			Title:    issue.Issue.Title, URL: issue.Issue.URL, State: "eligible", Reasons: append([]string{}, issue.Reasons...),
		})
	}
	sortCandidates(result)
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; value == "" || exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsAny(values []string, candidates ...string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if value == candidate {
				return true
			}
		}
	}
	return false
}
