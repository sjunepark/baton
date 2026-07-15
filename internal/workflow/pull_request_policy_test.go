package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/policy"
)

type pullRequestPolicyGitHub struct {
	*deliveryRecordGitHubStub
	repo           string
	issueNumbers   []int
	prNumber       int
	issues         map[int]gh.Issue
	omitOwnership  map[int]bool
	currentHeadSHA string
	current        gh.PullRequest
}

func (f *pullRequestPolicyGitHub) GetPullRequestContext(_ context.Context, _ string, number int) (gh.PullRequest, error) {
	if f.deliveryRecordGitHubStub != nil {
		if pullRequest, exists := f.deliveryRecordGitHubStub.pull[number]; exists {
			return pullRequest, nil
		}
	}
	if f.current.Number != 0 {
		current := f.current
		if f.currentHeadSHA != "" {
			current.HeadSHA = f.currentHeadSHA
		}
		return current, nil
	}
	headSHA := f.currentHeadSHA
	if headSHA == "" {
		headSHA = "head-sha"
	}
	return gh.PullRequest{Number: number, HeadSHA: headSHA}, nil
}

func policyClientForEvent(t *testing.T, raw string, client *pullRequestPolicyGitHub) *pullRequestPolicyGitHub {
	t.Helper()
	event, err := gh.ParsePullRequestEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	client.current = gh.PullRequest{
		Number: event.Number, NodeID: event.NodeID, Title: event.Title, Body: event.Body,
		BaseRef: event.BaseRef, HeadRef: event.HeadRef, BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA,
		BaseRepositoryFullName: event.BaseRepositoryFullName, HeadRepositoryFullName: event.HeadRepositoryFullName,
	}
	return client
}

func (f *pullRequestPolicyGitHub) GetIssueContext(_ context.Context, repo string, issueNumber int) (gh.Issue, error) {
	f.repo, f.issueNumbers = repo, append(f.issueNumbers, issueNumber)
	if f.deliveryRecordGitHubStub != nil {
		if issue, exists := f.deliveryRecordGitHubStub.issues[issueNumber]; exists {
			return issue, nil
		}
	}
	if issue, exists := f.issues[issueNumber]; exists {
		return issue, nil
	}
	return gh.Issue{NodeID: fmt.Sprintf("I_%d", issueNumber), Number: issueNumber, Labels: []string{"agent:ready-trivial", policy.ManagedIssueIndexLabel}}, nil
}

func (f *pullRequestPolicyGitHub) ListIssueCommentsContext(_ context.Context, _ string, issueNumber int) ([]gh.IssueComment, error) {
	if f.deliveryRecordGitHubStub != nil {
		if comments, exists := f.deliveryRecordGitHubStub.comments[issueNumber]; exists {
			return comments, nil
		}
	}
	if f.omitOwnership[issueNumber] {
		return nil, nil
	}
	record := policy.NewManagedIssueRecord(fmt.Sprintf("I_%d", issueNumber), issueNumber)
	return []gh.IssueComment{{ID: int64(issueNumber), Body: policy.RenderManagedIssueRecord(record), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}}, nil
}

func TestPullRequestPolicyFailsClosedWhenManagedIndexRecordIsMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.PRPolicy.FailWhenCommitListingReachesCap = false
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	issue := gh.Issue{
		Number: 7, NodeID: "I_7", Labels: []string{policy.ManagedIssueIndexLabel},
		Body: "### Work kind\n\nBug\n\n### Agent mode\n\nReady bounded\n\n### Summary\n\nImplement it.",
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{issues: map[int]gh.Issue{7: issue}, omitOwnership: map[int]bool{7: true}})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "managed-issue index exists without its trusted ownership record") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestPullRequestPolicyRejectsPullRequestAsReferencedIssue(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{issues: map[int]gh.Issue{7: {Number: 7, NodeID: "PR_7", PullRequest: true}}})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "reference resolves to a pull request, not an issue") {
		t.Fatalf("decision = %+v", decision)
	}
}

func (f *pullRequestPolicyGitHub) FetchCommitListingContext(_ context.Context, repo string, prNumber int) (gh.CommitListing, error) {
	f.repo, f.prNumber = repo, prNumber
	return gh.CommitListing{Messages: []string{"Implement policy"}, Count: 250, GitHubCapReached: true}, nil
}

