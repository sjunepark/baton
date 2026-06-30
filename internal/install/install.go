package install

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:templates
var templatesFS embed.FS

type Plan struct {
	SchemaVersion int          `json:"schemaVersion"`
	Kind          string       `json:"kind"`
	Root          string       `json:"root"`
	Changes       []FileChange `json:"changes"`
}

type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type Options struct {
	GoInstall      string
	InstallCommand string
}

const defaultGoInstall = "github.com/sjunepark/baton/cmd/baton@v0.2.1" // x-release-please-version
const installCommandPlaceholder = "__BATON_INSTALL_COMMAND__"

func Preview(root string) (Plan, error) {
	return PreviewWithOptions(root, Options{})
}

func PreviewWithOptions(root string, options Options) (Plan, error) {
	return plan(root, options.withDefaults())
}

func Apply(root string, overwrite bool) (Plan, error) {
	return ApplyWithOptions(root, overwrite, Options{})
}

func ApplyWithOptions(root string, overwrite bool, options Options) (Plan, error) {
	options = options.withDefaults()
	installPlan, err := plan(root, options)
	if err != nil {
		return Plan{}, err
	}
	for _, change := range installPlan.Changes {
		if change.Action == "unchanged" {
			continue
		}
		if change.Action == "overwrite" && !overwrite {
			return Plan{}, fmt.Errorf("%s already exists with different content; rerun with --yes to overwrite", change.Path)
		}
		content, err := templateContent(change.Path, options)
		if err != nil {
			return Plan{}, err
		}
		target := filepath.Join(root, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Plan{}, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return Plan{}, err
		}
	}
	return installPlan, nil
}

func plan(root string, options Options) (Plan, error) {
	paths := templatePaths()
	changes := make([]FileChange, 0, len(paths))
	for _, path := range paths {
		content, err := templateContent(path, options)
		if err != nil {
			return Plan{}, err
		}
		target := filepath.Join(root, filepath.FromSlash(path))
		existing, err := os.ReadFile(target)
		action := "create"
		if err == nil {
			if string(existing) == string(content) {
				action = "unchanged"
			} else {
				action = "overwrite"
			}
		} else if !os.IsNotExist(err) {
			return Plan{}, err
		}
		changes = append(changes, FileChange{Path: path, Action: action})
	}
	return Plan{SchemaVersion: 1, Kind: "initPlan", Root: root, Changes: changes}, nil
}

func templatePaths() []string {
	return []string{
		".github/baton.yml",
		".github/labels.yml",
		".github/ISSUE_WORKFLOW.md",
		".github/ISSUE_TEMPLATE/agent-work.yml",
		".github/workflows/issue-policy.yml",
		".github/workflows/pr-policy.yml",
	}
}

func templateContent(path string, options Options) ([]byte, error) {
	name := filepath.ToSlash(filepath.Join("templates", path))
	content, err := templatesFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	rendered := strings.ReplaceAll(string(content), defaultGoInstall, options.GoInstall)
	rendered = strings.ReplaceAll(rendered, installCommandPlaceholder, indentInstallCommand(options.InstallCommand))
	return []byte(rendered), nil
}

func (options Options) withDefaults() Options {
	if options.GoInstall == "" {
		options.GoInstall = defaultGoInstall
	}
	if options.InstallCommand == "" {
		options.InstallCommand = "mkdir -p \"$RUNNER_TEMP/baton-bin\"\nGOBIN=\"$RUNNER_TEMP/baton-bin\" go install " + options.GoInstall + "\necho \"$RUNNER_TEMP/baton-bin\" >> \"$GITHUB_PATH\""
	}
	return options
}

func indentInstallCommand(command string) string {
	lines := strings.Split(strings.TrimRight(command, "\n"), "\n")
	return strings.Join(lines, "\n          ")
}
