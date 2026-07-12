package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
)

type transitionGitHub struct {
	pr     gh.PullRequest
	issues map[int]gh.Issue
	getErr map[int]error
	addErr map[int]error
	added  []int
}

func (stub *transitionGitHub) GetPullRequestContext(context.Context, string, int) (gh.PullRequest, error) {
	return stub.pr, nil
}

func (stub *transitionGitHub) GetIssueContext(_ context.Context, _ string, number int) (gh.Issue, error) {
	if err := stub.getErr[number]; err != nil {
		return gh.Issue{}, err
	}
	return stub.issues[number], nil
}

func (stub *transitionGitHub) AddIssueLabelsContext(_ context.Context, _ string, number int, _ []string) error {
	stub.added = append(stub.added, number)
	return stub.addErr[number]
}

func TestPullRequestTransitionApplyIsIdempotent(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{
		7: {Number: 7, State: "open", Labels: []string{"needs:review"}},
		8: {Number: 8, State: "closed"},
	}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.added) != 0 || result.Report == nil || result.Report.Status != operation.ReportCompleted {
		t.Fatalf("added=%v report=%+v", stub.added, result.Report)
	}
	for _, result := range result.Report.Operations {
		if result.Status != operation.StatusUnchanged {
			t.Fatalf("operation = %+v", result)
		}
	}
}

func TestPullRequestTransitionPreflightsAllIssuesBeforeWriting(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}}, getErr: map[int]error{8: errors.New("boom")}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || len(stub.added) != 0 || result.Report == nil || result.Report.Status != operation.ReportFailed {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionReportsPartialApply(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}, 8: {Number: 8, State: "open"}}, addErr: map[int]error{8: errors.New("boom")}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportPartial || len(stub.added) != 2 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionRefusesStaleEvent(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7")
	latest.HeadSHA = "new-head"
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.added) != 0 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionRefusesEditedReferences(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7")
	latest.Body = "Refs #8"
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.added) != 0 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func TestPullRequestTransitionRefusesReferencedPullRequest(t *testing.T) {
	input, latest := transitionFixture(t, "Refs #7 and #8")
	stub := &transitionGitHub{pr: latest, issues: map[int]gh.Issue{7: {Number: 7, State: "open"}, 8: {Number: 8, State: "open", PullRequest: true}}}
	workflow := PullRequestTransitionWorkflow{newClient: func(context.Context, PullRequestTransitionInput) (PullRequestTransitionGitHub, error) {
		return stub, nil
	}}
	result, err := workflow.RunContext(context.Background(), input)
	if err == nil || result.Report == nil || result.Report.Status != operation.ReportRefused || len(stub.added) != 0 {
		t.Fatalf("err=%v added=%v report=%+v", err, stub.added, result.Report)
	}
}

func transitionFixture(t *testing.T, body string) (PullRequestTransitionInput, gh.PullRequest) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	configContent, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "baton.yml")
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "event.json")
	event := `{"action":"closed","repository":{"full_name":"example/repo"},"pull_request":{"number":42,"title":"work","body":"` + body + `","state":"closed","merged":true,"base":{"ref":"agent","sha":"base","repo":{"full_name":"example/repo"}},"head":{"ref":"agent-work/42","sha":"head","repo":{"full_name":"example/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	input := PullRequestTransitionInput{EventPath: eventPath, ConfigPath: configPath, Apply: true}
	latest := gh.PullRequest{Number: 42, Title: "work", Body: body, BaseRef: "agent", HeadRef: "agent-work/42", HeadSHA: "head", State: "closed", Merged: true}
	return input, latest
}