func TestPullRequestPolicyWorkflowAcquiresEventFacts(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "baton.yml")
	configContent, err := config.MarshalYAML(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if client.repo != "event/repo" || client.prNumber != 12 || len(client.issueNumbers) != 1 || client.issueNumbers[0] != 7 {
		t.Fatalf("repo=%q pr=%d issues=%v", client.repo, client.prNumber, client.issueNumbers)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "commit listing reached") {
		t.Fatalf("decision errors = %v", decision.Errors)
	}
}

func TestPullRequestPolicyWorkflowUsesConfiguredReferenceKeyword(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.PRPolicy.RequiredReferenceKeyword = "Tracks"
	configContent, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(configPath, configContent, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Tracks #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	if _, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"}); err != nil {
		t.Fatal(err)
	}
	if len(client.issueNumbers) != 1 || client.issueNumbers[0] != 7 {
		t.Fatalf("issues = %v", client.issueNumbers)
	}
}

func TestPullRequestPolicyWorkDecisionIgnoresMutableIssueBodyAndLabelsAfterOwnership(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.PRPolicy.FailWhenCommitListingReachesCap = false
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "baton.yml")
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	variants := []gh.Issue{
		{Number: 7, NodeID: "I_7", Body: "### Work kind\n\nBug", Labels: []string{"agent:ready-bounded"}},
		{Number: 7, NodeID: "I_7", Body: "body and headings replaced after the PR opened", Labels: []string{"needs:discussion", policy.ManagedIssueIndexLabel}},
	}
	var baseline policy.PRPolicyDecision
	for index, issue := range variants {
		client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{issues: map[int]gh.Issue{7: issue}})
		workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
		decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "event/repo"})
		if err != nil {
			t.Fatal(err)
		}
		if len(decision.Errors) != 0 {
			t.Fatalf("variant %d errors = %v", index, decision.Errors)
		}
		if index == 0 {
			baseline = decision
		} else if !reflect.DeepEqual(decision, baseline) {
			t.Fatalf("decision changed after issue body/label edit:\nbefore=%+v\nafter=%+v", baseline, decision)
		}
	}
}

func TestPullRequestPolicyFetchesOnlyReferencedIssues(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Refs #7\n\nCloses #999","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"head-sha","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.issueNumbers, []int{7}) || !strings.Contains(strings.Join(decision.Errors, "\n"), "not closing keywords") {
		t.Fatalf("issues=%v decision=%+v", client.issueNumbers, decision)
	}
}

func TestPullRequestPolicyRefusesCommitFactsFromNewerHeadRevision(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","sha":"event-head","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	client := policyClientForEvent(t, event, &pullRequestPolicyGitHub{currentHeadSHA: "newer-head"})
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	_, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.GitHub || !strings.Contains(applicationError.Message, "changed before policy acquisition") {
		t.Fatalf("error = %+v", applicationError)
	}
}

func TestPullRequestPolicyRejectsMissingManagedRevisionBeforeClient(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/7-work","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		t.Fatal("missing managed revision must fail before constructing a GitHub client")
		return nil, nil
	}}
	_, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo"})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Policy || !strings.Contains(applicationError.Message, "revision is incomplete") {
		t.Fatalf("error = %+v", applicationError)
	}
}

func TestPullRequestPolicyWorkflowDerivesExpectedPromotionIssues(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99})
	if err != nil {
		t.Fatalf("error = %+v", apperror.As(err))
	}
	if decision.PromotionFacts == nil || !decision.PromotionFacts.Complete || len(decision.PromotionFacts.ExpectedIssues) != 2 || decision.PromotionFacts.ExpectedIssues[0] != 7 || decision.PromotionFacts.ExpectedIssues[1] != 8 {
		t.Fatalf("promotion facts = %+v", decision.PromotionFacts)
	}
	if decision.PromotionFacts.Source != "sealedDeliveryPlan" || decision.PromotionFacts.PlanDigest == "" || len(decision.PromotionFacts.IncludedWorkPullRequests) != 1 || decision.PromotionFacts.IncludedWorkPullRequests[0] != 20 {
		t.Fatalf("promotion seal facts = %+v", decision.PromotionFacts)
	}
	if !strings.Contains(strings.Join(decision.Errors, "\n"), "missing: #8") {
		t.Fatalf("decision errors = %v", decision.Errors)
	}
	if len(client.created) != 1 || len(client.updated) != 1 {
		t.Fatalf("seal writes = created %d updated %d", len(client.created), len(client.updated))
	}
}

