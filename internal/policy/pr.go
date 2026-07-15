package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
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
	Number    int                    `json:"number"`
	Ownership IssueOwnershipDecision `json:"ownership"`
}

type PromotionFacts struct {
	ExpectedIssues           []int                         `json:"expectedIssues"`
	IncludedWorkPullRequests []int                         `json:"includedWorkPullRequests"`
	ExcludedWorkPullRequests []int                         `json:"excludedWorkPullRequests"`
	Complete                 bool                          `json:"complete"`
	Source                   string                        `json:"source"`
	PlanDigest               string                        `json:"planDigest,omitempty"`
	CursorDigest             string                        `json:"cursorDigest,omitempty"`
	CoverageDigest           string                        `json:"coverageDigest,omitempty"`
	BaseIntegration          delivery.BaseIntegrationFacts `json:"baseIntegration"`
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
	PRFlowUnmanaged     PRFlow = "unmanaged"
	PRFlowWork          PRFlow = "work"
	PRFlowPromotion     PRFlow = "promotion"
	PRFlowMisroutedWork PRFlow = "misroutedWork"
	PRFlowIndeterminate PRFlow = "indeterminate"
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
		validatePromotionPullRequest(&errors, input, cfg, closing, facts, baseBranch)
	case PRFlowMisroutedWork:
		errors = append(errors, fmt.Sprintf("Baton work PRs from %s* must target %s before promotion to %s.", cfg.Repository.WorkBranchPrefix, targetBranch, baseBranch))
	case PRFlowIndeterminate:
		errors = append(errors, "Baton managed intent could not be verified because pull request repository identity is incomplete.")
	case PRFlowUnmanaged:
	}

	return PRPolicyDecision{
		SchemaVersion:           4,
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
	prefixedWork := strings.HasPrefix(pr.HeadRef, cfg.Repository.WorkBranchPrefix)
	promotion := pr.BaseRef == cfg.Repository.BaseBranch && pr.HeadRef == cfg.Repository.StagingBranch
	if !prefixedWork && !promotion {
		return PRFlowUnmanaged
	}
	if strings.TrimSpace(pr.BaseRepositoryFullName) == "" || strings.TrimSpace(pr.HeadRepositoryFullName) == "" {
		return PRFlowIndeterminate
	}
	if !strings.EqualFold(pr.BaseRepositoryFullName, pr.HeadRepositoryFullName) {
		return PRFlowUnmanaged
	}
	if promotion {
		return PRFlowPromotion
	}
	if pr.BaseRef == cfg.Repository.StagingBranch {
		return PRFlowWork
	}
	return PRFlowMisroutedWork
}

func validateWorkPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, referenced, closing []int, targetBranch string) {
	if len(referenced) == 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must reference at least one issue with %s #123.", targetBranch, cfg.PRPolicy.RequiredReferenceKeyword))
	}
	if len(closing) > 0 {
		*errors = append(*errors, fmt.Sprintf("Work PRs into %s must use %s #123, not closing keywords.", targetBranch, cfg.PRPolicy.RequiredReferenceKeyword))
	}
	issuesByNumber := map[int]ReferencedIssue{}
	for _, issue := range input.ReferencedIssues {
		issuesByNumber[issue.Number] = issue
	}
	for _, issueNumber := range referenced {
		issue, exists := issuesByNumber[issueNumber]
		if !exists {
			*errors = append(*errors, fmt.Sprintf("#%d could not be loaded for managed-issue validation.", issueNumber))
			continue
		}
		if !issue.Ownership.Managed {
			if issue.Ownership.Source == IssueOwnershipInvalid {
				*errors = append(*errors, fmt.Sprintf("#%d has invalid managed-issue ownership: %s.", issueNumber, strings.Join(issue.Ownership.Errors, "; ")))
			} else {
				*errors = append(*errors, fmt.Sprintf("#%d is not a Baton-managed issue.", issueNumber))
			}
		}
	}

	validateCommitMessages(errors, input, cfg)
}

func validatePromotionPullRequest(errors *[]string, input PRPolicyInput, cfg config.Config, closing []int, facts PromotionFacts, baseBranch string) {
	if !facts.Complete || facts.Source != "sealedDeliveryPlan" || strings.TrimSpace(facts.PlanDigest) == "" || strings.TrimSpace(facts.CursorDigest) == "" || strings.TrimSpace(facts.CoverageDigest) == "" {
		*errors = append(*errors, "Promotion evidence is incomplete; Baton requires an exact sealed delivery plan with matching cursor and coverage.")
	} else if facts.BaseIntegration.State != delivery.BaseIntegrated {
		*errors = append(*errors, fmt.Sprintf("Promotion is blocked because base integration is %s at base %s and staging %s.", facts.BaseIntegration.State, facts.BaseIntegration.ObservedBaseSHA, facts.BaseIntegration.ObservedStagingSHA))
	} else if len(closing) > 0 {
		missing := missingInts(facts.ExpectedIssues, closing)
		extra := missingInts(closing, facts.ExpectedIssues)
		if len(missing) > 0 || len(extra) > 0 {
			*errors = append(*errors, fmt.Sprintf("Promotion closing references into %s are optional presentation, but when present must exactly match the sealed plan; missing: %s; extra: %s.", baseBranch, formatIssueNumbersOrNone(missing), formatIssueNumbersOrNone(extra)))
		}
	}
	validateCommitMessages(errors, input, cfg)
}

func normalizedPromotionFacts(facts *PromotionFacts) PromotionFacts {
	if facts == nil {
		return PromotionFacts{ExpectedIssues: []int{}, IncludedWorkPullRequests: []int{}, ExcludedWorkPullRequests: []int{}}
	}
	return PromotionFacts{
		ExpectedIssues:           uniqueSortedInts(facts.ExpectedIssues),
		IncludedWorkPullRequests: uniqueSortedInts(facts.IncludedWorkPullRequests),
		ExcludedWorkPullRequests: uniqueSortedInts(facts.ExcludedWorkPullRequests),
		Complete:                 facts.Complete, Source: facts.Source, PlanDigest: facts.PlanDigest,
		CursorDigest: facts.CursorDigest, CoverageDigest: facts.CoverageDigest, BaseIntegration: facts.BaseIntegration,
	}
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

func formatIssueNumbersOrNone(numbers []int) string {
	if len(numbers) == 0 {
		return "none"
	}
	return formatIssueNumbers(numbers)
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
