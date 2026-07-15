package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/labels"
)

type doctorScenarioManifest struct {
	Scenarios []doctorScenario `json:"scenarios"`
}

type doctorScenario struct {
	Name       string `json:"name"`
	Mutation   string `json:"mutation"`
	WantState  string `json:"wantState"`
	WantCheck  string `json:"wantCheck"`
	WantStatus string `json:"wantStatus"`
}

func TestAdopterCompatibilityScenarios(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "doctor", "adopter-scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest doctorScenarioManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Scenarios) != 6 {
		t.Fatalf("scenario count = %d, want 6", len(manifest.Scenarios))
	}
	for _, scenario := range manifest.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			facts := readyCompatibilityFacts(t, cfg)
			applyDoctorScenarioMutation(t, &facts, scenario.Mutation)
			checks := EvaluateCompatibility(cfg, facts)
			counts := countChecks(checks)
			if got := readyState(counts); got != scenario.WantState {
				t.Fatalf("readyState = %q, want %q; checks = %+v", got, scenario.WantState, checks)
			}
			if check := findCheck(t, checks, scenario.WantCheck); check.Status != scenario.WantStatus {
				t.Fatalf("check %q = %+v, want status %q", scenario.WantCheck, check, scenario.WantStatus)
			}
		})
	}
}

func readyCompatibilityFacts(t *testing.T, cfg config.Config) CompatibilityFacts {
	t.Helper()
	managed, err := install.RenderManagedFiles(cfg, install.Options{})
	if err != nil {
		t.Fatal(err)
	}
	files := make([]ManagedFileFact, 0, len(managed))
	for _, file := range managed {
		files = append(files, ManagedFileFact{Path: file.Path, LocalAction: "unchanged", LocalOwnership: file.Ownership, RemoteState: "matching"})
	}
	workflows := make([]WorkflowFact, 0, len(requiredWorkflowPaths))
	for _, path := range requiredWorkflowPaths {
		workflows = append(workflows, WorkflowFact{Path: path, State: "active"})
	}
	const githubActionsAppID = 15368
	required := []gh.RequiredCheck{{Context: requiredPolicyCheck, IntegrationID: githubActionsAppID}}
	return CompatibilityFacts{
		Repository: gh.RepositoryDetails{
			Identity:      gh.RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"},
			DefaultBranch: cfg.Repository.BaseBranch, OwnerType: "User", Visibility: "public", Settings: gh.RepositorySettings{AllowMergeCommit: true},
		},
		RepositoryObserved: true, ExpectedRepository: "example/repo",
		ExpectedVersion: cfg.Setup.BaselineBatonVersion, ConfiguredVersion: cfg.Setup.BaselineBatonVersion,
		BaseBranch: gh.Branch{Ref: cfg.Repository.BaseBranch, SHA: "base-sha"}, StagingBranch: gh.Branch{Ref: cfg.Repository.StagingBranch, SHA: "staging-sha"}, BranchesObserved: true,
		BaseRules: gh.BranchRules{Branch: cfg.Repository.BaseBranch, RequiredChecks: required}, StagingRules: gh.BranchRules{Branch: cfg.Repository.StagingBranch, RequiredChecks: required}, RulesObserved: true,
		GitHubActionsAppID: githubActionsAppID,
		Integration:        delivery.BaseIntegrationFacts{State: delivery.BaseIntegrated, Reason: "staging contains the reviewed base boundary"},
		Labels:             labels.SyncPlan{Repo: "example/repo", Counts: labels.SyncCounts{OK: 10}}, LabelsObserved: true,
		RepositoryActions: gh.ActionsPermissions{Enabled: true, AllowedActions: "all"}, ActionsObserved: true,
		RunnerPolicy: RunnerPolicyReadiness{Observed: true, Compatible: true},
		Workflows:    workflows, ManagedFiles: files,
		Delivery:          DeliveryReadiness{Configured: true, Sealed: true, IssueLocked: true, Complete: true},
		Ownership:         OwnershipReadiness{Observed: true, Durable: 2},
		AcquisitionChecks: []Check{okCheck("github-auth", "authenticated repository read succeeded")},
	}
}

func applyDoctorScenarioMutation(t *testing.T, facts *CompatibilityFacts, mutation string) {
	t.Helper()
	switch mutation {
	case "directBasePending":
		facts.Integration = delivery.BaseIntegrationFacts{State: delivery.BaseDirectWorkPending, Reason: "base contains reviewed commits not yet integrated into staging"}
	case "compatibleRuleset":
		facts.StagingRules.AllowedMergeMethodsSet = true
		facts.StagingRules.AllowedMergeMethods = []string{"merge", "squash"}
	case "missingWorkflow":
		facts.Workflows = facts.Workflows[1:]
	case "driftedTrustedCheckout":
		for index := range facts.ManagedFiles {
			if facts.ManagedFiles[index].Path == ".github/workflows/pr-policy.yml" {
				facts.ManagedFiles[index].RemoteState = "drifted"
				return
			}
		}
		t.Fatal("PR policy workflow fixture missing")
	case "mergeQueue":
		facts.StagingRules.MergeQueueEnabled = true
	case "insufficientToken":
		facts.ActionsObserved = false
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("actions-policy-acquisition", "403 Resource not accessible by integration", "Grant repository Administration read permission and retry doctor."))
	default:
		t.Fatalf("unknown mutation %q", mutation)
	}
}

