package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/policy"
	"github.com/sjunepark/baton/internal/queue"
	"github.com/sjunepark/baton/internal/workitem"
	"gopkg.in/yaml.v3"
)

type adopterCompatibilityManifest struct {
	Scenarios []adopterCompatibilityScenario `json:"scenarios"`
}

type adopterCompatibilityScenario struct {
	Name                     string          `json:"name"`
	Event                    string          `json:"event"`
	Policy                   json.RawMessage `json:"policy"`
	QueueCandidate           json.RawMessage `json:"queueCandidate"`
	Transition               json.RawMessage `json:"transition"`
	WorkflowPrefilterMatches bool            `json:"workflowPrefilterMatches"`
}

func TestAdopterCompatibilityCollisionCharacterization(t *testing.T) {
	manifest := loadAdopterCompatibilityManifest(t)
	cfg := config.DefaultConfig()
	policyWorkflow := renderedWorkflowForTest(t, cfg, ".github/workflows/pr-policy.yml")
	transitionWorkflow := renderedWorkflowForTest(t, cfg, ".github/workflows/work-item-transition.yml")
	policyPrefilter := assertPrefilteredInstalledJob(t, policyWorkflow, "opened")
	transitionPrefilter := assertPrefilteredInstalledJob(t, transitionWorkflow, "closed")
	if !strings.Contains(policyPrefilter, "startsWith") || strings.Contains(transitionPrefilter, "startsWith") || !strings.Contains(transitionPrefilter, "head.ref == 'agent'") || !strings.Contains(transitionPrefilter, "base.ref == 'main'") {
		t.Fatalf("generated workflow prefilters do not split policy candidates from promotion transitions:\npolicy: %s\ntransition: %s", policyPrefilter, transitionPrefilter)
	}

	for _, scenario := range manifest.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			event := loadAdopterCompatibilityEvent(t, scenario.Event)
			policyInput := policy.PRPolicyInput{
				PullRequest:    policyPullRequest(event),
				CommitMessages: []string{"Implement repository change"},
				Policy:         cfg,
			}
			for _, number := range pullRequestReferenceIssueNumbers(event, cfg.PRPolicy.RequiredReferenceKeyword) {
				policyInput.ReferencedIssues = append(policyInput.ReferencedIssues, policy.ReferencedIssue{
					Number:    number,
					Ownership: policy.IssueOwnershipDecision{SchemaVersion: 1, Kind: "managedIssueOwnership", Managed: true, Source: policy.IssueOwnershipRecord, Diagnostics: []string{}, Errors: []string{}},
				})
			}
			if event.BaseRef == cfg.Repository.BaseBranch && event.HeadRef == cfg.Repository.StagingBranch {
				policyInput.PromotionFacts = &policy.PromotionFacts{
					ExpectedIssues: []int{7}, IncludedWorkPullRequests: []int{20}, Complete: true, Source: "sealedDeliveryPlan",
					PlanDigest: "sha256:plan", CursorDigest: "sha256:cursor", CoverageDigest: "sha256:coverage",
					BaseIntegration: delivery.BaseIntegrationFacts{State: delivery.BaseIntegrated, ObservedBaseSHA: event.BaseSHA, ObservedStagingSHA: event.HeadSHA},
				}
			}
			assertSameJSON(t, policy.ComputePullRequestPolicy(policyInput), scenario.Policy)

			pullRequest := gh.PullRequest{
				Number: event.Number, Title: event.Title, Body: event.Body,
				BaseRef: event.BaseRef, HeadRef: event.HeadRef,
				BaseRepositoryFullName: event.BaseRepositoryFullName, HeadRepositoryFullName: event.HeadRepositoryFullName,
				BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA, CheckState: "success",
			}
			flow := classifyTransportPullRequest(pullRequest, cfg)
			if flow != policy.PRFlowUnmanaged && !conservativeWorkflowPrefilter(event, cfg) {
				t.Fatalf("generated workflow prefilter has a false negative for authoritative flow %q", flow)
			}
			snapshot := queue.BuildSnapshot("example/repo", cfg, nil, []queue.PullRequest{{
				Number: pullRequest.Number, Title: pullRequest.Title, Body: pullRequest.Body,
				BaseRef: pullRequest.BaseRef, HeadRef: pullRequest.HeadRef, HeadSHA: pullRequest.HeadSHA,
				CheckState: pullRequest.CheckState, Ownership: flow,
			}})
			next := queue.RecommendNext(snapshot)
			if string(scenario.QueueCandidate) == "null" {
				if len(next.Candidates) != 0 {
					t.Fatalf("unexpected queue candidates: %+v", next.Candidates)
				}
			} else {
				if len(next.Candidates) != 1 {
					t.Fatalf("queue candidates = %+v, want one", next.Candidates)
				}
				assertSameJSON(t, next.Candidates[0], scenario.QueueCandidate)
			}

			// The shared fixture captures the open PR seen by policy and queue;
			// project the same immutable PR facts onto its eventual merged event.
			deliveryDigest := ""
			deliveryIssues := []workitem.DeliveryIssueReference{}
			if flow == policy.PRFlowWork {
				deliveryDigest = "sha256:fixture"
				for _, number := range pullRequestReferenceIssueNumbers(event, cfg.PRPolicy.RequiredReferenceKeyword) {
					deliveryIssues = append(deliveryIssues, workitem.DeliveryIssueReference{Number: number, NodeID: "I_fixture", OwnershipDigest: "sha256:ownership"})
				}
			}
			transition := workitem.PlanPullRequestTransition(workitem.PullRequestEvent{
				Repository: "example/repo", Action: "closed", Number: event.Number,
				Title: event.Title, Body: event.Body, BaseRef: event.BaseRef, HeadRef: event.HeadRef,
				BaseRepositoryFullName: event.BaseRepositoryFullName, HeadRepositoryFullName: event.HeadRepositoryFullName,
				BaseSHA: event.BaseSHA, HeadSHA: event.HeadSHA, State: "closed", Merged: true,
				DeliveryRecordDigest: deliveryDigest, IssueReferences: deliveryIssues,
			}, cfg)
			assertSameJSON(t, transition, scenario.Transition)

			if got := conservativeWorkflowPrefilter(event, cfg); got != scenario.WorkflowPrefilterMatches {
				t.Fatalf("workflow prefilter match = %t, want %t", got, scenario.WorkflowPrefilterMatches)
			}
		})
	}
}