func TestSealedPromotionRerunRechecksRevisionAndIntegration(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	if _, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	sealedRevision := client.deliveryRecordGitHubStub.pull[50]
	advanced := sealedRevision
	advanced.BaseSHA = strings.Repeat("6", 40)
	client.deliveryRecordGitHubStub.pull[50] = advanced
	_, err = sealPromotionPolicy(context.Background(), client, "example/repo", cfg, gh.PullRequestEvent{Number: 50}, sealedRevision, delivery.WriterProvenance{Workflow: "PR Policy", RunID: 100})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Policy || !strings.Contains(applicationError.Message, "revision changed") {
		t.Fatalf("error = %+v", applicationError)
	}
}

func TestPullRequestPolicyWorkflowUsesCurrentPromotionBody(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	current := client.deliveryRecordGitHubStub.pull[50]
	current.Body = "Closes #8"
	client.deliveryRecordGitHubStub.pull[50] = current
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99})
	if err != nil {
		t.Fatal(err)
	}
	errorsText := strings.Join(decision.Errors, "\n")
	if !strings.Contains(errorsText, "missing: #7") || strings.Contains(errorsText, "missing: #8") {
		t.Fatalf("decision used stale webhook prose: %+v", decision)
	}
}

func TestPullRequestPolicyWorkflowFailsClosedWhenDeliveryRecordIsDelayed(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, false)
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	_, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Policy || !strings.Contains(applicationError.Message, "incomplete") {
		t.Fatalf("error = %+v cause=%v", applicationError, applicationError.Cause)
	}
}

func TestPullRequestPolicyWorkflowRefusesShadowAuthority(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Delivery.Authority = config.DeliveryAuthorityShadow
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	_, err = workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Config || !strings.Contains(applicationError.Message, "sealed delivery authority") || len(client.created) != 0 || len(client.updated) != 0 {
		t.Fatalf("error=%+v created=%d updated=%d", applicationError, len(client.created), len(client.updated))
	}
}

func TestPullRequestPolicyWorkflowInvalidatesPriorCheckWhenPromotionsOverlap(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	other := client.deliveryRecordGitHubStub.pull[50]
	other.Number, other.NodeID = 51, "PR_51"
	client.deliveryRecordGitHubStub.pull[51] = other
	client.deliveryRecordGitHubStub.open.PullRequests = append(client.deliveryRecordGitHubStub.open.PullRequests, other)
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	_, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Policy || !strings.Contains(applicationError.Message, "exactly one") || !reflect.DeepEqual(client.rerequest, []int64{700}) {
		t.Fatalf("error=%+v rerequest=%v", applicationError, client.rerequest)
	}
}

func TestPullRequestPolicyWorkflowInvalidatesActivePlanWhenOpenListingIsIncomplete(t *testing.T) {
	configPath, eventPath, client := promotionPolicyLedgerFixture(t, true)
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) { return client, nil }}
	if _, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 99}); err != nil {
		t.Fatal(err)
	}
	second := client.deliveryRecordGitHubStub.pull[50]
	second.Number, second.NodeID = 51, "PR_51"
	client.deliveryRecordGitHubStub.pull[51] = second
	client.deliveryRecordGitHubStub.open = gh.PullRequestListing{PullRequests: []gh.PullRequest{second}, Complete: false}
	event := fmt.Sprintf(`{"action":"opened","pull_request":{"number":51,"node_id":"PR_51","title":"Promote","body":"Closes #7","state":"open","base":{"ref":"%s","sha":"%s","repo":{"full_name":"example/repo"}},"head":{"ref":"%s","sha":"%s","repo":{"full_name":"example/repo"}}},"repository":{"full_name":"example/repo"}}`, second.BaseRef, second.BaseSHA, second.HeadRef, second.HeadSHA)
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: configPath, EnvironmentRepo: "example/repo", WorkflowName: "PR Policy", RunID: 100})
	applicationError := apperror.As(err)
	if applicationError == nil || applicationError.Category != apperror.Policy || !strings.Contains(applicationError.Message, "complete bounded") || !reflect.DeepEqual(client.rerequest, []int64{700}) {
		t.Fatalf("error=%+v rerequest=%v", applicationError, client.rerequest)
	}
}

