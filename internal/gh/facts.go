package gh

import "time"

type Issue struct {
	Number       int
	NodeID       string
	Title        string
	URL          string
	Body         string
	Labels       []string
	State        string
	PullRequest  bool
	Locked       bool
	CommentCount int
}

type PullRequest struct {
	Number                 int
	NodeID                 string
	Title                  string
	URL                    string
	Body                   string
	BaseRef                string
	BaseSHA                string
	HeadRef                string
	HeadSHA                string
	BaseRepositoryFullName string
	HeadRepositoryFullName string
	CheckState             string
	Draft                  bool
	Author                 Actor
	Mergeable              string
	MergeState             string
	State                  string
	Merged                 bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
	MergedAt               time.Time
	MergeRevision          string
}

type RepositoryIdentity struct {
	Host     string `json:"host"`
	FullName string `json:"fullName"`
	NodeID   string `json:"nodeId"`
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
	Context       string `json:"context"`
	IntegrationID int64  `json:"integrationId"`
}

type BranchRules struct {
	Branch                       string          `json:"branch"`
	RequiredChecks               []RequiredCheck `json:"requiredChecks"`
	RequiredApprovingReviewCount int             `json:"requiredApprovingReviewCount"`
	StrictRequiredChecks         bool            `json:"strictRequiredChecks"`
	DismissStaleReviews          bool            `json:"dismissStaleReviews"`
	RequireLastPushApproval      bool            `json:"requireLastPushApproval"`
	RequiredLinearHistory        bool            `json:"requiredLinearHistory"`
	MergeQueueEnabled            bool            `json:"mergeQueueEnabled"`
	AllowedMergeMethods          []string        `json:"allowedMergeMethods,omitempty"`
	AllowedMergeMethodsSet       bool            `json:"allowedMergeMethodsSet"`
}

type RepositorySettings struct {
	AllowMergeCommit bool `json:"allowMergeCommit"`
	AllowSquashMerge bool `json:"allowSquashMerge"`
	AllowRebaseMerge bool `json:"allowRebaseMerge"`
}

type Branch struct {
	Ref                  string          `json:"ref"`
	SHA                  string          `json:"sha"`
	Protected            bool            `json:"protected"`
	LegacyRequiredChecks []RequiredCheck `json:"legacyRequiredChecks"`
}

type BranchHealth struct {
	Ref        string
	SHA        string
	CheckState string
}

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type IssueComment struct {
	ID        int64
	NodeID    string
	IssueURL  string
	Body      string
	Author    Actor
	CreatedAt time.Time
	UpdatedAt time.Time
}

type IssueCommentListing struct {
	Comments []IssueComment
	// Complete is false when the requested bounded comment window was not fully
	// acquired.
	Complete bool
}

type PullRequestListing struct {
	PullRequests []PullRequest
	// Complete is false when the bounded result reached its acquisition cap.
	Complete bool
}

type CommitComparison struct {
	Status       string
	AheadBy      int
	BehindBy     int
	TotalCommits int
	MergeBaseSHA string
}

type PullRequestEvent struct {
	Action                 string
	Number                 int
	NodeID                 string
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
	MergedAt               time.Time
	MergeRevision          string
}

type CommitListing struct {
	Messages []string
	Count    int
	// GitHubCapReached is true only for a successful listing that reached
	// GitHub's 250-commit pull-request limit. Baton cancellation, deadlines, and
	// page failures return an error and no partial listing.
	GitHubCapReached bool
}
