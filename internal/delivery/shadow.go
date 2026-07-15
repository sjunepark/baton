package delivery

import (
	"fmt"
	"sort"
)

// PromotionProjection is the transport-neutral selection used only to prove a
// migration shadow read. Production authority is the resolved sealed plan.
type PromotionProjection struct {
	Complete bool                      `json:"complete"`
	Work     []PromotionProjectionWork `json:"work"`
}

type PromotionProjectionWork struct {
	PullRequestNumber int   `json:"pullRequestNumber"`
	IssueNumbers      []int `json:"issueNumbers"`
	Excluded          bool  `json:"excluded"`
}

type PromotionShadowMismatch struct {
	Code              string `json:"code"`
	PullRequestNumber int    `json:"pullRequestNumber,omitempty"`
	Message           string `json:"message"`
}

type PromotionShadowComparison struct {
	Complete   bool                      `json:"complete"`
	Matches    bool                      `json:"matches"`
	Ledger     PromotionProjection       `json:"ledger"`
	Legacy     PromotionProjection       `json:"legacy"`
	Mismatches []PromotionShadowMismatch `json:"mismatches"`
}

func ProjectionFromPromotionWork(work []PromotionWork) PromotionProjection {
	projection := PromotionProjection{Complete: true, Work: make([]PromotionProjectionWork, 0, len(work))}
	for _, item := range work {
		issues := make([]int, 0, len(item.Issues))
		for _, issue := range item.Issues {
			issues = append(issues, issue.Number)
		}
		projection.Work = append(projection.Work, PromotionProjectionWork{PullRequestNumber: item.PullRequest.Number, IssueNumbers: uniqueSortedProjectionInts(issues), Excluded: item.Excluded})
	}
	return normalizePromotionProjection(projection)
}

// ComparePromotionProjections reports stable blocking mismatch codes. Reviewed
// exclusions explain why work is omitted from expected delivery, but the
// underlying work PR and snapshotted issue set must still agree with shadow
// ancestry evidence.
func ComparePromotionProjections(ledger, legacy PromotionProjection) PromotionShadowComparison {
	ledger = normalizePromotionProjection(ledger)
	legacy = normalizePromotionProjection(legacy)
	result := PromotionShadowComparison{Complete: ledger.Complete && legacy.Complete, Ledger: ledger, Legacy: legacy, Mismatches: []PromotionShadowMismatch{}}
	if !ledger.Complete {
		result.Mismatches = append(result.Mismatches, PromotionShadowMismatch{Code: "ledger-incomplete", Message: "delivery-ledger promotion projection is incomplete"})
	}
	if !legacy.Complete {
		result.Mismatches = append(result.Mismatches, PromotionShadowMismatch{Code: "legacy-incomplete", Message: "legacy ancestry promotion projection is incomplete"})
	}
	ledgerByPR := projectionByPullRequest(ledger.Work)
	legacyByPR := projectionByPullRequest(legacy.Work)
	for number, item := range ledgerByPR {
		legacyItem, found := legacyByPR[number]
		if !found {
			result.Mismatches = append(result.Mismatches, PromotionShadowMismatch{Code: "ledger-only-work", PullRequestNumber: number, Message: fmt.Sprintf("work PR #%d exists only in the delivery-ledger projection", number)})
			continue
		}
		if !sameProjectionInts(item.IssueNumbers, legacyItem.IssueNumbers) {
			result.Mismatches = append(result.Mismatches, PromotionShadowMismatch{Code: "issue-set-mismatch", PullRequestNumber: number, Message: fmt.Sprintf("work PR #%d has different ledger and ancestry issue sets", number)})
		}
	}
	for number := range legacyByPR {
		if _, found := ledgerByPR[number]; !found {
			result.Mismatches = append(result.Mismatches, PromotionShadowMismatch{Code: "legacy-only-work", PullRequestNumber: number, Message: fmt.Sprintf("work PR #%d exists only in the ancestry projection", number)})
		}
	}
	sort.Slice(result.Mismatches, func(i, j int) bool {
		if result.Mismatches[i].PullRequestNumber != result.Mismatches[j].PullRequestNumber {
			return result.Mismatches[i].PullRequestNumber < result.Mismatches[j].PullRequestNumber
		}
		return result.Mismatches[i].Code < result.Mismatches[j].Code
	})
	result.Matches = result.Complete && len(result.Mismatches) == 0
	return result
}

func normalizePromotionProjection(value PromotionProjection) PromotionProjection {
	work := append([]PromotionProjectionWork(nil), value.Work...)
	for index := range work {
		work[index].IssueNumbers = uniqueSortedProjectionInts(work[index].IssueNumbers)
	}
	sort.Slice(work, func(i, j int) bool { return work[i].PullRequestNumber < work[j].PullRequestNumber })
	value.Work = work
	return value
}

func projectionByPullRequest(values []PromotionProjectionWork) map[int]PromotionProjectionWork {
	result := make(map[int]PromotionProjectionWork, len(values))
	for _, value := range values {
		if existing, duplicate := result[value.PullRequestNumber]; duplicate {
			existing.IssueNumbers = uniqueSortedProjectionInts(append(existing.IssueNumbers, value.IssueNumbers...))
			existing.Excluded = existing.Excluded && value.Excluded
			result[value.PullRequestNumber] = existing
			continue
		}
		result[value.PullRequestNumber] = value
	}
	return result
}

func uniqueSortedProjectionInts(values []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func sameProjectionInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
