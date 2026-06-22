package doctor

import "testing"

func TestDoctorCountsAndReadyState(t *testing.T) {
	checks := []Check{
		{Name: "config", Status: "ok"},
		{Name: "github-auth", Status: "warn"},
		{Name: "repo-root", Status: "fail"},
	}
	counts := countChecks(checks)
	if counts.OK != 1 || counts.Warn != 1 || counts.Fail != 1 {
		t.Fatalf("counts = %#v", counts)
	}
	if state := readyState(counts); state != "blocked" {
		t.Fatalf("readyState = %q, want blocked", state)
	}
}
