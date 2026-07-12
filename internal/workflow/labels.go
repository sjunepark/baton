package workflow

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/auth"
	"github.com/sjunepark/baton/internal/gh"
	"github.com/sjunepark/baton/internal/labels"
	"github.com/sjunepark/baton/internal/operation"
)

type LabelSyncInput struct {
	Repository      string
	EnvironmentRepo string
	ManifestPath    string
	WorkingDir      string
	ConfigPath      string
	Apply           bool
	GitHubAPIURL    string
	GitHubToken     string
	GHToken         string
}

type LabelSyncGitHub interface {
	ListLabelsContext(context.Context, string) ([]gh.Label, error)
	CreateLabelContext(context.Context, string, gh.Label) error
	UpdateLabelContext(context.Context, string, gh.Label) error
}

type LabelSyncWorkflow struct {
	newClient func(context.Context, LabelSyncInput) (LabelSyncGitHub, error)
}

func NewLabelSyncWorkflow() LabelSyncWorkflow {
	return LabelSyncWorkflow{newClient: func(ctx context.Context, input LabelSyncInput) (LabelSyncGitHub, error) {
		credentials, err := auth.DiscoverContext(ctx, auth.Inputs{GitHubToken: input.GitHubToken, GHToken: input.GHToken})
		if err != nil {
			return nil, apperror.Wrap(apperror.Auth, "GitHub credentials are not available", err, "")
		}
		return gh.NewClientWithCredentials(input.GitHubAPIURL, credentials, http.DefaultClient), nil
	}}
}

func (workflow LabelSyncWorkflow) Run(input LabelSyncInput) (labels.SyncPlan, error) {
	return workflow.RunContext(context.Background(), input)
}

func (workflow LabelSyncWorkflow) RunContext(ctx context.Context, input LabelSyncInput) (labels.SyncPlan, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	normalized, err := resolveRepositoryIdentities(
		repositoryIdentityInput{source: "--repo", value: input.Repository},
		repositoryIdentityInput{source: "GITHUB_REPOSITORY", value: input.EnvironmentRepo},
	)
	if err != nil {
		return labels.SyncPlan{}, err
	}
	manifestPath, err := resolveManifestPath(ctx, input.ManifestPath, input.WorkingDir, input.ConfigPath)
	if err != nil {
		return labels.SyncPlan{}, err
	}
	manifest, err := labels.LoadManifest(manifestPath)
	if err != nil {
		return labels.SyncPlan{}, apperror.Wrap(apperror.Config, "label manifest could not be loaded", err, "Check the label manifest path and contents, then retry.")
	}
	client, err := workflow.newClient(ctx, input)
	if err != nil {
		return labels.SyncPlan{}, err
	}
	existing, err := client.ListLabelsContext(ctx, normalized)
	if err != nil {
		return labels.SyncPlan{}, classifyGitHubError(err)
	}
	existingLabels := make([]labels.Label, 0, len(existing))
	for _, label := range existing {
		existingLabels = append(existingLabels, labels.Label{Name: label.Name, Color: label.Color, Description: label.Description})
	}
	plan := labels.PlanSync(normalized, manifest.Labels, existingLabels)
	if !input.Apply {
		return plan, nil
	}
	results := make([]operation.Result, len(plan.Changes))
	for index, change := range plan.Changes {
		results[index] = operation.Result{ID: fmt.Sprintf("label-%03d", index+1), Resource: change.Name, Action: change.Action, Status: operation.StatusNotAttempted}
		if change.Action == "ok" {
			results[index].Status = operation.StatusUnchanged
		}
	}
	latest, err := client.ListLabelsContext(ctx, normalized)
	if err != nil {
		return plan, classifyGitHubError(err)
	}
	latestLabels := make([]labels.Label, 0, len(latest))
	for _, label := range latest {
		latestLabels = append(latestLabels, labels.Label{Name: label.Name, Color: label.Color, Description: label.Description})
	}
	latestPlan := labels.PlanSync(normalized, manifest.Labels, latestLabels)
	if !reflect.DeepEqual(latestPlan.Changes, plan.Changes) {
		for index := range results {
			if results[index].Status == operation.StatusNotAttempted {
				results[index].Status = operation.StatusRefused
				results[index].Error = &operation.Failure{Category: "stale", Message: "label state changed after planning"}
			}
		}
		report := operation.NewReport(results)
		plan.Report = &report
		return plan, apperror.WithReport(apperror.New(apperror.GitHub, "label plan is stale", "Run `baton sync-labels --dry-run --json` and review the new plan."), report)
	}
	for index, change := range plan.Changes {
		label := gh.Label{Name: change.Name, Color: change.Color, Description: change.Description}
		switch change.Action {
		case "create":
			err = client.CreateLabelContext(ctx, normalized, label)
		case "update":
			err = client.UpdateLabelContext(ctx, normalized, label)
		}
		if err != nil {
			classified := classifyGitHubError(err)
			results[index].Status = operation.StatusFailed
			results[index].Error = operationFailure("github", "label mutation failed", classified)
			report := operation.NewReport(results)
			plan.Report = &report
			return plan, apperror.WithReport(classified, report)
		}
		if change.Action != "ok" {
			results[index].Status = operation.StatusApplied
		}
	}
	report := operation.NewReport(results)
	plan.Report = &report
	return plan, nil
}
