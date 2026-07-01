package policy

import (
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
)

func TestComputeIssuePolicy(t *testing.T) {
	cfg := config.DefaultConfig()
	tests := []struct {
		name                    string
		body                    string
		currentLabels           []string
		wantFormIssue           bool
		wantAdd                 []string
		wantRemove              []string
		wantMissing             []string
		wantPolicyCommentSubstr string
	}{
		{
			name:          "ready trivial complete",
			body:          issueBody(issueBodyInput{WorkKind: "Enhancement", AgentMode: "Ready trivial", AcceptanceCriteria: "- [ ] The typo is fixed."}),
			wantFormIssue: true,
			wantAdd:       []string{"agent:ready-trivial", "enhancement", "priority:p2"},
			wantRemove:    []string{},
			wantMissing:   []string{},
		},
		{
			name:          "priority p0 adds mapped label",
			body:          issueBody(issueBodyInput{Priority: "P0"}),
			wantFormIssue: true,
			wantAdd:       []string{"agent:ready-trivial", "bug", "priority:p0"},
			wantRemove:    []string{},
			wantMissing:   []string{},
		},
		{
			name:                    "ready bounded missing acceptance criteria",
			body:                    issueBody(issueBodyInput{AgentMode: "Ready bounded", AcceptanceCriteria: "No response"}),
			wantFormIssue:           true,
			wantAdd:                 []string{"agent:ready-bounded", "bug", "needs-info", "priority:p2"},
			wantRemove:              []string{},
			wantMissing:             []string{"Acceptance criteria"},
			wantPolicyCommentSubstr: "<!-- baton-issue-policy:v1 -->",
		},
		{
			name:                    "missing priority adds quality gate",
			body:                    issueBody(issueBodyInput{Priority: "No response"}),
			wantFormIssue:           true,
			wantAdd:                 []string{"agent:ready-trivial", "bug", "needs-info"},
			wantRemove:              []string{},
			wantMissing:             []string{"Priority"},
			wantPolicyCommentSubstr: "<!-- baton-issue-policy:v1 -->",
		},
		{
			name:                    "unknown priority adds quality gate and removes stale priority label",
			body:                    issueBody(issueBodyInput{Priority: "P9"}),
			currentLabels:           []string{"priority:p1"},
			wantFormIssue:           true,
			wantAdd:                 []string{"agent:ready-trivial", "bug", "needs-info"},
			wantRemove:              []string{"priority:p1"},
			wantMissing:             []string{"Priority"},
			wantPolicyCommentSubstr: "<!-- baton-issue-policy:v1 -->",
		},
		{
			name:          "ready bounded complete without allowed scope",
			body:          issueBody(issueBodyInput{AgentMode: "Ready bounded", AcceptanceCriteria: "- [ ] The bounded change is complete."}),
			wantFormIssue: true,
			wantAdd:       []string{"agent:ready-bounded", "bug", "priority:p2"},
			wantRemove:    []string{},
			wantMissing:   []string{},
		},
		{
			name:          "changing agent mode removes previous controlled labels",
			body:          issueBody(issueBodyInput{WorkKind: "Investigation", AgentMode: "Investigate only", AcceptanceCriteria: "No response"}),
			currentLabels: []string{"agent:ready-trivial", "needs-info", "bug", "priority:p1"},
			wantFormIssue: true,
			wantAdd:       []string{"agent:investigate-only", "priority:p2"},
			wantRemove:    []string{"agent:ready-trivial", "bug", "needs-info", "priority:p1"},
			wantMissing:   []string{},
		},
		{
			name:          "investigate only does not require implementation sections",
			body:          issueBody(issueBodyInput{AgentMode: "Investigate only", AcceptanceCriteria: "No response"}),
			wantFormIssue: true,
			wantAdd:       []string{"agent:investigate-only", "bug", "priority:p2"},
			wantRemove:    []string{},
			wantMissing:   []string{},
		},
		{
			name:          "legacy non form issue ignored",
			body:          "A legacy issue body asking an agent to fix a bug.",
			wantFormIssue: false,
			wantAdd:       []string{},
			wantRemove:    []string{},
			wantMissing:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := ComputeIssuePolicy(IssuePolicyInput{
				Body:          tt.body,
				CurrentLabels: tt.currentLabels,
				Policy:        cfg.IssuePolicy,
			})
			assertEqual(t, decision.IsFormIssue, tt.wantFormIssue)
			assertStringSlices(t, decision.LabelsToAdd, tt.wantAdd)
			assertStringSlices(t, decision.LabelsToRemove, tt.wantRemove)
			assertStringSlices(t, decision.MissingRequiredSections, tt.wantMissing)
			if tt.wantPolicyCommentSubstr == "" {
				if decision.PolicyCommentBody != nil {
					t.Fatalf("PolicyCommentBody = %q, want nil", *decision.PolicyCommentBody)
				}
			} else if decision.PolicyCommentBody == nil || !strings.Contains(*decision.PolicyCommentBody, tt.wantPolicyCommentSubstr) {
				t.Fatalf("PolicyCommentBody = %v, want substring %q", decision.PolicyCommentBody, tt.wantPolicyCommentSubstr)
			}
		})
	}
}

func TestParseIssueSections(t *testing.T) {
	sections := ParseIssueSections("intro\n### One\nfirst\n\n### Two\nsecond\n")
	assertEqual(t, sections["One"], "first")
	assertEqual(t, sections["Two"], "second")
}

type issueBodyInput struct {
	WorkKind           string
	AgentMode          string
	Priority           string
	Summary            string
	ContextEvidence    string
	AcceptanceCriteria string
}

func issueBody(input issueBodyInput) string {
	workKind := firstNonEmpty(input.WorkKind, "Bug")
	agentMode := firstNonEmpty(input.AgentMode, "Ready trivial")
	priority := firstNonEmpty(input.Priority, "P2")
	summary := firstNonEmpty(input.Summary, "Make the requested change.")
	contextEvidence := firstNonEmpty(input.ContextEvidence, "The relevant evidence is attached in the issue.")
	acceptanceCriteria := firstNonEmpty(input.AcceptanceCriteria, "- [ ] The requested change is complete.")
	return `### Work kind

` + workKind + `

### Agent mode

` + agentMode + `

### Priority

` + priority + `

### Summary

` + summary + `

### Context / evidence

` + contextEvidence + `

### Acceptance criteria

` + acceptanceCriteria
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertStringSlices(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
