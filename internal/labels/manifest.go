package labels

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	SchemaVersion int     `json:"schemaVersion" yaml:"-"`
	Kind          string  `json:"kind" yaml:"-"`
	Labels        []Label `json:"labels" yaml:"labels"`
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
	for _, label := range manifest.Labels {
		if label.Name == "" || label.Color == "" || label.Description == "" {
			return Manifest{}, fmt.Errorf("label entries require name, color, and description")
		}
	}
	return manifest, nil
}

func NormalizeColor(color string) string {
	return strings.ToUpper(strings.TrimPrefix(color, "#"))
}
