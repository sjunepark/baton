package gh

import "time"

type Issue struct {
	Number      int
	Title       string
	URL         string
	Body        string
	Labels      []string
	State       string
	PullRequest bool
}

type PullRequest struct {
	Number     int
	Title      string
	URL        string
	Body       string
	BaseRef    string
	BaseSHA    string
	HeadRef    string
	HeadSHA    string
	CheckState string
	Draft      bool
	Author     Actor
	Mergeable  string
	MergeState string
	State      string
	Merged     bool
}

type Actor struct {
	Login string
	Type  string
}

type ReviewRequest struct {
	Kind  string
	Login string
	Team  string
}

type PullRequestReview struct {
	ID          int64
	State       string
	CommitSHA   string
	SubmittedAt time.Time
	Author      Actor
}

type RequiredCheck struct {
	Context       string
	IntegrationID int64
}

type BranchRules struct {
	Branch                       string
	RequiredChecks               []RequiredCheck
	RequiredApprovingReviewCount int
	StrictRequiredChecks         bool
	DismissStaleReviews          bool
	RequireLastPushApproval      bool
}

type Branch struct {
	Ref                  string
	SHA                  string
	Protected            bool
	LegacyRequiredChecks []RequiredCheck
}

type BranchHealth struct {
	Ref        string
	SHA        string
	CheckState string
}

type ReferencedIssue struct {
	Number int
	Labels []string
}

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type IssueComment struct {
	ID   int64
	Body string
}

type PullRequestEvent struct {
	Action                 string
	Number                 int
	Title                  string
	Body                   string
	BaseRef                string
	HeadRef                string
	BaseRepositoryFullName string
	HeadRepositoryFullName string
	BaseSHA                string
	HeadSHA                string
	State                  string
	Merged                 bool
}

type CommitListing struct {
	Messages []string
	Count    int
	// GitHubCapReached is true only for a successful listing that reached
	// GitHub's 250-commit pull-request limit. Baton cancellation, deadlines, and
	// page failures return an error and no partial listing.
	GitHubCapReached bool
}
