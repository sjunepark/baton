package install

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
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

func Preview(root string) (Plan, error) {
	return plan(root)
}

func Apply(root string, overwrite bool) (Plan, error) {
	installPlan, err := plan(root)
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
		content, err := templateContent(change.Path)
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

func plan(root string) (Plan, error) {
	paths := templatePaths()
	changes := make([]FileChange, 0, len(paths))
	for _, path := range paths {
		content, err := templateContent(path)
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

func templateContent(path string) ([]byte, error) {
	name := filepath.ToSlash(filepath.Join("templates", path))
	return templatesFS.ReadFile(name)
}
