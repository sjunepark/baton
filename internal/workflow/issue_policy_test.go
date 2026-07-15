package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

type issuePolicyGitHub struct {
	added     []string
	removed   []string
	comments  []gh.IssueComment
	updatedID int64
	updated   string
	created   string
	issue     gh.Issue
	issueErr  error
	listErr   error
	removeErr error
}

func (f *issuePolicyGitHub) GetIssueContext(context.Context, string, int) (gh.Issue, error) {
	return f.issue, f.issueErr
}

func (f *issuePolicyGitHub) AddIssueLabelsContext(_ context.Context, _ string, _ int, labels []string) error {
	f.added = append(f.added, labels...)
	return nil
}
func (f *issuePolicyGitHub) RemoveIssueLabelContext(_ context.Context, _ string, _ int, label string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, label)
	return nil
}

func TestApplyIssueDecisionPreservesPartialOperationReport(t *testing.T) {
	client := &issuePolicyGitHub{removeErr: errors.New("remove failed")}
	decision := policy.IssuePolicyDecision{IsFormIssue: true, LabelsToAdd: []string{"bug"}, LabelsToRemove: []string{"needs-info"}}
	report, _, err := applyIssueDecisionWithOwnershipContext(context.Background(), client, "example/repo", gh.Issue{Number: 12, NodeID: "I_12"}, decision, "<!-- baton-policy -->", "needs-info", ownershipRepair{})
	if err == nil {
		t.Fatal("expected removal failure")
	}
	if report.Status != "partial" || len(report.Operations) != 3 || report.Operations[0].ID != "issue-ownership-record" || report.Operations[2].Status != "failed" {
		t.Fatalf("report = %+v", report)
	}
}
func (f *issuePolicyGitHub) ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error) {
	return f.comments, f.listErr
}

func TestApplyIssueDecisionReadsBeforeWriting(t *testing.T) {
	client := &issuePolicyGitHub{listErr: errors.New("comments unavailable")}
	decision := policy.IssuePolicyDecision{
		IsFormIssue: true, LabelsToAdd: []string{"bug"}, LabelsToRemove: []string{"needs-info"},
	}
	report, _, err := applyIssueDecisionWithOwnershipContext(context.Background(), client, "example/repo", gh.Issue{Number: 12, NodeID: "I_12"}, decision, "<!-- baton-policy -->", "needs-info", ownershipRepair{})
	if err == nil {
		t.Fatal("expected comment read failure")
	}
	if report.Status != "failed" || len(report.Operations) != 1 || report.Operations[0].Status != "failed" {
		t.Fatalf("preflight report = %+v", report)
	}
	if len(client.added) != 0 || len(client.removed) != 0 || client.updated != "" || client.created != "" {
		t.Fatalf("writes after read failure: added=%v removed=%v updated=%q created=%q", client.added, client.removed, client.updated, client.created)
	}
}

