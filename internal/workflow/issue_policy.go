package workflow

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/operation"
	"github.com/sjunepark/baton/internal/policy"
)

type IssuePolicyInput struct {
	BodyPath        string
	EventPath       string
	CurrentLabels   []string
	ConfigPath      string
	Repository      string
	EnvironmentRepo string
	Apply           bool
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
}

type IssuePolicyWorkflow struct {
	newClient func(context.Context, IssuePolicyInput) (IssuePolicyGitHub, error)
}

type IssuePolicyResult struct {
	Decision  policy.IssuePolicyDecision
	Ownership policy.IssueOwnershipDecision
	Evaluated bool
	Report    *operation.Report `json:"report,omitempty"`
}

func NewIssuePolicyWorkflow() IssuePolicyWorkflow {
	return IssuePolicyWorkflow{newClient: func(ctx context.Context, input IssuePolicyInput) (IssuePolicyGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow IssuePolicyWorkflow) Run(input IssuePolicyInput) (IssuePolicyResult, error) {
	return workflow.RunContext(context.Background(), input)
}

func (workflow IssuePolicyWorkflow) RunContext(ctx context.Context, input IssuePolicyInput) (IssuePolicyResult, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	if (input.BodyPath == "") == (input.EventPath == "") {
		return IssuePolicyResult{}, apperror.New(apperror.Usage, "issue policy requires exactly one body file or event", "")
	}
	cfg, err := loadWorkflowConfig(input.ConfigPath)
	if err != nil {
		return IssuePolicyResult{}, err
	}
	body := ""
	currentLabels := append([]string(nil), input.CurrentLabels...)
	issueNumber := 0
	eventRepo := ""
	var issueEvent gh.IssueEvent
	if input.EventPath != "" {
		content, err := os.ReadFile(input.EventPath)
		if err != nil {
			return IssuePolicyResult{}, apperror.Wrap(apperror.Usage, "issue event could not be read", err, "")
		}
		event, err := gh.ParseIssueEvent(content)
		if err != nil {
			return IssuePolicyResult{}, apperror.Wrap(apperror.Usage, "issue event could not be parsed", err, "")
		}
		issueEvent = event
		body, currentLabels, issueNumber, eventRepo = event.Body, event.Labels, event.Number, event.Repository
	} else {
		content, err := os.ReadFile(input.BodyPath)
		if err != nil {
			return IssuePolicyResult{}, apperror.Wrap(apperror.Usage, "issue body could not be read", err, "")
		}
		body = string(content)
	}
	decision := policy.ComputeIssuePolicy(policy.IssuePolicyInput{Body: body, CurrentLabels: currentLabels, Policy: cfg.IssuePolicy})
	ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
		IssueNodeID: issueEvent.NodeID, IssueNumber: issueNumber, Body: body, Labels: currentLabels, CommentsUnavailable: input.EventPath != "", Policy: cfg.IssuePolicy,
	})
	result := IssuePolicyResult{Decision: decision, Ownership: ownership, Evaluated: true}
	if !input.Apply {
		return result, nil
	}
	if input.EventPath == "" {
		return result, apperror.New(apperror.Usage, "issue-policy --apply requires --event", "")
	}
	if issueNumber == 0 {
		return result, apperror.New(apperror.Usage, "issue-policy --apply requires a repository and issue number", "")
	}
	repo, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "issue event repository", value: eventRepo},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
	)
	if err != nil {
		return result, err
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return result, err
	}
	latest, err := client.GetIssueContext(ctx, repo, issueNumber)
	if err != nil {
		classified := classifyGitHubError(err)
		report := operation.NewReport([]operation.Result{{
			ID: "issue-policy-state-preflight", Resource: fmt.Sprintf("%s#%d", repo, issueNumber), Action: "get_issue", Status: operation.StatusFailed,
			Error: operationFailure("github", "issue state preflight failed", classified),
		}})
		result.Report = &report
		return result, apperror.WithReport(classified, report)
	}
	if !issueStateCompatibleWithDecision(body, currentLabels, latest, decision) {
		report := operation.NewReport([]operation.Result{{
			ID: "issue-policy-state-preflight", Resource: fmt.Sprintf("%s#%d", repo, issueNumber), Action: "verify_issue_state", Status: operation.StatusRefused,
			Error: &operation.Failure{Category: "stale", Message: "issue body or labels changed after the event was captured"},
		}})
		result.Report = &report
		stale := apperror.New(apperror.GitHub, "issue policy decision is stale", "Let the newest issue event run, or rerun policy apply from current issue state.")
		return result, apperror.WithReport(stale, report)
	}
	if issueEvent.NodeID != "" && latest.NodeID != issueEvent.NodeID {
		report := operation.NewReport([]operation.Result{{
			ID: "issue-policy-state-preflight", Resource: fmt.Sprintf("%s#%d", repo, issueNumber), Action: "verify_issue_identity", Status: operation.StatusRefused,
			Error: &operation.Failure{Category: "stale", Message: "issue identity changed after the event was captured"},
		}})
		result.Report = &report
		stale := apperror.New(apperror.GitHub, "issue policy decision is stale", "Let the newest issue event run, or rerun policy apply from current issue state.")
		return result, apperror.WithReport(stale, report)
	}
	decision = policy.ComputeIssuePolicy(policy.IssuePolicyInput{Body: latest.Body, CurrentLabels: latest.Labels, Policy: cfg.IssuePolicy})
	result.Decision = decision
	repair := ownershipRepairFromEvent(issueEvent)
	if !decision.IsFormIssue && !repair.Requested {
		comments, err := client.ListIssueCommentsContext(ctx, repo, latest.Number)
		if err != nil {
			classified := classifyGitHubError(err)
			report := operation.NewReport([]operation.Result{{
				ID: "issue-ownership-preflight", Resource: fmt.Sprintf("%s#%d:ownership", repo, latest.Number), Action: "list_comments", Status: operation.StatusFailed,
				Error: operationFailure("github", "managed-issue ownership preflight failed", classified),
			}})
			result.Report = &report
			return result, apperror.WithReport(classified, report)
		}
		result.Ownership = policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
			IssueNodeID: latest.NodeID, IssueNumber: latest.Number, Body: latest.Body, Labels: latest.Labels,
			Comments: issueOwnershipComments(comments), Policy: cfg.IssuePolicy,
		})
		report := operation.NewReport(nil)
		result.Report = &report
		return result, nil
	}
	report, ownership, err := applyIssueDecisionWithOwnershipContext(ctx, client, repo, latest, decision, cfg.IssuePolicy.PolicyCommentMarker, policy.QualityGateLabel(cfg.IssuePolicy), repair)
	result.Ownership = ownership
	result.Report = &report
	if err != nil {
		return result, apperror.WithReport(err, report)
	}
	return result, nil
}

