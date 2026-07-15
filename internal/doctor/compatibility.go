package doctor

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/delivery"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/labels"
	"github.com/sjunepark/baton/internal/policy"
)

const requiredPolicyCheck = "Check PR policy"

var requiredGeneratedActions = []string{"actions/checkout@v4", "actions/setup-go@v5"}

var requiredWorkflowPaths = []string{
	".github/workflows/issue-policy.yml",
	".github/workflows/pr-policy.yml",
	".github/workflows/work-item-transition.yml",
	".github/workflows/delivery-recorder.yml",
}

type ManagedFileFact struct {
	Path           string `json:"path"`
	LocalAction    string `json:"localAction"`
	LocalOwnership string `json:"localOwnership"`
	RemoteState    string `json:"remoteState"`
	RemoteMessage  string `json:"remoteMessage,omitempty"`
}

type WorkflowFact struct {
	Path    string `json:"path"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type DeliveryReadiness struct {
	Configured  bool   `json:"configured"`
	Sealed      bool   `json:"sealed"`
	IssueLocked bool   `json:"issueLocked"`
	Complete    bool   `json:"complete"`
	Message     string `json:"message,omitempty"`
}

type OwnershipReadiness struct {
	Observed bool   `json:"observed"`
	Durable  int    `json:"durable"`
	Legacy   int    `json:"legacy"`
	Invalid  int    `json:"invalid"`
	Message  string `json:"message,omitempty"`
}

type RunnerPolicyReadiness struct {
	Observed   bool   `json:"observed"`
	Compatible bool   `json:"compatible"`
	Message    string `json:"message,omitempty"`
}

// CompatibilityFacts is the pure adoption-gate input. Acquisition failures
// remain explicit checks so omitted GitHub facts can never imply readiness.
type CompatibilityFacts struct {
	Repository           gh.RepositoryDetails          `json:"repository"`
	RepositoryObserved   bool                          `json:"repositoryObserved"`
	ExpectedRepository   string                        `json:"expectedRepository"`
	ExpectedVersion      string                        `json:"expectedVersion"`
	ConfiguredVersion    string                        `json:"configuredVersion"`
	BaseBranch           gh.Branch                     `json:"baseBranch"`
	StagingBranch        gh.Branch                     `json:"stagingBranch"`
	BranchesObserved     bool                          `json:"branchesObserved"`
	BaseRules            gh.BranchRules                `json:"baseRules"`
	StagingRules         gh.BranchRules                `json:"stagingRules"`
	RulesObserved        bool                          `json:"rulesObserved"`
	GitHubActionsAppID   int64                         `json:"githubActionsAppId"`
	Integration          delivery.BaseIntegrationFacts `json:"integration"`
	Labels               labels.SyncPlan               `json:"labels"`
	LabelsObserved       bool                          `json:"labelsObserved"`
	RepositoryActions    gh.ActionsPermissions         `json:"repositoryActions"`
	OrganizationActions  *gh.ActionsPermissions        `json:"organizationActions,omitempty"`
	RepositorySelected   *gh.SelectedActions           `json:"repositorySelected,omitempty"`
	OrganizationSelected *gh.SelectedActions           `json:"organizationSelected,omitempty"`
	ActionsObserved      bool                          `json:"actionsObserved"`
	RunnerPolicy         RunnerPolicyReadiness         `json:"runnerPolicy"`
	Workflows            []WorkflowFact                `json:"workflows"`
	ManagedFiles         []ManagedFileFact             `json:"managedFiles"`
	Delivery             DeliveryReadiness             `json:"delivery"`
	Ownership            OwnershipReadiness            `json:"ownership"`
	AcquisitionChecks    []Check                       `json:"acquisitionChecks,omitempty"`
}

type compatibilityGitHub interface {
	GetRepositoryDetailsContext(context.Context, string) (gh.RepositoryDetails, error)
	GetAppContext(context.Context, string) (gh.App, error)
	GetBranchContext(context.Context, string, string) (gh.Branch, error)
	GetEffectiveBranchRulesContext(context.Context, string, string) (gh.BranchRules, error)
	GetClassicBranchRulesContext(context.Context, string, string) (gh.BranchRules, error)
	CompareCommitsContext(context.Context, string, string, string) (gh.CommitComparison, error)
	ListLabelsContext(context.Context, string) ([]gh.Label, error)
	GetRepositoryActionsPermissionsContext(context.Context, string) (gh.ActionsPermissions, error)
	GetOrganizationActionsPermissionsContext(context.Context, string) (gh.ActionsPermissions, error)
	GetRepositorySelectedActionsContext(context.Context, string) (gh.SelectedActions, error)
	GetOrganizationSelectedActionsContext(context.Context, string) (gh.SelectedActions, error)
	GetWorkflowContext(context.Context, string, string) (gh.Workflow, error)
	GetRepositoryFileContext(context.Context, string, string, string) (gh.RepositoryFile, error)
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	GetIssueCommentContext(context.Context, string, int64) (gh.IssueComment, error)
	ListOpenIssuesContext(context.Context, string) ([]gh.Issue, error)
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
}

func EvaluateCompatibility(cfg config.Config, facts CompatibilityFacts) []Check {
	checks := append([]Check(nil), facts.AcquisitionChecks...)
	if !facts.RepositoryObserved {
		checks = append(checks, failCheck("github-repository", "repository facts were not observed", "Grant repository metadata access and rerun doctor."))
	}
	if facts.RepositoryObserved {
		if !strings.EqualFold(facts.Repository.Identity.FullName, facts.ExpectedRepository) {
			checks = append(checks, failCheck("github-repository", "GitHub returned a different repository identity", "Run doctor from the matching checkout and repository."))
		} else {
			checks = append(checks, okCheck("github-repository", facts.Repository.Identity.FullName))
		}
		if facts.Repository.DefaultBranch != cfg.Repository.BaseBranch {
			checks = append(checks, failCheck("default-branch", fmt.Sprintf("GitHub default branch %q does not match configured base %q", facts.Repository.DefaultBranch, cfg.Repository.BaseBranch), "Align repository.default_branch and repository.base_branch before enabling Baton."))
		} else {
			checks = append(checks, okCheck("default-branch", facts.Repository.DefaultBranch))
		}
	}

	if facts.ConfiguredVersion == "" || facts.ConfiguredVersion != facts.ExpectedVersion {
		checks = append(checks, failCheck("baton-version", fmt.Sprintf("configured setup baseline %q does not match this Baton release %q", facts.ConfiguredVersion, facts.ExpectedVersion), "Review the adopter update and reconcile the pinned Baton version in one normal PR."))
	} else {
		checks = append(checks, okCheck("baton-version", facts.ConfiguredVersion))
	}
	if !facts.RepositoryObserved {
		return checks
	}

	if !facts.BranchesObserved {
		checks = append(checks, failCheck("base-staging-branches", "configured branch facts were not observed", "Grant branch metadata access and rerun doctor."))
	}
	if facts.BranchesObserved {
		if facts.BaseBranch.Ref != cfg.Repository.BaseBranch || facts.StagingBranch.Ref != cfg.Repository.StagingBranch || facts.BaseBranch.SHA == "" || facts.StagingBranch.SHA == "" {
			checks = append(checks, failCheck("base-staging-branches", "configured base or staging branch is missing from GitHub", "Create or repair the configured branches before enabling Baton."))
		} else {
			checks = append(checks, okCheck("base-staging-branches", fmt.Sprintf("%s and %s", facts.BaseBranch.Ref, facts.StagingBranch.Ref)))
		}
	}

	if facts.Delivery.Configured && facts.Delivery.Sealed && facts.Delivery.IssueLocked && facts.Delivery.Complete {
		checks = append(checks, okCheck("delivery-readiness", "sealed delivery ledger is complete"))
		switch facts.Integration.State {
		case delivery.BaseIntegrated:
			checks = append(checks, okCheck("base-staging-integration", facts.Integration.Reason))
		case delivery.BaseDirectWorkPending:
			checks = append(checks, warnCheck("base-staging-integration", facts.Integration.Reason, "Open and merge the recommended reviewed base-to-staging synchronization PR."))
		case delivery.BaseIntegrationDiverged:
			checks = append(checks, failCheck("base-staging-integration", facts.Integration.Reason, "Repair the branch lineage or explicitly rebootstrap reviewed delivery evidence."))
		default:
			checks = append(checks, failCheck("base-staging-integration", firstNonBlank(facts.Integration.Reason, "base/staging integration is unknown"), "Repair incomplete delivery evidence before enabling Baton."))
		}
	} else if facts.Delivery.Configured && !facts.Delivery.Sealed {
		message := firstNonBlank(facts.Delivery.Message, "delivery authority remains in shadow mode")
		checks = append(checks, warnCheck("delivery-readiness", message, "Complete reviewed delivery bootstrap and seal the exact locator before finishing adoption."))
	} else {
		message := firstNonBlank(facts.Delivery.Message, "sealed delivery ledger is not ready")
		checks = append(checks, failCheck("delivery-readiness", message, "Install Delivery Recorder, complete reviewed bootstrap, and seal the exact locator."))
	}

	switch {
	case !facts.Ownership.Observed || facts.Ownership.Invalid > 0:
		checks = append(checks, failCheck("issue-ownership", firstNonBlank(facts.Ownership.Message, "managed-issue ownership evidence is unavailable or invalid"), "Repair invalid ownership records and rerun doctor with issue comment read access."))
	case facts.Ownership.Legacy > 0:
		checks = append(checks, warnCheck("issue-ownership", fmt.Sprintf("%d open managed issues still use legacy fingerprint ownership", facts.Ownership.Legacy), "Run the reviewed ownership-record backfill before finishing adoption."))
	default:
		checks = append(checks, okCheck("issue-ownership", fmt.Sprintf("%d durable managed-issue records are valid", facts.Ownership.Durable)))
	}

	if !facts.LabelsObserved {
		checks = append(checks, failCheck("managed-labels", "managed label facts were not observed", "Grant issue metadata access and rerun doctor."))
	} else {
		if facts.Labels.Counts.Create > 0 || facts.Labels.Counts.Update > 0 {
			checks = append(checks, failCheck("managed-labels", fmt.Sprintf("%d labels missing and %d labels drifted", facts.Labels.Counts.Create, facts.Labels.Counts.Update), "Review `baton sync-labels --dry-run --json`, then apply the approved label plan."))
		} else {
			checks = append(checks, okCheck("managed-labels", fmt.Sprintf("%d managed labels match", facts.Labels.Counts.OK)))
		}
	}

	checks = append(checks, evaluateManagedFiles(facts.ManagedFiles)...)
	checks = append(checks, evaluateWorkflows(facts.Workflows)...)
	if !facts.ActionsObserved {
		checks = append(checks, failCheck("actions-policy", "GitHub Actions policy facts were not observed", "Grant Actions administration read access and rerun doctor."))
	} else {
		checks = append(checks, evaluateActionsPolicy(facts))
	}
	switch {
	case !facts.RunnerPolicy.Observed:
		checks = append(checks, warnCheck("hosted-runner-policy", firstNonBlank(facts.RunnerPolicy.Message, "standard GitHub-hosted runner policy is unavailable"), "Confirm that standard GitHub-hosted runners are enabled for this organization before enabling automation."))
	case !facts.RunnerPolicy.Compatible:
		checks = append(checks, failCheck("hosted-runner-policy", firstNonBlank(facts.RunnerPolicy.Message, "standard GitHub-hosted runners are incompatible"), "Enable standard GitHub-hosted runners or change Baton's reviewed workflow runner strategy."))
	default:
		checks = append(checks, okCheck("hosted-runner-policy", firstNonBlank(facts.RunnerPolicy.Message, "standard GitHub-hosted runners are available")))
	}
	if !facts.RulesObserved {
		checks = append(checks, failCheck("branch-rules", "branch rules were not observed", "Grant rules and branch-protection read access and rerun doctor."))
	} else {
		checks = append(checks, requiredCheckCompatibility("base-required-check", facts.BaseRules, facts.GitHubActionsAppID))
		checks = append(checks, requiredCheckCompatibility("staging-required-check", facts.StagingRules, facts.GitHubActionsAppID))
		checks = append(checks, SynchronizationCompatibilityCheck(facts.Repository.Settings, facts.StagingRules))
		if facts.BaseRules.MergeQueueEnabled || facts.StagingRules.MergeQueueEnabled {
			checks = append(checks, failCheck("merge-queue", "an active merge queue applies to a Baton-managed branch", "Disable the merge queue for the configured base and staging branches; merge_group is not supported."))
		} else {
			checks = append(checks, okCheck("merge-queue", "no active merge queue applies"))
		}
	}
	return checks
}

func evaluateManagedFiles(files []ManagedFileFact) []Check {
	if len(files) == 0 {
		return []Check{failCheck("managed-files", "managed-file reconciliation did not run", "Repair branch/config acquisition and rerun doctor."), failCheck("editable-documents", "editable guidance was not acquired", "Repair branch/config acquisition and rerun doctor.")}
	}
	criticalDrift, criticalUnavailable, editable := []string{}, []string{}, []string{}
	for _, file := range files {
		if file.LocalAction == "unchanged" && file.RemoteState == "matching" {
			continue
		}
		if file.Path == ".github/ISSUE_WORKFLOW.md" {
			editable = append(editable, file.Path)
		} else if file.LocalAction == "unavailable" || file.RemoteState == "unavailable" {
			criticalUnavailable = append(criticalUnavailable, file.Path)
		} else {
			criticalDrift = append(criticalDrift, file.Path)
		}
	}
	sort.Strings(criticalDrift)
	sort.Strings(criticalUnavailable)
	sort.Strings(editable)
	checks := []Check{}
	if len(criticalUnavailable) > 0 || len(criticalDrift) > 0 {
		problems := []string{}
		if len(criticalUnavailable) > 0 {
			problems = append(problems, "unavailable: "+strings.Join(criticalUnavailable, ", "))
		}
		if len(criticalDrift) > 0 {
			problems = append(problems, "missing or different: "+strings.Join(criticalDrift, ", "))
		}
		checks = append(checks, failCheck("managed-files", "security or policy files are incompatible ("+strings.Join(problems, "; ")+")", "Grant Contents read permission, repair local reconciliation errors, then review `baton init --dry-run --json` and reconcile trusted-file drift; pass doctor the same reviewed install target used by init."))
	} else {
		checks = append(checks, okCheck("managed-files", "security and policy files match locally and on the default branch"))
	}
	if len(editable) > 0 {
		checks = append(checks, warnCheck("editable-documents", "editable guidance differs or could not be read: "+strings.Join(editable, ", "), "Review or reacquire the guidance; it does not weaken workflow execution policy."))
	} else {
		checks = append(checks, okCheck("editable-documents", "editable guidance matches"))
	}
	return checks
}

func evaluateWorkflows(workflows []WorkflowFact) []Check {
	byPath := map[string]WorkflowFact{}
	for _, workflow := range workflows {
		byPath[workflow.Path] = workflow
	}
	missing, disabled, unavailable := []string{}, []string{}, []string{}
	for _, path := range requiredWorkflowPaths {
		workflow, found := byPath[path]
		switch {
		case !found || workflow.State == "missing":
			missing = append(missing, path)
		case workflow.State == "unavailable":
			unavailable = append(unavailable, path)
		case workflow.State != "active":
			disabled = append(disabled, path)
		}
	}
	if len(missing)+len(disabled)+len(unavailable) > 0 {
		problems := []string{}
		if len(missing) > 0 {
			problems = append(problems, "missing: "+strings.Join(missing, ", "))
		}
		if len(disabled) > 0 {
			problems = append(problems, "disabled: "+strings.Join(disabled, ", "))
		}
		if len(unavailable) > 0 {
			problems = append(problems, "unavailable: "+strings.Join(unavailable, ", "))
		}
		return []Check{failCheck("workflows-enabled", "required workflows are not active ("+strings.Join(problems, "; ")+")", "Grant Actions read permission, reconcile missing workflows, and enable each required workflow.")}
	}
	return []Check{okCheck("workflows-enabled", "all required workflows are active")}
}

func evaluateActionsPolicy(facts CompatibilityFacts) Check {
	if !facts.RepositoryActions.Enabled {
		return failCheck("actions-policy", "GitHub Actions is disabled for the repository", "Enable GitHub Actions before enabling Baton automation.")
	}
	patternsApply := strings.EqualFold(facts.Repository.Visibility, "public")
	if !actionsPolicyAllowsGenerated(facts.RepositoryActions, facts.RepositorySelected, patternsApply) {
		return failCheck("actions-policy", "repository Actions policy does not permit generated GitHub-owned actions", "Allow GitHub-owned actions/checkout and actions/setup-go, and do not require tag pins to be full SHAs unless templates are updated.")
	}
	if facts.OrganizationActions != nil && !actionsPolicyAllowsGenerated(*facts.OrganizationActions, facts.OrganizationSelected, patternsApply) {
		return failCheck("actions-policy", "organization Actions policy does not permit generated GitHub-owned actions", "Update the organization allowlist or generated action pins before enabling Baton.")
	}
	return okCheck("actions-policy", "repository and organization policy permit generated GitHub-owned actions")
}

func actionsPolicyAllowsGenerated(policy gh.ActionsPermissions, selected *gh.SelectedActions, patternsApply bool) bool {
	if !policy.Enabled || policy.SHAPinningRequired {
		return false
	}
	switch policy.AllowedActions {
	case "all":
		return true
	case "selected":
		if selected == nil {
			return false
		}
		if selected.GitHubOwnedAllowed {
			for _, action := range requiredGeneratedActions {
				_, denied := selectedActionPatternDecision(selected.PatternsAllowed, action)
				if denied {
					return false
				}
			}
			return true
		}
		if !patternsApply {
			return false
		}
		for _, action := range requiredGeneratedActions {
			if !selectedActionAllowed(selected.PatternsAllowed, action) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func selectedActionAllowed(patterns []string, action string) bool {
	allowed, denied := selectedActionPatternDecision(patterns, action)
	return allowed && !denied
}

func selectedActionPatternDecision(patterns []string, action string) (bool, bool) {
	allowed, denied := false, false
	for _, entry := range patterns {
		for _, rawPattern := range strings.Split(entry, ",") {
			pattern := strings.TrimSpace(rawPattern)
			negative := strings.HasPrefix(pattern, "!")
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
			if pattern == "" || !githubActionPatternMatches(pattern, action) {
				continue
			}
			if negative {
				denied = true
			} else {
				allowed = true
			}
		}
	}
	return allowed, denied
}

func githubActionPatternMatches(pattern, action string) bool {
	expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	matched, err := regexp.MatchString(expression, action)
	return err == nil && matched
}

func requiredCheckCompatibility(name string, rules gh.BranchRules, githubActionsAppID int64) Check {
	for _, required := range rules.RequiredChecks {
		if required.Context == requiredPolicyCheck && githubActionsAppID > 0 && required.IntegrationID == githubActionsAppID {
			return okCheck(name, requiredPolicyCheck+" is required on "+rules.Branch)
		}
	}
	return failCheck(name, requiredPolicyCheck+" from the GitHub Actions app is not required on "+rules.Branch, "Require the exact generated PR Policy check and bind its source to the GitHub Actions app.")
}

func acquireCompatibilityFacts(ctx context.Context, client compatibilityGitHub, repo, root string, cfg config.Config, installOptions install.Options) CompatibilityFacts {
	facts := CompatibilityFacts{
		ExpectedVersion:    config.DefaultConfig().Setup.BaselineBatonVersion,
		ConfiguredVersion:  cfg.Setup.BaselineBatonVersion,
		ExpectedRepository: repo,
		Workflows:          []WorkflowFact{}, ManagedFiles: []ManagedFileFact{}, AcquisitionChecks: []Check{},
	}
	details, err := client.GetRepositoryDetailsContext(ctx, repo)
	if err != nil {
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("github-auth", "authenticated repository read failed: "+err.Error(), "Grant the token repository metadata access and verify the selected repository."))
		return facts
	}
	facts.Repository, facts.RepositoryObserved = details, true
	facts.AcquisitionChecks = append(facts.AcquisitionChecks, okCheck("github-auth", "authenticated repository read succeeded"))
	githubActionsApp, appErr := client.GetAppContext(ctx, "github-actions")
	if appErr != nil || githubActionsApp.ID <= 0 || githubActionsApp.Slug != "github-actions" {
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("github-actions-app", firstNonBlank(joinedFactErrors(appErr), "GitHub Actions app identity is invalid"), "Grant public app metadata access and verify the GitHub Actions app identity on this host."))
	} else {
		facts.GitHubActionsAppID = githubActionsApp.ID
	}

	base, baseErr := client.GetBranchContext(ctx, repo, cfg.Repository.BaseBranch)
	staging, stagingErr := client.GetBranchContext(ctx, repo, cfg.Repository.StagingBranch)
	if baseErr != nil || stagingErr != nil {
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("branch-facts", joinedFactErrors(baseErr, stagingErr), "Create or repair both configured branches and grant metadata read access."))
	} else {
		facts.BaseBranch, facts.StagingBranch, facts.BranchesObserved = base, staging, true
	}

	desired, err := install.RenderManagedFiles(cfg, installOptions)
	facts.Workflows = acquireWorkflows(ctx, client, repo)
	if err != nil {
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("managed-files-acquisition", err.Error(), "Repair Baton config before reconciling managed files."))
	} else {
		if facts.BranchesObserved {
			defaultSHA := base.SHA
			if details.DefaultBranch != cfg.Repository.BaseBranch {
				defaultBranch, defaultErr := client.GetBranchContext(ctx, repo, details.DefaultBranch)
				if defaultErr != nil {
					facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("default-branch-facts", defaultErr.Error(), "Grant branch metadata access and repair the repository default branch."))
					defaultSHA = ""
				} else {
					defaultSHA = defaultBranch.SHA
				}
			}
			if defaultSHA != "" {
				facts.ManagedFiles = acquireManagedFiles(ctx, client, repo, root, defaultSHA, desired, &facts.AcquisitionChecks)
			}
		}
		facts.Labels, facts.LabelsObserved = acquireLabelFacts(ctx, client, repo, cfg, desired, &facts.AcquisitionChecks)
	}

	baseRules, baseRulesErr := acquireBranchRules(ctx, client, repo, cfg.Repository.BaseBranch)
	stagingRules, stagingRulesErr := acquireBranchRules(ctx, client, repo, cfg.Repository.StagingBranch)
	if baseRulesErr != nil || stagingRulesErr != nil {
		facts.AcquisitionChecks = append(facts.AcquisitionChecks, failCheck("branch-rules", joinedFactErrors(baseRulesErr, stagingRulesErr), "Grant rules/protection read access and retry doctor."))
	} else {
		facts.BaseRules, facts.StagingRules, facts.RulesObserved = baseRules, stagingRules, true
	}

	facts.RepositoryActions, facts.OrganizationActions, facts.RepositorySelected, facts.OrganizationSelected, facts.ActionsObserved = acquireActionsFacts(ctx, client, repo, details, &facts.AcquisitionChecks)
	if strings.EqualFold(details.OwnerType, "Organization") {
		facts.RunnerPolicy = RunnerPolicyReadiness{Message: "GitHub does not expose the organization standard-hosted-runner disablement setting through the supported REST API"}
	} else {
		facts.RunnerPolicy = RunnerPolicyReadiness{Observed: true, Compatible: true, Message: "no organization hosted-runner restriction applies"}
	}
	facts.Ownership = acquireOwnershipFacts(ctx, client, repo, cfg)
	facts.Delivery, facts.Integration = acquireDeliveryFacts(ctx, client, repo, cfg, details, base, staging)
	return facts
}

func acquireManagedFiles(ctx context.Context, client compatibilityGitHub, repo, root, defaultBranch string, desired []install.ManagedFile, acquisition *[]Check) []ManagedFileFact {
	local, err := install.PreviewManagedFiles(root, desired)
	localByPath := map[string]install.FileChange{}
	if err != nil {
		*acquisition = append(*acquisition, failCheck("local-reconciliation", err.Error(), "Repair the checkout before rerunning doctor."))
	} else {
		for _, change := range local.Changes {
			localByPath[change.Path] = change
		}
	}
	result := make([]ManagedFileFact, 0, len(desired))
	for _, expected := range desired {
		fact := ManagedFileFact{Path: expected.Path, LocalAction: "unavailable", RemoteState: "unavailable"}
		if change, found := localByPath[expected.Path]; found {
			fact.LocalAction, fact.LocalOwnership = change.Action, change.Ownership
		}
		remote, remoteErr := client.GetRepositoryFileContext(ctx, repo, expected.Path, defaultBranch)
		switch {
		case remoteErr == nil && bytes.Equal(remote.Content, expected.Content):
			fact.RemoteState = "matching"
		case remoteErr == nil:
			fact.RemoteState = "drifted"
		case gh.IsNotFound(remoteErr):
			fact.RemoteState = "missing"
			fact.RemoteMessage = remoteErr.Error()
		default:
			fact.RemoteMessage = remoteErr.Error()
		}
		result = append(result, fact)
	}
	return result
}

func acquireWorkflows(ctx context.Context, client compatibilityGitHub, repo string) []WorkflowFact {
	result := make([]WorkflowFact, 0, len(requiredWorkflowPaths))
	for _, workflowPath := range requiredWorkflowPaths {
		workflow, err := client.GetWorkflowContext(ctx, repo, workflowPath)
		if err != nil {
			state := "unavailable"
			if gh.IsNotFound(err) {
				state = "missing"
			}
			result = append(result, WorkflowFact{Path: workflowPath, State: state, Message: err.Error()})
			continue
		}
		result = append(result, WorkflowFact{Path: workflow.Path, State: workflow.State})
	}
	return result
}

func acquireLabelFacts(ctx context.Context, client compatibilityGitHub, repo string, cfg config.Config, desired []install.ManagedFile, acquisition *[]Check) (labels.SyncPlan, bool) {
	var content []byte
	for _, file := range desired {
		if file.Path == cfg.Labels.Manifest {
			content = file.Content
			break
		}
	}
	manifest, err := labels.ParseManifest(content)
	if err != nil {
		*acquisition = append(*acquisition, failCheck("managed-labels-acquisition", err.Error(), "Repair the generated label manifest."))
		return labels.SyncPlan{}, false
	}
	existing, err := client.ListLabelsContext(ctx, repo)
	if err != nil {
		*acquisition = append(*acquisition, failCheck("managed-labels-acquisition", err.Error(), "Grant issues metadata read access and retry doctor."))
		return labels.SyncPlan{}, false
	}
	current := make([]labels.Label, 0, len(existing))
	for _, label := range existing {
		current = append(current, labels.Label{Name: label.Name, Color: label.Color, Description: label.Description})
	}
	return labels.PlanSync(repo, manifest.Labels, current), true
}

func acquireBranchRules(ctx context.Context, client compatibilityGitHub, repo, branch string) (gh.BranchRules, error) {
	effective, err := client.GetEffectiveBranchRulesContext(ctx, repo, branch)
	if err != nil {
		return gh.BranchRules{}, err
	}
	classic, classicErr := client.GetClassicBranchRulesContext(ctx, repo, branch)
	if gh.IsNotFound(classicErr) {
		classicErr = nil
	}
	if classicErr != nil {
		return gh.BranchRules{}, classicErr
	}
	return mergeBranchRules(effective, classic), nil
}

func mergeBranchRules(effective, classic gh.BranchRules) gh.BranchRules {
	result := effective
	if result.Branch == "" {
		result.Branch = classic.Branch
	}
	result.RequiredLinearHistory = result.RequiredLinearHistory || classic.RequiredLinearHistory
	result.MergeQueueEnabled = result.MergeQueueEnabled || classic.MergeQueueEnabled
	result.StrictRequiredChecks = result.StrictRequiredChecks || classic.StrictRequiredChecks
	result.DismissStaleReviews = result.DismissStaleReviews || classic.DismissStaleReviews
	result.RequireLastPushApproval = result.RequireLastPushApproval || classic.RequireLastPushApproval
	if classic.RequiredApprovingReviewCount > result.RequiredApprovingReviewCount {
		result.RequiredApprovingReviewCount = classic.RequiredApprovingReviewCount
	}
	seen := map[string]struct{}{}
	for _, required := range result.RequiredChecks {
		seen[requiredCheckKey(required)] = struct{}{}
	}
	for _, required := range classic.RequiredChecks {
		if _, exists := seen[requiredCheckKey(required)]; !exists {
			result.RequiredChecks = append(result.RequiredChecks, required)
		}
	}
	return result
}

func requiredCheckKey(check gh.RequiredCheck) string {
	return strings.ToLower(strings.TrimSpace(check.Context)) + ":" + strconv.FormatInt(check.IntegrationID, 10)
}

func acquireOwnershipFacts(ctx context.Context, client compatibilityGitHub, repo string, cfg config.Config) OwnershipReadiness {
	issues, err := client.ListOpenIssuesContext(ctx, repo)
	if err != nil {
		return OwnershipReadiness{Message: "open issues are unavailable: " + err.Error()}
	}
	readiness := OwnershipReadiness{Observed: true}
	for _, issue := range issues {
		if cfg.Delivery != nil && issue.Number == cfg.Delivery.Issue.Number {
			continue
		}
		ownershipComments := []policy.IssueOwnershipComment{}
		if issue.CommentCount > 0 {
			comments, commentsErr := client.ListIssueCommentsContext(ctx, repo, issue.Number)
			if commentsErr != nil {
				return OwnershipReadiness{Message: fmt.Sprintf("ownership comments for issue #%d are unavailable: %v", issue.Number, commentsErr)}
			}
			ownershipComments = make([]policy.IssueOwnershipComment, 0, len(comments))
			for _, comment := range comments {
				ownershipComments = append(ownershipComments, policy.IssueOwnershipComment{ID: comment.ID, Body: comment.Body, AuthorLogin: comment.Author.Login, AuthorType: comment.Author.Type})
			}
		}
		decision := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
			IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels,
			Comments: ownershipComments, Policy: cfg.IssuePolicy,
		})
		switch decision.Source {
		case policy.IssueOwnershipRecord:
			readiness.Durable++
		case policy.IssueOwnershipLegacyFingerprint:
			readiness.Legacy++
		case policy.IssueOwnershipInvalid, policy.IssueOwnershipUnavailable:
			readiness.Invalid++
		}
	}
	if readiness.Invalid > 0 {
		readiness.Message = fmt.Sprintf("%d open issues have invalid or unavailable ownership evidence", readiness.Invalid)
	}
	return readiness
}

func acquireActionsFacts(ctx context.Context, client compatibilityGitHub, repo string, details gh.RepositoryDetails, acquisition *[]Check) (gh.ActionsPermissions, *gh.ActionsPermissions, *gh.SelectedActions, *gh.SelectedActions, bool) {
	repositoryPolicy, err := client.GetRepositoryActionsPermissionsContext(ctx, repo)
	if err != nil {
		*acquisition = append(*acquisition, failCheck("actions-policy-acquisition", err.Error(), "Grant repository Administration read permission and retry doctor."))
		return gh.ActionsPermissions{}, nil, nil, nil, false
	}
	var repositorySelected *gh.SelectedActions
	if repositoryPolicy.AllowedActions == "selected" {
		selected, selectedErr := client.GetRepositorySelectedActionsContext(ctx, repo)
		if selectedErr != nil {
			*acquisition = append(*acquisition, failCheck("actions-policy-acquisition", selectedErr.Error(), "Grant repository Administration read permission for selected-actions policy."))
			return gh.ActionsPermissions{}, nil, nil, nil, false
		}
		repositorySelected = &selected
	}
	if !strings.EqualFold(details.OwnerType, "Organization") {
		return repositoryPolicy, nil, repositorySelected, nil, true
	}
	owner := strings.SplitN(repo, "/", 2)[0]
	organizationPolicy, err := client.GetOrganizationActionsPermissionsContext(ctx, owner)
	if err != nil {
		*acquisition = append(*acquisition, failCheck("actions-policy-acquisition", err.Error(), "Grant organization Administration read permission and retry doctor."))
		return gh.ActionsPermissions{}, nil, nil, nil, false
	}
	var organizationSelected *gh.SelectedActions
	if organizationPolicy.AllowedActions == "selected" {
		selected, selectedErr := client.GetOrganizationSelectedActionsContext(ctx, owner)
		if selectedErr != nil {
			*acquisition = append(*acquisition, failCheck("actions-policy-acquisition", selectedErr.Error(), "Grant organization Administration read permission for selected-actions policy."))
			return gh.ActionsPermissions{}, nil, nil, nil, false
		}
		organizationSelected = &selected
	}
	return repositoryPolicy, &organizationPolicy, repositorySelected, organizationSelected, true
}

func acquireDeliveryFacts(ctx context.Context, client compatibilityGitHub, repo string, cfg config.Config, details gh.RepositoryDetails, base, staging gh.Branch) (DeliveryReadiness, delivery.BaseIntegrationFacts) {
	if cfg.Delivery == nil {
		return DeliveryReadiness{Message: "delivery locator is not configured"}, delivery.BaseIntegrationFacts{}
	}
	readiness := DeliveryReadiness{Configured: true, Sealed: cfg.Delivery.Authority == config.DeliveryAuthoritySealed}
	if !readiness.Sealed {
		readiness.Message = "delivery authority is shadow, not sealed"
		return readiness, delivery.BaseIntegrationFacts{}
	}
	if !strings.EqualFold(cfg.Delivery.Repository.FullName, details.Identity.FullName) || cfg.Delivery.Repository.NodeID != details.Identity.NodeID || !strings.EqualFold(cfg.Delivery.Host, details.Identity.Host) {
		readiness.Message = "delivery repository identity does not match the authenticated repository"
		return readiness, delivery.BaseIntegrationFacts{}
	}
	locator := delivery.DeliveryStoreLocator{
		Repository: delivery.RepositoryIdentity{Host: cfg.Delivery.Host, FullName: cfg.Delivery.Repository.FullName, NodeID: cfg.Delivery.Repository.NodeID},
		Issue:      delivery.ResourceIdentity{Number: cfg.Delivery.Issue.Number, NodeID: cfg.Delivery.Issue.NodeID},
		Checkpoint: delivery.CommentIdentity{DatabaseID: cfg.Delivery.Checkpoint.DatabaseID, NodeID: cfg.Delivery.Checkpoint.NodeID},
	}
	issue, err := client.GetIssueContext(ctx, repo, locator.Issue.Number)
	if err != nil || issue.NodeID != locator.Issue.NodeID || !issue.Locked {
		readiness.Message = "pinned delivery issue is unavailable, changed, or unlocked"
		return readiness, delivery.BaseIntegrationFacts{}
	}
	readiness.IssueLocked = true
	checkpointComment, err := client.GetIssueCommentContext(ctx, repo, locator.Checkpoint.DatabaseID)
	if err != nil || !deliveryCommentBelongsToIssue(checkpointComment, repo, locator.Issue.Number) {
		readiness.Message = "pinned delivery checkpoint is unavailable"
		return readiness, delivery.BaseIntegrationFacts{}
	}
	checkpointStored := storedDeliveryComment(checkpointComment)
	checkpoint, err := delivery.ParseCheckpointIndex(locator, checkpointStored)
	if err != nil {
		readiness.Message = "pinned delivery checkpoint is invalid: " + err.Error()
		return readiness, delivery.BaseIntegrationFacts{}
	}
	records := []delivery.StoredComment{}
	for _, reference := range deliveryReferences(checkpoint) {
		comment, readErr := client.GetIssueCommentContext(ctx, repo, reference.Comment.DatabaseID)
		if readErr != nil || !deliveryCommentBelongsToIssue(comment, repo, locator.Issue.Number) {
			readiness.Message = "referenced delivery record is unavailable"
			return readiness, delivery.BaseIntegrationFacts{}
		}
		records = append(records, storedDeliveryComment(comment))
	}
	snapshot, err := delivery.ParseStoreSnapshot(delivery.StoreSnapshot{Locator: locator, Checkpoint: checkpointStored, Records: records, Complete: true})
	if err != nil {
		readiness.Message = "delivery store is incomplete or invalid: " + err.Error()
		return readiness, delivery.BaseIntegrationFacts{}
	}
	readiness.Complete = true
	integration, err := acquireIntegrationFacts(ctx, client, repo, snapshot, base.SHA, staging.SHA)
	if err != nil {
		readiness.Complete = false
		readiness.Message = "base integration facts are unavailable: " + err.Error()
		return readiness, delivery.BaseIntegrationFacts{}
	}
	return readiness, integration
}

func deliveryCommentBelongsToIssue(comment gh.IssueComment, repo string, issueNumber int) bool {
	want := fmt.Sprintf("/repos/%s/issues/%d", strings.Trim(repo, "/"), issueNumber)
	return strings.HasSuffix(strings.TrimSpace(comment.IssueURL), want)
}

func acquireIntegrationFacts(ctx context.Context, client compatibilityGitHub, repo string, snapshot delivery.Snapshot, baseSHA, stagingSHA string) (delivery.BaseIntegrationFacts, error) {
	observation := delivery.BaseIntegrationObservation{BaseSHA: baseSHA, StagingSHA: stagingSHA, BaseRelation: delivery.RevisionUnknown, StagingRelation: delivery.RevisionUnknown}
	anchorBase, anchorStaging := snapshot.Checkpoint.GenesisBaseSHA, snapshot.Checkpoint.Coverage.StagingSHA
	if snapshot.Checkpoint.BaseIntegration != nil {
		for _, record := range snapshot.BaseIntegrations {
			if record.Digest == snapshot.Checkpoint.BaseIntegration.Digest {
				anchorBase, anchorStaging = record.BaseSHA, record.StagingSHA
				break
			}
		}
	}
	if anchorBase != "" {
		if anchorBase == baseSHA {
			observation.BaseRelation = delivery.RevisionIdentical
		} else {
			comparison, err := client.CompareCommitsContext(ctx, repo, anchorBase, baseSHA)
			if err != nil {
				return delivery.BaseIntegrationFacts{}, err
			}
			observation.BaseRelation = delivery.RelationFromComparison(comparison.Status)
		}
	}
	if anchorStaging != "" {
		if anchorStaging == stagingSHA {
			observation.StagingRelation = delivery.RevisionIdentical
		} else {
			comparison, err := client.CompareCommitsContext(ctx, repo, anchorStaging, stagingSHA)
			if err != nil {
				return delivery.BaseIntegrationFacts{}, err
			}
			observation.StagingRelation = delivery.RelationFromComparison(comparison.Status)
		}
	}
	return delivery.ClassifyBaseIntegration(snapshot, observation)
}

func deliveryReferences(checkpoint delivery.DeliveryCheckpoint) []delivery.RecordReference {
	byID := map[int64]delivery.RecordReference{}
	for _, reference := range checkpoint.ActiveRecords {
		byID[reference.Comment.DatabaseID] = reference
	}
	for _, reference := range []*delivery.RecordReference{checkpoint.CursorBoundary, checkpoint.BaseIntegration, checkpoint.PromotionIntegration} {
		if reference != nil {
			byID[reference.Comment.DatabaseID] = *reference
		}
	}
	result := make([]delivery.RecordReference, 0, len(byID))
	for _, reference := range byID {
		result = append(result, reference)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result
}

func storedDeliveryComment(comment gh.IssueComment) delivery.StoredComment {
	return delivery.StoredComment{
		Comment: delivery.CommentIdentity{DatabaseID: comment.ID, NodeID: comment.NodeID}, Body: comment.Body,
		AuthorLogin: comment.Author.Login, AuthorType: comment.Author.Type,
	}
}

func joinedFactErrors(errors ...error) string {
	parts := []string{}
	for _, err := range errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, "; ")
}