func loadAdopterCompatibilityManifest(t *testing.T) adopterCompatibilityManifest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "adopter-compatibility", "current-behavior.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest adopterCompatibilityManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Scenarios) != 10 {
		t.Fatalf("scenario count = %d, want 10", len(manifest.Scenarios))
	}
	return manifest
}

func loadAdopterCompatibilityEvent(t *testing.T, name string) gh.PullRequestEvent {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "adopter-compatibility", name))
	if err != nil {
		t.Fatal(err)
	}
	event, err := gh.ParsePullRequestEvent(content)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

type renderedWorkflow struct {
	On struct {
		PullRequestTarget struct {
			Branches []string `yaml:"branches"`
			Types    []string `yaml:"types"`
		} `yaml:"pull_request_target"`
	} `yaml:"on"`
	Jobs map[string]struct {
		If    string `yaml:"if"`
		Steps []struct {
			Name string `yaml:"name"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func renderedWorkflowForTest(t *testing.T, cfg config.Config, path string) renderedWorkflow {
	t.Helper()
	files, err := install.RenderManagedFiles(cfg, install.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path != path {
			continue
		}
		var workflow renderedWorkflow
		if err := yaml.Unmarshal(file.Content, &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow
	}
	t.Fatalf("rendered workflow %s not found", path)
	return renderedWorkflow{}
}

func assertPrefilteredInstalledJob(t *testing.T, workflow renderedWorkflow, eventType string) string {
	t.Helper()
	if !compatibilityContainsString(workflow.On.PullRequestTarget.Types, eventType) {
		t.Fatalf("workflow event types = %v, want %q", workflow.On.PullRequestTarget.Types, eventType)
	}
	if len(workflow.On.PullRequestTarget.Branches) != 0 {
		t.Fatalf("workflow still has a target-branch filter: %v", workflow.On.PullRequestTarget.Branches)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("workflow jobs = %+v, want one", workflow.Jobs)
	}
	for _, job := range workflow.Jobs {
		if job.If == "" {
			t.Fatal("workflow ownership prefilter is missing")
		}
		names := make([]string, 0, len(job.Steps))
		for _, step := range job.Steps {
			names = append(names, step.Name)
			if (step.Name == "Set up Go" || step.Name == "Install Baton") && step.If != "" {
				t.Fatalf("workflow step %q unexpectedly has a prefilter: %q", step.Name, step.If)
			}
		}
		if !compatibilityContainsString(names, "Set up Go") || !compatibilityContainsString(names, "Install Baton") {
			t.Fatalf("workflow steps = %v, want unconditional setup and install", names)
		}
		return job.If
	}
	return ""
}

func conservativeWorkflowPrefilter(event gh.PullRequestEvent, cfg config.Config) bool {
	repositoriesCompatible := event.HeadRepositoryFullName == "" || event.BaseRepositoryFullName == "" || strings.EqualFold(event.HeadRepositoryFullName, event.BaseRepositoryFullName)
	shape := strings.HasPrefix(event.HeadRef, cfg.Repository.WorkBranchPrefix) || (event.HeadRef == cfg.Repository.StagingBranch && event.BaseRef == cfg.Repository.BaseBranch)
	return repositoriesCompatible && shape
}

func assertSameJSON(t *testing.T, got any, want json.RawMessage) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch:\ngot:  %s\nwant: %s", encoded, want)
	}
}

func compatibilityContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
