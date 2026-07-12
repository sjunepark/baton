package operation

type ReportStatus string

const (
	ReportCompleted ReportStatus = "completed"
	ReportRefused   ReportStatus = "refused"
	ReportPartial   ReportStatus = "partial"
	ReportFailed    ReportStatus = "failed"
)

type Status string

const (
	StatusApplied      Status = "applied"
	StatusUnchanged    Status = "unchanged"
	StatusRefused      Status = "refused"
	StatusFailed       Status = "failed"
	StatusNotAttempted Status = "not_attempted"
)

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	Kind          string       `json:"kind"`
	Status        ReportStatus `json:"status"`
	Operations    []Result     `json:"operations"`
}

type Result struct {
	ID       string   `json:"id"`
	Resource string   `json:"resource"`
	Action   string   `json:"action"`
	Status   Status   `json:"status"`
	Error    *Failure `json:"error,omitempty"`
}

type Failure struct {
	Category  string `json:"category"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func NewReport(results []Result) Report {
	if results == nil {
		results = []Result{}
	}
	return Report{SchemaVersion: 1, Kind: "operationReport", Status: deriveStatus(results), Operations: results}
}

func deriveStatus(results []Result) ReportStatus {
	applied, failed, refused := false, false, false
	for _, result := range results {
		switch result.Status {
		case StatusApplied:
			applied = true
		case StatusFailed:
			failed = true
		case StatusRefused:
			refused = true
		}
	}
	switch {
	case applied && (failed || refused):
		return ReportPartial
	case failed:
		return ReportFailed
	case refused:
		return ReportRefused
	default:
		return ReportCompleted
	}
}