func TestConcurrentPromotionInvalidationContinuesAfterEarlierFailure(t *testing.T) {
	transport := &deliveryRecordGitHubStub{
		checkErrors:  map[int]error{51: errors.New("rollup unavailable")},
		checkRollups: map[int]gh.CheckRollup{52: {Complete: true, Checks: []gh.CheckState{{ID: 702, Name: deliveryPolicyCheckName, Status: "completed", Conclusion: "success"}}}},
	}
	client := &pullRequestPolicyGitHub{deliveryRecordGitHubStub: transport}
	err := rerequestConcurrentPromotionChecks(context.Background(), client, "example/repo", []gh.PullRequest{{Number: 51, HeadSHA: "one"}, {Number: 52, HeadSHA: "two"}}, 50)
	if err == nil || !strings.Contains(err.Error(), "#51") || !reflect.DeepEqual(transport.rerequest, []int64{702}) {
		t.Fatalf("error=%v rerequest=%v", err, transport.rerequest)
	}
}

func TestPromotionOwnershipProjectionIncludesExcludedWork(t *testing.T) {
	ownership7 := policy.NewManagedIssueRecord("I_7", 7)
	ownership8 := policy.NewManagedIssueRecord("I_8", 8)
	references, err := promotionWorkIssueReferences([]delivery.PromotionWork{
		{Issues: []delivery.ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: ownership7.Digest}}},
		{Excluded: true, Issues: []delivery.ManagedIssueReference{{Number: 8, NodeID: "I_8", OwnershipDigest: ownership8.Digest}}},
	})
	if err != nil || len(references) != 2 || references[0].Number != 7 || references[1].Number != 8 {
		t.Fatalf("references=%+v error=%v", references, err)
	}
	client := &pullRequestPolicyGitHub{
		issues:        map[int]gh.Issue{7: {Number: 7, NodeID: "I_7"}, 8: {Number: 8, NodeID: "I_8"}},
		omitOwnership: map[int]bool{8: true},
	}
	err = verifyPromotionOwnership(context.Background(), client, "example/repo", config.DefaultConfig(), references)
	if err == nil || !strings.Contains(err.Error(), "#8") {
		t.Fatalf("excluded ownership error=%v", err)
	}
}

func promotionPolicyLedgerFixture(t *testing.T, withRecord bool) (string, string, *pullRequestPolicyGitHub) {
	t.Helper()
	cfg, locator, checkpointBody := deliveryWorkflowFixture(t)
	checkpoint, err := delivery.ParseCheckpointIndex(locator, delivery.StoredComment{
		Comment: locator.Checkpoint, Body: checkpointBody, AuthorLogin: delivery.TrustedAuthorLogin, AuthorType: delivery.TrustedAuthorType,
	})
	if err != nil {
		t.Fatal(err)
	}
	records := map[int64]gh.IssueComment{}
	if withRecord {
		appendPlan, err := delivery.PlanStagedWorkAppend(delivery.Snapshot{Locator: locator, Checkpoint: checkpoint}, delivery.StagedWorkAppendInput{
			PullRequest: delivery.ResourceIdentity{Number: 20, NodeID: "PR_20"}, StagingBranch: cfg.Repository.StagingBranch,
			BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40), MergeRevision: strings.Repeat("3", 40), MergedAt: "2026-07-14T01:00:00Z",
			Issues: []delivery.ManagedIssueReference{
				{Number: 7, NodeID: "I_7", OwnershipDigest: policy.NewManagedIssueRecord("I_7", 7).Digest},
				{Number: 8, NodeID: "I_8", OwnershipDigest: policy.NewManagedIssueRecord("I_8", 8).Digest},
			},
			Writer: delivery.WriterProvenance{Workflow: "Delivery Recorder", RunID: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		commit, err := delivery.FinalizeStagedWorkAppend(appendPlan, checkpoint, delivery.CommentIdentity{DatabaseID: 201, NodeID: "IC_201"})
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, checkpointBody = commit.Checkpoint, commit.CheckpointBody
		records[201] = gh.IssueComment{ID: 201, NodeID: "IC_201", IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: commit.RecordBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}}
	}
	promotion := gh.PullRequest{
		Number: 50, NodeID: "PR_50", Title: "Promote", Body: "Closes #7", BaseRef: cfg.Repository.BaseBranch, BaseSHA: strings.Repeat("4", 40),
		HeadRef: cfg.Repository.StagingBranch, HeadSHA: strings.Repeat("3", 40), BaseRepositoryFullName: "example/repo", HeadRepositoryFullName: "example/repo", State: "open",
	}
	ownership7, ownership8 := policy.NewManagedIssueRecord("I_7", 7), policy.NewManagedIssueRecord("I_8", 8)
	transport := &deliveryRecordGitHubStub{
		repository: gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
		issues: map[int]gh.Issue{
			locator.Issue.Number: {Number: locator.Issue.Number, NodeID: locator.Issue.NodeID, Locked: true},
			7:                    {Number: 7, NodeID: "I_7", State: "open"}, 8: {Number: 8, NodeID: "I_8", State: "open"},
		},
		comments: map[int][]gh.IssueComment{
			7: {{ID: 77, Body: policy.RenderManagedIssueRecord(ownership7), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}},
			8: {{ID: 88, Body: policy.RenderManagedIssueRecord(ownership8), Author: gh.Actor{Login: policy.ManagedIssueTrustedLogin, Type: "Bot"}}},
		},
		checkpoint: gh.IssueComment{ID: locator.Checkpoint.DatabaseID, NodeID: locator.Checkpoint.NodeID, IssueURL: "https://api.github.com/repos/example/repo/issues/900", Body: checkpointBody, Author: gh.Actor{Login: delivery.TrustedAuthorLogin, Type: delivery.TrustedAuthorType}},
		pull:       map[int]gh.PullRequest{50: promotion}, open: gh.PullRequestListing{PullRequests: []gh.PullRequest{promotion}, Complete: true}, records: records,
	}
	client := &pullRequestPolicyGitHub{deliveryRecordGitHubStub: transport}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "baton.yml")
	content, err := config.MarshalYAML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(dir, "event.json")
	event := fmt.Sprintf(`{"action":"opened","pull_request":{"number":50,"node_id":"PR_50","title":"Promote","body":"Closes #7","state":"open","base":{"ref":"%s","sha":"%s","repo":{"full_name":"example/repo"}},"head":{"ref":"%s","sha":"%s","repo":{"full_name":"example/repo"}}},"repository":{"full_name":"example/repo"}}`, cfg.Repository.BaseBranch, promotion.BaseSHA, cfg.Repository.StagingBranch, promotion.HeadSHA)
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = checkpoint
	return configPath, eventPath, client
}

