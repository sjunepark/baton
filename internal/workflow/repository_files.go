package workflow

import (
	"context"
	"path/filepath"

	"github.com/sjunepark/baton/internal/apperror"
	"github.com/sjunepark/baton/internal/config"
	gitadapter "github.com/sjunepark/baton/internal/git"
	"github.com/sjunepark/baton/internal/install"
	"github.com/sjunepark/baton/internal/labels"
)

type RepositoryFilesWorkflow struct{}

type InitInput struct {
	Root           string
	Apply          bool
	Overwrite      bool
	GoInstall      string
	InstallCommand string
}

func (RepositoryFilesWorkflow) Init(input InitInput) (install.Plan, error) {
	root := input.Root
	if root == "" {
		root = "."
		if resolved, err := gitadapter.RepositoryRoot(root); err == nil {
			root = resolved
		}
	}
	options := install.Options{GoInstall: input.GoInstall, InstallCommand: input.InstallCommand}
	var (
		plan install.Plan
		err  error
	)
	if input.Apply {
		plan, err = install.ApplyWithOptions(root, input.Overwrite, options)
	} else {
		plan, err = install.PreviewWithOptions(root, options)
	}
	if err != nil {
		if plan.Report != nil {
			return plan, apperror.WithReport(apperror.Wrap(apperror.Config, err.Error(), err, ""), *plan.Report)
		}
		return plan, apperror.Wrap(apperror.Config, err.Error(), err, "")
	}
	return plan, nil
}

type ConfigMigrationInput struct {
	From      string
	To        string
	Apply     bool
	Overwrite bool
	Full      bool
	BodyLimit int
}

type ConfigMigrationResult struct {
	SchemaVersion    int           `json:"schemaVersion"`
	Kind             string        `json:"kind"`
	From             string        `json:"from"`
	To               string        `json:"to"`
	Action           string        `json:"action"`
	Content          string        `json:"content,omitempty"`
	ContentChars     int           `json:"contentChars,omitempty"`
	ContentTruncated bool          `json:"contentTruncated,omitempty"`
	ContentPreview   string        `json:"contentPreview,omitempty"`
	FullCommand      string        `json:"fullCommand,omitempty"`
	Plan             *install.Plan `json:"plan,omitempty"`
}

func (RepositoryFilesWorkflow) MigrateConfig(input ConfigMigrationInput) (ConfigMigrationResult, error) {
	if input.BodyLimit < 0 {
		return ConfigMigrationResult{}, apperror.New(apperror.Usage, "migrate-config --body-limit must be non-negative", "")
	}
	cfg, err := config.Load(input.From)
	if err != nil {
		return ConfigMigrationResult{}, apperror.Wrap(apperror.Config, err.Error(), err, "")
	}
	rendered, err := install.RenderManagedFiles(cfg, install.Options{})
	if err != nil {
		return ConfigMigrationResult{}, apperror.Wrap(apperror.Config, err.Error(), err, "")
	}
	var content []byte
	for _, file := range rendered {
		if file.Path == ".github/baton.yml" {
			content = file.Content
			break
		}
	}
	if len(content) == 0 {
		return ConfigMigrationResult{}, apperror.New(apperror.Config, "compiled repository policy did not render baton.yml", "")
	}
	root, relativeTarget := migrationTarget(input.To)
	managed := []install.ManagedFile{{Path: relativeTarget, Content: content, Ownership: "repository-policy"}}
	plan, err := install.PreviewManagedFiles(root, managed)
	if err != nil {
		return ConfigMigrationResult{}, apperror.Wrap(apperror.Config, err.Error(), err, "")
	}
	action := plan.Changes[0].Action
	result := ConfigMigrationResult{SchemaVersion: 1, Kind: "configMigration", From: input.From, To: input.To, Action: action, Plan: &plan}
	if !input.Apply {
		result.Content, result.ContentChars, result.ContentTruncated = limitText(string(content), input.BodyLimit, input.Full)
		if result.ContentTruncated {
			result.ContentPreview = result.Content
			result.FullCommand = "baton migrate-config --dry-run --full --json"
		}
		return result, nil
	}
	applied, err := install.ApplyManagedFiles(root, managed, input.Overwrite)
	result.Plan = &applied
	if err != nil {
		applicationError := apperror.Wrap(apperror.Config, err.Error(), err, "")
		if applied.Report != nil {
			return result, apperror.WithReport(applicationError, *applied.Report)
		}
		return result, applicationError
	}
	return result, nil
}

func migrationTarget(target string) (string, string) {
	if filepath.IsAbs(target) {
		return filepath.Dir(target), filepath.Base(target)
	}
	return ".", filepath.Clean(target)
}

type LabelsInput struct {
	Path       string
	WorkingDir string
	ConfigPath string
}

func (RepositoryFilesWorkflow) Labels(input LabelsInput) (labels.Manifest, error) {
	return (RepositoryFilesWorkflow{}).LabelsContext(context.Background(), input)
}

func (RepositoryFilesWorkflow) LabelsContext(ctx context.Context, input LabelsInput) (labels.Manifest, error) {
	path, err := resolveManifestPath(ctx, input.Path, input.WorkingDir, input.ConfigPath)
	if err != nil {
		return labels.Manifest{}, err
	}
	manifest, err := labels.LoadManifest(path)
	if err != nil {
		return labels.Manifest{}, apperror.Wrap(apperror.Config, err.Error(), err, "")
	}
	return manifest, nil
}

func resolveManifestPath(ctx context.Context, override, workingDir, configPath string) (string, error) {
	if override != "" {
		return override, nil
	}
	if workingDir == "" {
		workingDir = "."
	}
	root, err := gitadapter.RepositoryRootContext(ctx, workingDir)
	if err != nil {
		return "", apperror.Wrap(apperror.LocalGit, "repository root could not be resolved", err, "")
	}
	var policy config.RepositoryPolicy
	if configPath != "" {
		policy, err = config.Load(configPath)
	} else {
		policy, err = config.LoadForRepo(root)
	}
	if err != nil {
		return "", apperror.Wrap(apperror.Config, "repository policy could not be loaded", err, "")
	}
	path := policy.Labels.Manifest
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	return path, nil
}

func limitText(value string, limit int, full bool) (string, int, bool) {
	runes := []rune(value)
	if full || len(runes) <= limit {
		return value, len(runes), false
	}
	return string(runes[:limit]), len(runes), true
}