func TestOperationFailureUsesTypedRetryability(t *testing.T) {
	for _, test := range []struct {
		name      string
		retryable bool
	}{
		{name: "permanent"},
		{name: "transient", retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := operationFailure("github", "mutation failed", &apperror.Error{Category: apperror.GitHub, Retryable: test.retryable})
			if failure.Retryable != test.retryable {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}
func (f *issuePolicyGitHub) UpdateIssueCommentContext(_ context.Context, _ string, id int64, body string) error {
	f.updatedID, f.updated = id, body
	return nil
}
func (f *issuePolicyGitHub) CreateIssueCommentContext(_ context.Context, _ string, _ int, body string) error {
	f.created = body
	return nil
}

func TestApplyIssueDecisionOwnsMutationSequence(t *testing.T) {
	client := &issuePolicyGitHub{comments: []gh.IssueComment{{ID: 99, Body: "<!-- baton-policy -->\nold"}}}
	decision := policy.IssuePolicyDecision{
		IsFormIssue: true, LabelsToAdd: []string{"bug"}, LabelsToRemove: []string{"needs-info"},
	}
	if _, _, err := applyIssueDecisionWithOwnershipContext(context.Background(), client, "example/repo", gh.Issue{Number: 12, NodeID: "I_12"}, decision, "<!-- baton-policy -->", "needs-info", ownershipRepair{}); err != nil {
		t.Fatal(err)
	}
	if len(client.added) != 1 || client.added[0] != "bug" || len(client.removed) != 1 || client.removed[0] != "needs-info" {
		t.Fatalf("labels added=%v removed=%v", client.added, client.removed)
	}
	if client.updatedID != 99 || client.updated == "" || client.created == "" || !strings.Contains(client.created, "baton-managed-issue:v1") {
		t.Fatalf("comment update id=%d body=%q created=%q", client.updatedID, client.updated, client.created)
	}
}

func TestIssuePolicyRepairsEditedTrustedOwnershipRecordAfterBodyEdit(t *testing.T) {
	client := &issuePolicyGitHub{comments: []gh.IssueComment{{
		ID: 88, Body: "<!-- baton-managed-issue:v1 malformed -->", Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"},
	}}}
	issue := gh.Issue{Number: 12, NodeID: "I_12", Body: "body no longer matches the issue form"}
	report, ownership, err := applyIssueDecisionWithOwnershipContext(
		context.Background(), client, "example/repo", issue, policy.IssuePolicyDecision{}, "<!-- baton-policy -->", "needs-info",
		ownershipRepair{Requested: true, CommentID: 88, Action: "edited", ExpectedBody: "<!-- baton-managed-issue:v1 malformed -->"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ownership.Managed || ownership.Source != policy.IssueOwnershipRecord || report.Status != "completed" || client.updatedID != 88 || !strings.Contains(client.updated, "baton-managed-issue:v1") || !containsLabel(client.added, policy.ManagedIssueIndexLabel) {
		t.Fatalf("ownership=%+v report=%+v updated=%q added=%v", ownership, report, client.updated, client.added)
	}
}

func TestOwnershipRepairFromEvent(t *testing.T) {
	for _, test := range []struct {
		name      string
		event     gh.IssueEvent
		requested bool
	}{
		{name: "marker removed", requested: true, event: gh.IssueEvent{
			Action: "edited", CommentID: 88, CommentBody: "marker removed", CommentPreviousBody: "<!-- baton-managed-issue:v1 original -->", Labels: []string{policy.ManagedIssueIndexLabel},
			CommentAuthor: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"},
		}},
		{name: "unrelated bot comment", event: gh.IssueEvent{
			Action: "edited", CommentID: 88, CommentBody: "routine automation note", Labels: []string{policy.ManagedIssueIndexLabel},
			CommentAuthor: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"},
		}},
		{name: "untrusted marker", event: gh.IssueEvent{
			Action: "edited", CommentID: 88, CommentBody: "<!-- baton-managed-issue:v1 malformed -->",
			CommentAuthor: gh.Actor{Login: "contributor", Type: "User"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repair := ownershipRepairFromEvent(test.event)
			if repair.Requested != test.requested || (test.requested && (repair.CommentID != 88 || repair.ExpectedBody != test.event.CommentBody || repair.Action != test.event.Action)) {
				t.Fatalf("repair = %+v", repair)
			}
		})
	}
}

func TestIssuePolicyRefusesStaleOwnershipRepairBody(t *testing.T) {
	client := &issuePolicyGitHub{comments: []gh.IssueComment{{
		ID: 88, Body: "newer comment revision", Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"},
	}}}
	report, _, err := applyIssueDecisionWithOwnershipContext(
		context.Background(), client, "example/repo", gh.Issue{Number: 12, NodeID: "I_12", Labels: []string{policy.ManagedIssueIndexLabel}},
		policy.IssuePolicyDecision{}, "<!-- baton-policy -->", "needs-info",
		ownershipRepair{Requested: true, CommentID: 88, Action: "edited", ExpectedBody: "marker removed"},
	)
	if err == nil || report.Status != operation.ReportRefused || client.updated != "" || client.created != "" || len(client.added) != 0 {
		t.Fatalf("report=%+v err=%v updated=%q created=%q added=%v", report, err, client.updated, client.created, client.added)
	}
}

func TestIssuePolicyBodyEditDoesNotRevokeDurableOwnership(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"action":"edited","repository":{"full_name":"example/repo"},"issue":{"number":12,"node_id":"I_12","body":"body no longer matches the form","labels":[{"name":"baton:managed"}]}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	record := policy.NewManagedIssueRecord("I_12", 12)
	client := &issuePolicyGitHub{
		issue:    gh.Issue{Number: 12, NodeID: "I_12", Body: "body no longer matches the form", Labels: []string{"baton:managed"}},
		comments: []gh.IssueComment{{ID: 88, Body: policy.RenderManagedIssueRecord(record), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}},
	}
	workflow := IssuePolicyWorkflow{newClient: func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error) { return client, nil }}
	result, err := workflow.Run(IssuePolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.IsFormIssue || !result.Ownership.Managed || result.Ownership.Source != policy.IssueOwnershipRecord || len(result.Report.Operations) != 0 || client.created != "" || client.updated != "" || len(client.added) != 0 {
		t.Fatalf("result=%+v created=%q updated=%q added=%v", result, client.created, client.updated, client.added)
	}
}

func TestIssuePolicyWorkflowEvaluatesBodyWithoutConstructingGitHubClient(t *testing.T) {
	dir := t.TempDir()
	configPath := writeWorkflowConfig(t, dir)
	bodyPath := filepath.Join(dir, "issue.md")
	if err := os.WriteFile(bodyPath, []byte("plain issue body"), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := IssuePolicyWorkflow{newClient: func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error) {
		t.Fatal("dry-run body evaluation must not construct a GitHub client")
		return nil, nil
	}}
	result, err := workflow.Run(IssuePolicyInput{BodyPath: bodyPath, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Evaluated || result.Decision.IsFormIssue {
		t.Fatalf("result = %+v", result)
	}
}

func TestIssuePolicyWorkflowRejectsApplyWithoutEventBeforeClient(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "issue.md")
	if err := os.WriteFile(bodyPath, []byte("plain issue body"), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := IssuePolicyWorkflow{newClient: func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error) {
		t.Fatal("invalid apply must not construct a GitHub client")
		return nil, nil
	}}
	result, err := workflow.Run(IssuePolicyInput{BodyPath: bodyPath, ConfigPath: writeWorkflowConfig(t, dir), Apply: true})
	if err == nil || !result.Evaluated {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIssuePolicyWorkflowRejectsRepositoryMismatchBeforeClient(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"issue":{"number":12,"body":"plain body","labels":[]},"repository":{"full_name":"event/repo"}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := IssuePolicyWorkflow{newClient: func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error) {
		t.Fatal("repository mismatch must not construct a GitHub client")
		return nil, nil
	}}
	result, err := workflow.Run(IssuePolicyInput{
		EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), Apply: true, Repository: "flag/repo",
	})
	if err == nil || !result.Evaluated {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIssuePolicyWorkflowRefusesStaleEventBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	body := "### Work kind\nBug\n\n### Agent mode\nReady trivial\n\n### Summary\nDo it\n\n### Context / evidence\nEvidence\n\n### Acceptance criteria\nDone\n"
	eventPath := filepath.Join(dir, "event.json")
	event := `{"issue":{"number":12,"body":` + strconv.Quote(body) + `,"labels":[]},"repository":{"full_name":"example/repo"}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &issuePolicyGitHub{issue: gh.Issue{Number: 12, Body: body, Labels: []string{"manual:hold"}}}
	workflow := IssuePolicyWorkflow{newClient: func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error) { return client, nil }}
	result, err := workflow.Run(IssuePolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), Apply: true, Repository: "example/repo"})
	if err == nil || result.Report == nil || result.Report.Status != "refused" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(client.added) != 0 || len(client.removed) != 0 || client.created != "" || client.updated != "" {
		t.Fatalf("stale event caused mutations: %+v", client)
	}
}

func TestIssueStatePreflightAllowsIdempotentPartialProgress(t *testing.T) {
	decision := policy.IssuePolicyDecision{LabelsToAdd: []string{"bug", "agent:ready"}, LabelsToRemove: []string{"needs-info"}}
	if !issueStateCompatibleWithDecision("body", []string{"needs-info"}, gh.Issue{Body: "body", Labels: []string{"bug"}}, decision) {
		t.Fatal("Baton-authorized partial progress was treated as stale")
	}
	if issueStateCompatibleWithDecision("body", []string{"needs-info"}, gh.Issue{Body: "body", Labels: []string{"manual:hold"}}, decision) {
		t.Fatal("unrelated label mutation was accepted")
	}
}

func writeWorkflowConfig(t *testing.T, dir string) string {
	t.Helper()
	content, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