func TestPullRequestPolicyWorkflowRejectsRepositoryMismatchBeforeClient(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Work","body":"","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/12","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		t.Fatal("repository mismatch must not construct a GitHub client")
		return nil, nil
	}}
	_, err := workflow.Run(PullRequestPolicyInput{
		EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), Repository: "flag/repo",
	})
	if err == nil {
		t.Fatal("expected repository mismatch")
	}
}

func TestPullRequestPolicyWorkflowSkipsGitHubForDirectBasePR(t *testing.T) {
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	event := `{"pull_request":{"number":12,"title":"Maintenance","body":"","base":{"ref":"main","repo":{"full_name":"event/repo"}},"head":{"ref":"maintenance","repo":{"full_name":"event/repo"}}}}`
	if err := os.WriteFile(eventPath, []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
		t.Fatal("direct base PR must not construct a GitHub client")
		return nil, nil
	}}
	decision, err := workflow.Run(PullRequestPolicyInput{
		EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir), EnvironmentRepo: "event/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Flow != policy.PRFlowUnmanaged || len(decision.Errors) != 0 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestPullRequestPolicyWorkflowDoesNotConstructClientForNonManagedFlows(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name  string
		event string
		flow  policy.PRFlow
	}{
		{name: "unmanaged with issue reference", event: `{"pull_request":{"number":12,"body":"Refs #7","base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"feature/12","repo":{"full_name":"event/repo"}}}}`, flow: policy.PRFlowUnmanaged},
		{name: "misrouted", event: `{"pull_request":{"number":12,"base":{"ref":"release","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/12","repo":{"full_name":"event/repo"}}}}`, flow: policy.PRFlowMisroutedWork},
		{name: "indeterminate", event: `{"pull_request":{"number":12,"base":{"ref":"agent","repo":{"full_name":"event/repo"}},"head":{"ref":"agent-work/12"}}}`, flow: policy.PRFlowIndeterminate},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eventPath := filepath.Join(dir, test.name+".json")
			if err := os.WriteFile(eventPath, []byte(test.event), 0o600); err != nil {
				t.Fatal(err)
			}
			workflow := PullRequestPolicyWorkflow{newClient: func(context.Context, PullRequestPolicyInput) (PullRequestPolicyGitHub, error) {
				t.Fatal("non-managed flow must not construct a GitHub client")
				return nil, nil
			}}
			decision, err := workflow.Run(PullRequestPolicyInput{EventPath: eventPath, ConfigPath: writeWorkflowConfig(t, dir)})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Flow != test.flow {
				t.Fatalf("flow = %q, want %q", decision.Flow, test.flow)
			}
		})
	}
}