func issueStateCompatibleWithDecision(eventBody string, eventLabels []string, latest gh.Issue, decision policy.IssuePolicyDecision) bool {
	if latest.Body != eventBody {
		return false
	}
	eventSet := labelSet(eventLabels)
	latestSet := labelSet(latest.Labels)
	allowedAdds := labelSet(decision.LabelsToAdd)
	allowedRemovals := labelSet(decision.LabelsToRemove)
	for label := range latestSet {
		if _, existed := eventSet[label]; existed {
			continue
		}
		if _, allowed := allowedAdds[label]; !allowed {
			return false
		}
	}
	for label := range eventSet {
		if _, remains := latestSet[label]; remains {
			continue
		}
		if _, allowed := allowedRemovals[label]; !allowed {
			return false
		}
	}
	return true
}

func labelSet(labels []string) map[string]struct{} {
	result := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		result[strings.ToLower(strings.TrimSpace(label))] = struct{}{}
	}
	return result
}

// IssuePolicyGitHub is the mutation boundary needed to apply an issue policy
// decision. The GitHub adapter only exposes transport-shaped primitives.
type IssuePolicyGitHub interface {
	GetIssueContext(context.Context, string, int) (gh.Issue, error)
	AddIssueLabelsContext(context.Context, string, int, []string) error
	RemoveIssueLabelContext(context.Context, string, int, string) error
	ListIssueCommentsContext(context.Context, string, int) ([]gh.IssueComment, error)
	UpdateIssueCommentContext(context.Context, string, int64, string) error
	CreateIssueCommentContext(context.Context, string, int, string) error
}

type ownershipRepair struct {
	Requested bool
	CommentID int64
}

