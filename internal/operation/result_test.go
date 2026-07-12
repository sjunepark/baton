package operation

import (
	"encoding/json"
	"testing"
)

func TestNewReportDerivesOverallStatus(t *testing.T) {
	for _, test := range []struct {
		name    string
		results []Result
		want    ReportStatus
	}{
		{name: "completed", results: []Result{{Status: StatusApplied}, {Status: StatusUnchanged}}, want: ReportCompleted},
		{name: "refused", results: []Result{{Status: StatusRefused}, {Status: StatusNotAttempted}}, want: ReportRefused},
		{name: "failed", results: []Result{{Status: StatusFailed}}, want: ReportFailed},
		{name: "partial", results: []Result{{Status: StatusApplied}, {Status: StatusFailed}}, want: ReportPartial},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := NewReport(test.results).Status; got != test.want {
				t.Fatalf("status = %s, want %s", got, test.want)
			}
		})
	}
}

func TestReportJSONContract(t *testing.T) {
	report := NewReport([]Result{{
		ID: "label-001", Resource: "needs-info", Action: "create", Status: StatusFailed,
		Error: &Failure{Category: "github", Message: "label mutation failed", Retryable: true},
	}})
	content, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":1,"kind":"operationReport","status":"failed","operations":[{"id":"label-001","resource":"needs-info","action":"create","status":"failed","error":{"category":"github","message":"label mutation failed","retryable":true}}]}`
	if string(content) != want {
		t.Fatalf("JSON = %s\nwant = %s", content, want)
	}
}
