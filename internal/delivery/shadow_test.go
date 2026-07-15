package delivery

import "testing"

func TestPromotionShadowComparisonMatchesIncludedAndReviewedExcludedWork(t *testing.T) {
	ledger := PromotionProjection{Complete: true, Work: []PromotionProjectionWork{
		{PullRequestNumber: 21, IssueNumbers: []int{8}, Excluded: true},
		{PullRequestNumber: 20, IssueNumbers: []int{7, 7}},
	}}
	legacy := PromotionProjection{Complete: true, Work: []PromotionProjectionWork{
		{PullRequestNumber: 20, IssueNumbers: []int{7}},
		{PullRequestNumber: 21, IssueNumbers: []int{8}},
	}}
	comparison := ComparePromotionProjections(ledger, legacy)
	if !comparison.Complete || !comparison.Matches || len(comparison.Mismatches) != 0 {
		t.Fatalf("comparison = %+v", comparison)
	}
}

func TestPromotionShadowComparisonReportsStableBlockingMismatches(t *testing.T) {
	comparison := ComparePromotionProjections(
		PromotionProjection{Complete: true, Work: []PromotionProjectionWork{{PullRequestNumber: 20, IssueNumbers: []int{7}}, {PullRequestNumber: 22, IssueNumbers: []int{9}}}},
		PromotionProjection{Complete: false, Work: []PromotionProjectionWork{{PullRequestNumber: 20, IssueNumbers: []int{8}}, {PullRequestNumber: 21, IssueNumbers: []int{9}}}},
	)
	if comparison.Complete || comparison.Matches || len(comparison.Mismatches) != 4 {
		t.Fatalf("comparison = %+v", comparison)
	}
	want := []string{"legacy-incomplete", "issue-set-mismatch", "legacy-only-work", "ledger-only-work"}
	for index, code := range want {
		if comparison.Mismatches[index].Code != code {
			t.Fatalf("mismatches = %+v", comparison.Mismatches)
		}
	}
}