func ownershipRepairFromEvent(event gh.IssueEvent) ownershipRepair {
	trusted := strings.EqualFold(event.CommentAuthor.Login, policy.ManagedIssueTrustedLogin) && strings.EqualFold(event.CommentAuthor.Type, "Bot")
	ownershipEvidence := strings.Contains(event.CommentBody, "<!-- baton-managed-issue:v1") || strings.Contains(event.CommentPreviousBody, "<!-- baton-managed-issue:v1")
	return ownershipRepair{Requested: event.CommentID != 0 && trusted && ownershipEvidence && (event.Action == "edited" || event.Action == "deleted"), CommentID: event.CommentID}
}

func applyIssueDecisionWithOwnershipContext(ctx context.Context, client IssuePolicyGitHub, repo string, issue gh.Issue, decision policy.IssuePolicyDecision, marker, qualityGateLabel string, repair ownershipRepair) (operation.Report, policy.IssueOwnershipDecision, error) {
	if !decision.IsFormIssue && !repair.Requested {
		return operation.NewReport(nil), policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels}), nil
	}
	comments, err := client.ListIssueCommentsContext(ctx, repo, issue.Number)
	if err != nil {
		classified := classifyGitHubError(err)
		return operation.NewReport([]operation.Result{{
			ID: "issue-policy-preflight", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: "list_comments", Status: operation.StatusFailed,
			Error: operationFailure("github", "issue policy preflight failed", classified),
		}}), policy.IssueOwnershipDecision{}, classified
	}
	ownershipComments := issueOwnershipComments(comments)
	ownership := policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
		IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels, Comments: ownershipComments,
	})
	record := policy.NewManagedIssueRecord(issue.NodeID, issue.Number)
	repairEditedRecord := false
	if ownership.Source == policy.IssueOwnershipInvalid && repair.Requested && repair.CommentID != 0 {
		for index := range comments {
			if comments[index].ID != repair.CommentID || !strings.EqualFold(comments[index].Author.Login, policy.ManagedIssueTrustedLogin) || !strings.EqualFold(comments[index].Author.Type, "Bot") {
				continue
			}
			comments[index].Body = policy.RenderManagedIssueRecord(record)
			repairEditedRecord = true
			break
		}
		if repairEditedRecord {
			ownership = policy.ClassifyIssueOwnership(policy.IssueOwnershipInput{
				IssueNodeID: issue.NodeID, IssueNumber: issue.Number, Body: issue.Body, Labels: issue.Labels, Comments: issueOwnershipComments(comments),
			})
		}
	}
	if ownership.Source == policy.IssueOwnershipInvalid && hasTrustedOwnershipMarker(comments) {
		failure := apperror.New(apperror.Policy, "managed issue ownership record is invalid", "Repair the trusted ownership record before retrying issue policy.")
		return operation.NewReport([]operation.Result{{
			ID: "issue-ownership-preflight", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: "verify_ownership_record", Status: operation.StatusRefused,
			Error: &operation.Failure{Category: "policy", Message: strings.Join(ownership.Errors, "; ")},
		}}), ownership, failure
	}
	var existingID int64
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			existingID = comment.ID
			break
		}
	}
	type mutation struct {
		result operation.Result
		apply  func() error
	}
	mutations := []mutation{}
	if repairEditedRecord {
		body := policy.RenderManagedIssueRecord(record)
		mutations = append(mutations, mutation{result: operation.Result{
			ID: "issue-ownership-record", Resource: fmt.Sprintf("%s#%d:ownership", repo, issue.Number), Action: "repair_ownership_record", Status: operation.StatusNotAttempted,
		}, apply: func() error {
			return client.UpdateIssueCommentContext(ctx, repo, repair.CommentID, body)
		}})
	} else if ownership.Source == policy.IssueOwnershipRecord {
		mutations = append(mutations, mutation{result: operation.Result{
			ID: "issue-ownership-record", Resource: fmt.Sprintf("%s#%d:ownership", repo, issue.Number), Action: "create_ownership_record", Status: operation.StatusUnchanged,
		}})
	} else {
		body := policy.RenderManagedIssueRecord(record)
		mutations = append(mutations, mutation{result: operation.Result{
			ID: "issue-ownership-record", Resource: fmt.Sprintf("%s#%d:ownership", repo, issue.Number), Action: "create_ownership_record", Status: operation.StatusNotAttempted,
		}, apply: func() error {
			return client.CreateIssueCommentContext(ctx, repo, issue.Number, body)
		}})
	}
	labelsToAdd := append([]string(nil), decision.LabelsToAdd...)
	if repair.Requested && !containsLabel(issue.Labels, policy.ManagedIssueIndexLabel) {
		labelsToAdd = append(labelsToAdd, policy.ManagedIssueIndexLabel)
	}
	if len(labelsToAdd) > 0 {
		labels := labelsToAdd
		mutations = append(mutations, mutation{result: operation.Result{ID: "issue-labels-add", Resource: fmt.Sprintf("%s#%d", repo, issue.Number), Action: "add_labels", Status: operation.StatusNotAttempted}, apply: func() error {
			return client.AddIssueLabelsContext(ctx, repo, issue.Number, labels)
		}})
	}
	if !decision.IsFormIssue {
		decision.LabelsToRemove = nil
		decision.PolicyCommentBody = nil
		existingID = 0
	}
	for _, label := range decision.LabelsToRemove {
		label := label
		mutations = append(mutations, mutation{result: operation.Result{ID: "issue-label-remove-" + label, Resource: fmt.Sprintf("%s#%d:%s", repo, issue.Number, label), Action: "remove_label", Status: operation.StatusNotAttempted}, apply: func() error {
			return client.RemoveIssueLabelContext(ctx, repo, issue.Number, label)
		}})
	}
	if decision.PolicyCommentBody == nil {
		if existingID != 0 {
			mutations = append(mutations, mutation{result: operation.Result{ID: "issue-policy-comment", Resource: fmt.Sprintf("%s#%d:comment", repo, issue.Number), Action: "clear_comment", Status: operation.StatusNotAttempted}, apply: func() error {
				return client.UpdateIssueCommentContext(ctx, repo, existingID, policy.ClearIssuePolicyComment(marker, qualityGateLabel))
			}})
		}
	} else if existingID != 0 {
		body := *decision.PolicyCommentBody
		mutations = append(mutations, mutation{result: operation.Result{ID: "issue-policy-comment", Resource: fmt.Sprintf("%s#%d:comment", repo, issue.Number), Action: "update_comment", Status: operation.StatusNotAttempted}, apply: func() error {
			return client.UpdateIssueCommentContext(ctx, repo, existingID, body)
		}})
	} else {
		body := *decision.PolicyCommentBody
		mutations = append(mutations, mutation{result: operation.Result{ID: "issue-policy-comment", Resource: fmt.Sprintf("%s#%d:comment", repo, issue.Number), Action: "create_comment", Status: operation.StatusNotAttempted}, apply: func() error {
			return client.CreateIssueCommentContext(ctx, repo, issue.Number, body)
		}})
	}
	results := make([]operation.Result, len(mutations))
	for index := range mutations {
		results[index] = mutations[index].result
	}
	for index := range mutations {
		if mutations[index].result.Status == operation.StatusUnchanged {
			continue
		}
		if err := mutations[index].apply(); err != nil {
			classified := classifyMutationError(err)
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "issue policy mutation failed", classified)
			return operation.NewReport(results), ownership, classified
		}
		results[index].Status = operation.StatusApplied
		if results[index].ID == "issue-ownership-record" {
			ownership.Managed = true
			ownership.Source = policy.IssueOwnershipRecord
			ownership.Record = &record
			ownership.Diagnostics = []string{"managed-issue ownership record persisted"}
			ownership.Errors = []string{}
		}
	}
	ownership.Managed = true
	ownership.Source = policy.IssueOwnershipRecord
	ownership.Record = &record
	ownership.Errors = []string{}
	return operation.NewReport(results), ownership, nil
}

func issueOwnershipComments(comments []gh.IssueComment) []policy.IssueOwnershipComment {
	result := make([]policy.IssueOwnershipComment, 0, len(comments))
	for _, comment := range comments {
		result = append(result, policy.IssueOwnershipComment{ID: comment.ID, Body: comment.Body, AuthorLogin: comment.Author.Login, AuthorType: comment.Author.Type})
	}
	return result
}

func hasTrustedOwnershipMarker(comments []gh.IssueComment) bool {
	for _, comment := range comments {
		if strings.EqualFold(comment.Author.Login, policy.ManagedIssueTrustedLogin) && strings.EqualFold(comment.Author.Type, "Bot") && strings.Contains(comment.Body, "<!-- baton-managed-issue:") {
			return true
		}
	}
	return false
}

func classifyMutationError(err error) error {
	if err == nil {
		return nil
	}
	return classifyGitHubError(err)
}
