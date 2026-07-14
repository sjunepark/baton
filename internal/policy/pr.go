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

type PromotionFacts struct {
	ExpectedIssues []int `json:"expectedIssues"`
	Complete       bool  `json:"complete"`
}

type PRPolicyInput struct {
	PullRequest             PullRequest       `json:"pullRequest"`
	ReferencedIssues        []ReferencedIssue `json:"referencedIssues"`
	CommitMessages          []string          `json:"commitMessages"`
	CommitListingReachedCap bool              `json:"commitListingReachedCap"`
	PromotionFacts          *PromotionFacts   `json:"promotionFacts,omitempty"`
	Policy                  config.Config     `json:"-"`
}

type PRPolicyDecision struct {
	SchemaVersion           int             `json:"schemaVersion"`
	Kind                    string          `json:"kind"`
	Flow                    PRFlow          `json:"flow"`
	Errors                  []string        `json:"errors"`
	Warnings                []string        `json:"warnings"`
	ReferencedIssues        []int           `json:"referencedIssues"`
	ClosingIssues           []int           `json:"closingIssues"`
	CommitListingReachedCap bool            `json:"commitListingReachedCap"`
	PromotionFacts          *PromotionFacts `json:"promotionFacts,omitempty"`
}

type PRFlow string

const (
	PRFlowWork              PRFlow = "work"
	PRFlowPromotion         PRFlow = "promotion"
	PRFlowDirectBase        PRFlow = "directBase"
	PRFlowInvalidDirectWork PRFlow = "invalidDirectWork"
	PRFlowUnsupportedTarget PRFlow = "unsupportedTarget"
)

var (
	issueNumberPattern = regexp.MustCompile(`#(\d+)`)
)

func ComputePullRequestPolicy(input PRPolicyInput) PRPolicyDecision {
	cfg := input.Policy
	referenceKeywords := referenceKeywordsForPolicy(cfg.PRPolicy.RequiredReferenceKeyword)
	closingKeywords := closingKeywordsForPolicy(cfg.PRPolicy.ForbiddenClosingKeywords)
	referenced := uniqueSortedInts(append(
		extractIssueNumbersAfterKeywords(input.PullRequest.Title, referenceKeywords),
		extractIssueNumbersAfterKeywords(input.PullRequest.Body, referenceKeywords)...,
	))
	closing := uniqueSortedInts(append(
		extractIssueNumbersAfterKeywords(input.PullRequest.Title, closingKeywords),
		extractIssueNumbersAfterKeywords(input.PullRequest.Body, closingKeywords)...,
	))
	errors := []string{}
	targetBranch := cfg.Repository.StagingBranch
	baseBranch := cfg.Repository.BaseBranch
	flow := ClassifyPullRequestFlow(input.PullRequest, cfg)
	var promotionFacts *PromotionFacts

	switch flow {
	case PRFlowWork:
		validateWorkPullRequest(&errors, input, cfg, referenced, closing, targetBranch)
	case PRFlowPromotion:
		facts := normalizedPromotionFacts(input.PromotionFacts)
		promotionFacts = &facts
		validatePromotionPullRequest(&errors, input, cfg, closing, facts, targetBranch, baseBranch)
	case PRFlowInvalidDirectWork:
		errors = append(errors, fmt.Sprintf("Baton work PRs from %s* must target %s before promotion to %s.", cfg.Repository.WorkBranchPrefix, targetBranch, baseBranch))
	case PRFlowDirectBase:
		if !cfg.PRPolicy.AllowDirectBaseBranchPRs {
			errors = append(errors, fmt.Sprintf("Direct PRs into %s are disabled by Baton policy; target %s for Baton-managed work or use %s for promotion.", baseBranch, targetBranch, targetBranch))
		}
	case PRFlowUnsupportedTarget:
		errors = append(errors, fmt.Sprintf("PRs must target %s for agent work or %s for promotion.", targetBranch, baseBranch))
	}

	return PRPolicyDecision{
		SchemaVersion:           1,
		Kind:                    "prPolicyDecision",
		Flow:                    flow,
		Errors:                  errors,
		Warnings:                []string{},
		ReferencedIssues:        referenced,
		ClosingIssues:           closing,
		CommitListingReachedCap: input.CommitListingReachedCap,
		PromotionFacts:          promotionFacts,
	}
}

func ClassifyPullRequestFlow(pr PullRequest, cfg config.Config) PRFlow {
	targetBranch := cfg.Repository.StagingBranch
	baseBranch := cfg.Repository.BaseBranch
	workBranchPrefix := cfg.Repository.WorkBranchPrefix

	switch pr.BaseRef {
	case targetBranch:
		return PRFlowWork
	case baseBranch:
		switch {
		case pr.HeadRef == targetBranch:
			return PRFlowPromotion
		case workBranchPrefix != "" && strings.HasPrefix(pr.HeadRef, workBranchPrefix):
			return PRFlowInvalidDirectWork
		default:
			return PRFlowDirectBase
		}
	default:
		return PRFlowUnsupportedTarget
	}
}

func validateWorkPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, referenced, closing []int, targetBranch string) {
	if len(referenced) == 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must reference at least one issue with %s #123.", targetBranch, cfg.PRPolicy.RequiredReferenceKeyword))
	}
	if len(closing) > 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must use %s #123, not closing keywords.", targetBranch, cfg.PRPolicy.RequiredReferenceKeyword))
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
	trivialLabels := trivialImplementationLabels(cfg.IssuePolicy)
	for _, issue := range implementationIssues {
		if !hasAnyLabel(stringSet(issue.Labels), trivialLabels) {
			allTrivial = false
			break
		}
	}
	if len(referenced) > 1 && allTrivial && cfg.PRPolicy.RejectAllTrivialMultiIssuePRs {
		*errors = append(*errors, "Multi-issue PRs cannot be all-trivial in v1; split them or use bounded review.")
	}

	validateCommitMessages(errors, input, cfg)
}

func validatePromotionPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, closing []int, facts PromotionFacts, targetBranch, baseBranch string) {
	if input.PullRequest.HeadRef != targetBranch {
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must come from %s.", baseBranch, targetBranch))
	}
	if input.PullRequest.HeadRepositoryFullName != "" &&
		input.PullRequest.BaseRepositoryFullName != "" &&
		input.PullRequest.HeadRepositoryFullName != input.PullRequest.BaseRepositoryFullName {
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must come from the same repository.", baseBranch))
	}
	if !facts.Complete {
		*errors = append(*errors, "Promotion evidence is incomplete; Baton could not verify every included work PR and its issue references between the promotion base and head revisions.")
	} else if missing := missingInts(facts.ExpectedIssues, closing); len(missing) > 0 {
		keyword := firstNonEmpty(firstString(cfg.PRPolicy.ForbiddenClosingKeywords), "a closing keyword")
		*errors = append(*errors, fmt.Sprintf("Promotion PRs into %s must close every included Baton issue with %s; missing: %s.", baseBranch, keyword, formatIssueNumbers(missing)))
	}
	validateCommitMessages(errors, input, cfg)
}

func normalizedPromotionFacts(facts *PromotionFacts) PromotionFacts {
	if facts == nil {
		return PromotionFacts{ExpectedIssues: []int{}}
	}
	return PromotionFacts{ExpectedIssues: uniqueSortedInts(facts.ExpectedIssues), Complete: facts.Complete}
}

func missingInts(expected, actual []int) []int {
	actualSet := map[int]struct{}{}
	for _, number := range actual {
		actualSet[number] = struct{}{}
	}
	missing := make([]int, 0)
	for _, number := range uniqueSortedInts(expected) {
		if _, exists := actualSet[number]; !exists {
			missing = append(missing, number)
		}
	}
	return missing
}

func formatIssueNumbers(numbers []int) string {
	values := make([]string, len(numbers))
	for index, number := range numbers {
		values[index] = fmt.Sprintf("#%d", number)
	}
	return strings.Join(values, ", ")
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

func trivialImplementationLabels(issuePolicy config.IssuePolicy) []string {
	result := []string{}
	for option, label := range issuePolicy.AgentModeLabels {
		if normalizePolicyOption(option) == "ready-trivial" {
			result = append(result, label)
		}
	}
	return result
}

func normalizePolicyOption(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastDash := false
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			result.WriteRune(character)
			lastDash = false
		} else if !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func ExtractReferenceIssueNumbers(text string) []int {
	return extractIssueNumbersAfterKeywords(text, referenceKeywordsForPolicy("Refs"))
}

func ExtractReferenceIssueNumbersForPolicy(text, keyword string) []int {
	return extractIssueNumbersAfterKeywords(text, referenceKeywordsForPolicy(keyword))
}

func ExtractClosingIssueNumbers(text string) []int {
	return extractIssueNumbersAfterKeywords(text, closingKeywordsForPolicy([]string{"Closes", "Fixes", "Resolves"}))
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

func extractIssueNumbersAfterKeywords(text string, keywords []string) []int {
	pattern := issueKeywordPattern(keywords)
	if pattern == nil {
		return nil
	}
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

func issueKeywordPattern(keywords []string) *regexp.Regexp {
	alternatives := make([]string, 0, len(keywords))
	seen := map[string]struct{}{}
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		key := strings.ToLower(keyword)
		if _, has := seen[key]; has {
			continue
		}
		seen[key] = struct{}{}
		alternatives = append(alternatives, regexp.QuoteMeta(keyword))
	}
	if len(alternatives) == 0 {
		return nil
	}
	return regexp.MustCompile(`(?i)(?:^|[^\pL\pN_])(?:` + strings.Join(alternatives, "|") + `)[ \t]+((?:#\d+)(?:(?:[ \t]*,[ \t]*|[ \t]+and[ \t]+|[ \t]+)#\d+)*)`)
}

func referenceKeywordsForPolicy(keyword string) []string {
	if strings.EqualFold(strings.TrimSpace(keyword), "Refs") {
		return []string{"Ref", "Refs", "Reference", "References"}
	}
	return []string{keyword}
}

func closingKeywordsForPolicy(keywords []string) []string {
	defaults := []string{"Closes", "Fixes", "Resolves"}
	if sameStringsFold(keywords, defaults) {
		return []string{"Close", "Closes", "Closed", "Fix", "Fixes", "Fixed", "Resolve", "Resolves", "Resolved"}
	}
	return keywords
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sameStringsFold(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := map[string]struct{}{}
	for _, value := range got {
		set[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range want {
		if _, has := set[strings.ToLower(strings.TrimSpace(value))]; !has {
			return false
		}
	}
	return true
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
