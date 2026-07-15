package task

import "context"

type IssueState string

const (
	IssueOpen   IssueState = "open"
	IssueClosed IssueState = "closed"
)

type State string

const (
	StateReady      State = "ready"
	StateInProgress State = "in_progress"
	StateBlocked    State = "blocked"
	StateDone       State = "done"
)

type Mode string

const (
	ModeTrivial     Mode = "trivial"
	ModeBounded     Mode = "bounded"
	ModeInvestigate Mode = "investigate"
)

type Priority string

const (
	PriorityP0 Priority = "p0"
	PriorityP1 Priority = "p1"
	PriorityP2 Priority = "p2"
	PriorityP3 Priority = "p3"
)

type Issue struct {
	Number int
	Title  string
	URL    string
	Body   string
	State  IssueState
	Labels []string
}

// Task is the single public domain value shared by every Task operation.
type Task struct {
	Number        int        `json:"number"`
	Title         string     `json:"title"`
	URL           string     `json:"url"`
	IssueState    IssueState `json:"issueState"`
	State         State      `json:"state"`
	Mode          *Mode      `json:"mode"`
	Priority      *Priority  `json:"priority"`
	InProgress    bool       `json:"inProgress"`
	Blockers      []string   `json:"blockers"`
	ProjectLabels []string   `json:"projectLabels"`
	Reasons       []string   `json:"reasons"`
	Body          *string    `json:"body,omitempty"`
	BodyTruncated *bool      `json:"bodyTruncated,omitempty"`
}

type ListState string

const (
	ListOpen   ListState = "open"
	ListClosed ListState = "closed"
	ListAll    ListState = "all"
)

type LabelDefinition struct {
	Name        string
	Color       string
	Description string
}

// IssueStore is the complete external seam required by Task commands.
type IssueStore interface {
	ListIssues(context.Context, string, ListState) ([]Issue, error)
	GetIssue(context.Context, string, int) (Issue, error)
	EnsureLabel(context.Context, string, LabelDefinition) (bool, error)
	AddLabel(context.Context, string, int, string) error
	RemoveLabel(context.Context, string, int, string) error
	CloseIssue(context.Context, string, int) error
}