func TestSynchronizationCompatibilityRejectsRulesetWithoutMergeMethod(t *testing.T) {
	check := SynchronizationCompatibilityCheck(gh.RepositorySettings{AllowMergeCommit: true}, gh.BranchRules{
		AllowedMergeMethodsSet: true, AllowedMergeMethods: []string{"squash", "rebase"},
	})
	if check.Status != "fail" || check.Remediation == "" {
		t.Fatalf("check = %+v", check)
	}
}

func TestEditableDocumentDriftDegradesWithoutBlocking(t *testing.T) {
	cfg := config.DefaultConfig()
	facts := readyCompatibilityFacts(t, cfg)
	for index := range facts.ManagedFiles {
		if facts.ManagedFiles[index].Path == ".github/ISSUE_WORKFLOW.md" {
			facts.ManagedFiles[index].RemoteState = "drifted"
		}
	}
	checks := EvaluateCompatibility(cfg, facts)
	if got := readyState(countChecks(checks)); got != "degraded" {
		t.Fatalf("readyState = %q, checks = %+v", got, checks)
	}
	if check := findCheck(t, checks, "editable-documents"); check.Status != "warn" {
		t.Fatalf("editable-documents = %+v", check)
	}
}

func TestActionsPolicyFailsClosedOnUnknownOrIncompatiblePolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		policy   gh.ActionsPermissions
		selected *gh.SelectedActions
		public   bool
		want     bool
	}{
		{name: "all", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "all"}, want: true},
		{name: "selected github owned", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{GitHubOwnedAllowed: true}, want: true},
		{name: "selected github owned exclusion", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{GitHubOwnedAllowed: true, PatternsAllowed: []string{"!actions/setup-go@*"}}},
		{name: "selected explicit public patterns", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{PatternsAllowed: []string{"actions/checkout@v4", "actions/setup-go@v5"}}, public: true, want: true},
		{name: "selected wildcard public patterns", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{PatternsAllowed: []string{"actions/*"}}, public: true, want: true},
		{name: "selected private patterns", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{PatternsAllowed: []string{"actions/*"}}},
		{name: "selected exclusion", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}, selected: &gh.SelectedActions{PatternsAllowed: []string{"actions/*", "!actions/setup-go@*"}}, public: true},
		{name: "unknown", policy: gh.ActionsPermissions{Enabled: true}},
		{name: "selected unavailable", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "selected"}},
		{name: "sha pins required", policy: gh.ActionsPermissions{Enabled: true, AllowedActions: "all", SHAPinningRequired: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := actionsPolicyAllowsGenerated(test.policy, test.selected, test.public); got != test.want {
				t.Fatalf("allowed = %t, want %t", got, test.want)
			}
		})
	}
}

func TestOwnershipReadinessDegradesLegacyAndBlocksInvalidEvidence(t *testing.T) {
	cfg := config.DefaultConfig()
	legacy := readyCompatibilityFacts(t, cfg)
	legacy.Ownership = OwnershipReadiness{Observed: true, Legacy: 1}
	legacyChecks := EvaluateCompatibility(cfg, legacy)
	if got := readyState(countChecks(legacyChecks)); got != "degraded" || findCheck(t, legacyChecks, "issue-ownership").Status != "warn" {
		t.Fatalf("legacy ownership checks = %+v", legacyChecks)
	}

	invalid := readyCompatibilityFacts(t, cfg)
	invalid.Ownership = OwnershipReadiness{Observed: true, Invalid: 1, Message: "one invalid record"}
	invalidChecks := EvaluateCompatibility(cfg, invalid)
	if got := readyState(countChecks(invalidChecks)); got != "blocked" || findCheck(t, invalidChecks, "issue-ownership").Status != "fail" {
		t.Fatalf("invalid ownership checks = %+v", invalidChecks)
	}
}

func TestUnavailableHostedRunnerPolicyDegradesInsteadOfAssumingExecution(t *testing.T) {
	cfg := config.DefaultConfig()
	facts := readyCompatibilityFacts(t, cfg)
	facts.RunnerPolicy = RunnerPolicyReadiness{Message: "organization standard-hosted-runner setting is unavailable"}
	checks := EvaluateCompatibility(cfg, facts)
	if got := readyState(countChecks(checks)); got != "degraded" || findCheck(t, checks, "hosted-runner-policy").Status != "warn" {
		t.Fatalf("runner policy checks = %+v", checks)
	}
}

func TestRequiredPolicyCheckMustMatchExactContextAndGitHubActionsApp(t *testing.T) {
	for _, test := range []struct {
		name  string
		check gh.RequiredCheck
	}{
		{name: "unbound", check: gh.RequiredCheck{Context: requiredPolicyCheck}},
		{name: "wrong app", check: gh.RequiredCheck{Context: requiredPolicyCheck, IntegrationID: 7}},
		{name: "case variant", check: gh.RequiredCheck{Context: "check pr policy", IntegrationID: 15368}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := requiredCheckCompatibility("required", gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{test.check}}, 15368)
			if result.Status != "fail" {
				t.Fatalf("check = %+v", result)
			}
		})
	}
}

func TestMergeBranchRulesPreservesDistinctRequiredCheckSources(t *testing.T) {
	rules := mergeBranchRules(
		gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{{Context: requiredPolicyCheck}}},
		gh.BranchRules{Branch: "main", RequiredChecks: []gh.RequiredCheck{{Context: requiredPolicyCheck, IntegrationID: 15368}}},
	)
	if len(rules.RequiredChecks) != 2 || rules.RequiredChecks[1].IntegrationID != 15368 {
		t.Fatalf("required checks = %+v", rules.RequiredChecks)
	}
}
