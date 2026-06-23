package doctor

import "testing"

func TestDoctorCountsAndReadyState(t *testing.T) {
	tests := []struct {
		name       string
		checks     []Check
		wantCounts Counts
		wantState  string
	}{
		{
			name: "failures_block_ready_state",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "warn"},
				{Name: "repo-root", Status: "fail"},
			},
			wantCounts: Counts{OK: 1, Warn: 1, Fail: 1},
			wantState:  "blocked",
		},
		{
			name: "warnings_degrade_ready_state",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "warn"},
			},
			wantCounts: Counts{OK: 1, Warn: 1},
			wantState:  "degraded",
		},
		{
			name: "all_ok_is_ready",
			checks: []Check{
				{Name: "config", Status: "ok"},
				{Name: "github-auth", Status: "ok"},
			},
			wantCounts: Counts{OK: 2},
			wantState:  "ready",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := countChecks(tt.checks)
			if counts != tt.wantCounts {
				t.Fatalf("counts = %#v, want %#v", counts, tt.wantCounts)
			}
			if state := readyState(counts); state != tt.wantState {
				t.Fatalf("readyState = %q, want %q", state, tt.wantState)
			}
		})
	}
}
