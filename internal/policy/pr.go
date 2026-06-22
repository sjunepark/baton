package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sjunepark/baton/internal/config"
)

const PRCommitListingCap = 250

type PullRequest struct {
	Number                 int    `json:"number"`
	Title                  string `json:"title"`
	Body                   string `json:"body"`
	BaseRef                string `json:"baseRef"`
	HeadRef                string `json:"headRef"`
	BaseRepositoryFullName string `json:"baseRepositoryFullName"`
	HeadRepositoryFullName string `json:"headRepositoryFullName"`
}

type ReferencedIssue struct {
	Number int      `json:"number"`
	Labels []string `json:"labels"`
}

type PRPolicyInput struct {
	PullRequest             PullRequest       `json:"pullRequest"`
	ReferencedIssues        []ReferencedIssue `json:"referencedIssues"`
	CommitMessages          []string          `json:"commitMessages"`
	CommitListingReachedCap bool              `json:"commitListingReachedCap"`
	Policy                  config.Config     `json:"-"`
}

type PRPolicyDecision struct {
	SchemaVersion           int      `json:"schemaVersion"`
	Kind                    string   `json:"kind"`
	Errors                  []string `json:"errors"`
	Warnings                []string `json:"warnings"`
	ReferencedIssues        []int    `json:"referencedIssues"`
	ClosingIssues           []int    `json:"closingIssues"`
	CommitListingReachedCap bool     `json:"commitListingReachedCap"`
}

var (
	referencePattern   = regexp.MustCompile(`(?i)\b(?:refs?|references?)[ \t]+((?:#\d+)(?:(?:[ \t]*,[ \t]*|[ \t]+and[ \t]+|[ \t]+)#\d+)*)`)
	closingPattern     = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[ \t]+((?:#\d+)(?:(?:[ \t]*,[ \t]*|[ \t]+and[ \t]+|[ \t]+)#\d+)*)`)
	issueNumberPattern = regexp.MustCompile(`#(\d+)`)
)

func ComputePullRequestPolicy(input PRPolicyInput) PRPolicyDecision {
	cfg := input.Policy
	if cfg.Version == 0 {
		cfg = config.DefaultCreoCompat()
	}
	referenced := uniqueSortedInts(append(
		ExtractReferenceIssueNumbers(input.PullRequest.Title),
		ExtractReferenceIssueNumbers(input.PullRequest.Body)...,
	))
	closing := uniqueSortedInts(append(
		ExtractClosingIssueNumbers(input.PullRequest.Title),
		ExtractClosingIssueNumbers(input.PullRequest.Body)...,
	))
	errors := []string{}
	targetBranch := firstNonEmpty(cfg.Repository.StagingBranch, "agent")
	baseBranch := firstNonEmpty(cfg.Repository.BaseBranch, "main")

	switch input.PullRequest.BaseRef {
	case targetBranch:
		validateWorkPullRequest(&errors, input, cfg, referenced, closing, targetBranch)
	case baseBranch:
		validatePromotionPullRequest(&errors, input, cfg, closing, targetBranch, baseBranch)
	default:
		errors = append(errors, fmt.Sprintf("PRs must target %s for agent work or %s for promotion.", targetBranch, baseBranch))
	}

	return PRPolicyDecision{
		SchemaVersion:           1,
		Kind:                    "prPolicyDecision",
		Errors:                  errors,
		Warnings:                []string{},
		ReferencedIssues:        referenced,
		ClosingIssues:           closing,
		CommitListingReachedCap: input.CommitListingReachedCap,
	}
}

func validateWorkPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, referenced, closing []int, targetBranch string) {
	if len(referenced) == 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must reference at least one issue with Refs #123.", targetBranch))
	}
	if len(closing) > 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must use Refs #123, not closing keywords.", targetBranch))
	}
	if cfg.Repository.WorkBranchPrefix != "" && !strings.HasPrefix(input.PullRequest.HeadRef, cfg.Repository.WorkBranchPrefix) {
		*errors = append(*errors, fmt.Sprintf("Work PR branches into %s must start with %s; %s/... is reserved by the shared staging branch.", targetBranch, cfg.Repository.WorkBranchPrefix, targetBranch))
	}

	issuesByNumber := map[int]ReferencedIssue{}
	for _, issue := range input.ReferencedIssues {
		issuesByNumber[issue.Number] = issue
	}
	for _, issueNumber := range referenced {
		issue, exists := issuesByNumber[issueNumber]
		if !exists {
			*errors = append(*errors, fmt.Sprintf("#%d could not be loaded for label policy validation.", issueNumber))
			continue
		}
		labels := stringSet(issue.Labels)
		if !hasAnyLabel(labels, cfg.IssuePolicy.ImplementationLabels) {
			*errors = append(*errors, fmt.Sprintf("#%d must have one of: %s.", issueNumber, strings.Join(cfg.IssuePolicy.ImplementationLabels, ", ")))
		}
		for _, skipLabel := range cfg.IssuePolicy.SkipLabels {
			if _, has := labels[skipLabel]; has {
				*errors = append(*errors, fmt.Sprintf("#%d has skip label %s.", issueNumber, skipLabel))
				break
			}
		}
	}

	implementationIssues := make([]ReferencedIssue, 0)
	for _, issueNumber := range referenced {
		issue, exists := issuesByNumber[issueNumber]
		if !exists {
			continue
		}
		if hasAnyLabel(stringSet(issue.Labels), cfg.IssuePolicy.ImplementationLabels) {
			implementationIssues = append(implementationIssues, issue)
		}
	}
	allTrivial := len(implementationIssues) > 0
	for _, issue := range implementationIssues {
		if _, has := stringSet(issue.Labels)["agent:ready-trivial"]; !has {
			allTrivial = false
			break
		}
	}
	if len(referenced) > 1 && allTrivial && cfg.PRPolicy.RejectAllTrivialMultiIssuePRs {
		*errors = append(*errors, "Multi-issue PRs cannot be all-trivial in v1; split them or use bounded review.")
	}

	validateCommitMessages(errors, input, cfg)
}

func validatePromotionPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, closing []int, targetBranch, baseBranch string) {
	if input.PullRequest.HeadRef != targetBranch {
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must come from %s.", baseBranch, targetBranch))
	}
	if input.PullRequest.HeadRepositoryFullName != "" &&
		input.PullRequest.BaseRepositoryFullName != "" &&
		input.PullRequest.HeadRepositoryFullName != input.PullRequest.BaseRepositoryFullName {
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must come from the same repository.", baseBranch))
	}
	if len(closing) == 0 {
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must close promoted issues with Closes #123.", baseBranch))
	}
	validateCommitMessages(errors, input, cfg)
}

func validateCommitMessages(errors *[]string, input PRPolicyInput, cfg config.Config) {
	if input.CommitListingReachedCap && cfg.PRPolicy.FailWhenCommitListingReachesCap {
		*errors = append(*errors, fmt.Sprintf("PR commit listing reached GitHub API cap of %d commits; commit hygiene cannot be fully verified.", PRCommitListingCap))
	}
	for _, message := range input.CommitMessages {
		subject := strings.TrimSpace(strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")[0])
		if IsNoisyCommitSubject(subject, cfg.PRPolicy.NoisyCommitSubjects) {
			*errors = append(*errors, fmt.Sprintf("Commit subject is too vague to keep permanently: %q.", subject))
		}
	}
}

func ExtractReferenceIssueNumbers(text string) []int {
	return extractIssueNumbersAfterKeyword(text, referencePattern)
}

func ExtractClosingIssueNumbers(text string) []int {
	return extractIssueNumbersAfterKeyword(text, closingPattern)
}

func IsNoisyCommitSubject(subject string, noisySubjects []string) bool {
	normalized := strings.ToLower(strings.TrimSpace(subject))
	normalized = strings.TrimRight(normalized, ".!:")
	normalized = strings.Join(strings.Fields(normalized), " ")
	for _, noisy := range noisySubjects {
		if normalized == noisy {
			return true
		}
	}
	return false
}

func extractIssueNumbersAfterKeyword(text string, pattern *regexp.Regexp) []int {
	numbers := []int{}
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		for _, issueMatch := range issueNumberPattern.FindAllStringSubmatch(match[1], -1) {
			var number int
			if _, err := fmt.Sscanf(issueMatch[1], "%d", &number); err == nil {
				numbers = append(numbers, number)
			}
		}
	}
	return numbers
}

func uniqueSortedInts(values []int) []int {
	set := map[int]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]int, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func hasAnyLabel(labels map[string]struct{}, candidates []string) bool {
	for _, candidate := range candidates {
		if _, has := labels[candidate]; has {
			return true
		}
	}
	return false
}
