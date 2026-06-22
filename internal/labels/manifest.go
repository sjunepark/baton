package labels

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	SchemaVersion int      `json:"schemaVersion" yaml:"-"`
	Kind          string   `json:"kind" yaml:"-"`
	Count         int      `json:"count" yaml:"-"`
	Labels        []Label  `json:"labels" yaml:"labels"`
	Help          []string `json:"help,omitempty" yaml:"-"`
}

type Label struct {
	Name        string `json:"name" yaml:"name"`
	Color       string `json:"color" yaml:"color"`
	Description string `json:"description" yaml:"description"`
}

func LoadManifest(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(content)
}

func ParseManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse labels manifest: %w", err)
	}
	manifest.SchemaVersion = 1
	manifest.Kind = "labelManifest"
	manifest.Count = len(manifest.Labels)
	manifest.Help = manifestHelp(manifest.Count)
	for _, label := range manifest.Labels {
		if label.Name == "" || label.Color == "" || label.Description == "" {
			return Manifest{}, fmt.Errorf("label entries require name, color, and description")
		}
	}
	return manifest, nil
}

func manifestHelp(count int) []string {
	if count == 0 {
		return []string{"Add labels to the manifest before syncing."}
	}
	return []string{"Run `baton sync-labels --dry-run --json` to compare labels with GitHub."}
}

func NormalizeColor(color string) string {
	return strings.ToUpper(strings.TrimPrefix(color, "#"))
}
