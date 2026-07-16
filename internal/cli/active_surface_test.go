package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var activeMarkdown = []string{
	"AGENTS.md",
	"ARCHITECTURE.md",
	"CONTEXT.md",
	"README.md",
	"docs/CLI_SPEC.md",
	"docs/OUTPUT_SPEC.md",
	"docs/RELEASE.md",
	"docs/REQUIREMENTS.md",
	"docs/SKILL_SPEC.md",
	"docs/USER_FLOWS.md",
	"docs/adopter-updates/README.md",
	"docs/adopter-updates/v0.7.0.md",
	"skills/baton/DISTRIBUTION.md",
	"skills/baton/SKILL.md",
	"skills/baton/references/commands.md",
	"skills/baton/references/todo-creation.md",
	"testdata/README.md",
}

func TestDocumentationLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	link := regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)
	markdown := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if !info.IsDir() && filepath.Ext(path) == ".md" {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			markdown = append(markdown, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range markdown {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		for _, match := range link.FindAllStringSubmatch(string(content), -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path := filepath.Clean(filepath.Join(root, filepath.Dir(relative), filepath.FromSlash(target)))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s links to missing %s", relative, match[1])
			}
		}
	}
}

func TestActiveDocumentationOmitsRetiredCommandContracts(t *testing.T) {
	root := repositoryRoot(t)
	retired := []string{
		"agent-work/", "Refs #", "--config", "--fields", "--format",
		"operationReport", "queueSnapshot", "nextCandidates", "repositorySnapshot",
		"baton init", "baton doctor", "baton snapshot", "baton queue", "baton pr-policy",
	}
	for _, relative := range activeMarkdown {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		for _, marker := range retired {
			if strings.Contains(string(content), marker) {
				t.Errorf("%s retains retired contract %q", relative, marker)
			}
		}
	}
}

func TestBundledSkillHasOnlyCurrentReferences(t *testing.T) {
	root := repositoryRoot(t)
	want := map[string]bool{
		"commands.md":      true,
		"todo-creation.md": true,
	}
	entries, err := os.ReadDir(filepath.Join(root, "skills/baton/references"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !want[entry.Name()] {
			t.Errorf("unexpected bundled skill reference %s", entry.Name())
		}
		delete(want, entry.Name())
	}
	for missing := range want {
		t.Errorf("missing bundled skill reference %s", missing)
	}
}

func TestMigrationEvidenceIsSelfConsistent(t *testing.T) {
	root := repositoryRoot(t)
	profiles := []struct {
		directory string
		version   string
		paths     int
	}{
		{directory: "v0.5.0", version: "v0.5.0", paths: 7},
		{directory: "v0.5.1", version: "v0.5.1", paths: 7},
		{directory: "v0.6", version: "v0.6.0", paths: 8},
	}
	wantScenarios := map[string]bool{
		"unmodified-default-install": true,
		"modified-install":           true,
		"partial-install":            true,
		"already-removed":            true,
	}
	for _, profile := range profiles {
		t.Run(profile.version, func(t *testing.T) {
			base := filepath.Join(root, "testdata", "migration", profile.directory)
			manifest := readMigrationManifest(t, filepath.Join(base, "managed-files.json"))
			if manifest.Version != profile.version || len(manifest.Files) != profile.paths {
				t.Fatalf("manifest identity = %s/%d, want %s/%d", manifest.Version, len(manifest.Files), profile.version, profile.paths)
			}
			hashes := make(map[string]string, len(manifest.Files))
			for _, file := range manifest.Files {
				if _, duplicate := hashes[file.Path]; duplicate {
					t.Fatalf("duplicate managed path %s", file.Path)
				}
				content, err := base64.StdEncoding.DecodeString(file.ContentBase64)
				if err != nil {
					t.Fatalf("decode %s: %v", file.Path, err)
				}
				digest := sha256.Sum256(content)
				actual := hex.EncodeToString(digest[:])
				if actual != file.SHA256 {
					t.Fatalf("%s hash = %s, want %s", file.Path, actual, file.SHA256)
				}
				hashes[file.Path] = file.SHA256
			}

			inventory := readAdopterInventory(t, filepath.Join(base, "adopter-inventories.json"))
			seen := map[string]bool{}
			for _, scenario := range inventory.Scenarios {
				if !wantScenarios[scenario.Name] || seen[scenario.Name] {
					t.Fatalf("unexpected or duplicate scenario %q", scenario.Name)
				}
				seen[scenario.Name] = true
				for _, file := range scenario.Files {
					if file.State == "exact_default" && file.SHA256 == "" {
						t.Errorf("%s/%s exact default lacks a hash", scenario.Name, file.Path)
					}
					if file.SHA256 != "" && hashes[file.Path] != file.SHA256 {
						t.Errorf("%s/%s references unknown hash %s", scenario.Name, file.Path, file.SHA256)
					}
				}
			}
			if len(seen) != len(wantScenarios) {
				t.Fatalf("scenario set = %v", seen)
			}
		})
	}
}

func TestUnknownMigrationEvidenceAlwaysPreservesResources(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "testdata", "migration", "cross-version-inventories.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Scenarios []struct {
			Name            string   `json:"name"`
			FileDisposition string   `json:"fileDisposition"`
			ManualActions   []string `json:"manualActions"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"mixed-version": true, "customized-install": true, "older-version": true, "unknown-version": true}
	for _, scenario := range inventory.Scenarios {
		if !want[scenario.Name] || !strings.Contains(scenario.FileDisposition, "preserve") || len(scenario.ManualActions) == 0 {
			t.Errorf("unsafe or unexpected cross-version scenario: %+v", scenario)
		}
		delete(want, scenario.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing cross-version scenarios: %v", want)
	}
}

func TestAdopterGuidePreservesSafetyOrdering(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "docs", "adopter-updates", "v0.7.0.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(content)
	inventoryStart := strings.Index(guide, "## 1. Inventory without changing anything")
	checks := strings.Index(guide, "## 2. Remove retired required checks first")
	files := strings.Index(guide, "## 3. Review repository file removals")
	if inventoryStart < 0 || checks < 0 || files < 0 || inventoryStart >= checks || checks >= files {
		t.Fatal("guide does not remove retired required checks before workflow files")
	}
	for _, evidence := range []string{
		`repos/$repo/rulesets/$ruleset`,
		`repos/$repo/rules/branches/$encoded`,
		`repos/$repo/branches/$encoded/protection`,
		"without a mode",
		"prefix-safe update",
		"It is valid to approve no old issues.",
	} {
		if !strings.Contains(guide, evidence) {
			t.Errorf("guide lacks safety evidence %q", evidence)
		}
	}
	inventory := guide[inventoryStart:checks]
	for _, mutation := range []string{"--method POST", "--method PATCH", "--method PUT", "--method DELETE", "gh issue create", "gh issue edit"} {
		if strings.Contains(inventory, mutation) {
			t.Errorf("read-only inventory contains mutation %q", mutation)
		}
	}
}

func TestReleasePleaseExtraFilesExistAndAreMarked(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "release-please-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Packages map[string]struct {
			ExtraFiles []struct {
				Path string `json:"path"`
			} `json:"extra-files"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	pkg, ok := config.Packages["."]
	if !ok || len(pkg.ExtraFiles) == 0 {
		t.Fatal("root Release Please package has no marked version target")
	}
	for _, extra := range pkg.ExtraFiles {
		marked, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(extra.Path)))
		if err != nil {
			t.Errorf("extra-file %s: %v", extra.Path, err)
			continue
		}
		text := string(marked)
		if !strings.Contains(text, "x-release-please-start-version") || !strings.Contains(text, "x-release-please-end") {
			t.Errorf("extra-file %s lacks a complete generic marker", extra.Path)
		}
	}
}

type migrationManifest struct {
	Version string `json:"version"`
	Files   []struct {
		Path          string `json:"path"`
		SHA256        string `json:"sha256"`
		ContentBase64 string `json:"contentBase64"`
	} `json:"files"`
}

func readMigrationManifest(t *testing.T, path string) migrationManifest {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest migrationManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

type adopterInventory struct {
	Scenarios []struct {
		Name  string `json:"name"`
		Files []struct {
			Path   string `json:"path"`
			State  string `json:"state"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	} `json:"scenarios"`
}

func readAdopterInventory(t *testing.T, path string) adopterInventory {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var inventory adopterInventory
	if err := json.Unmarshal(content, &inventory); err != nil {
		t.Fatal(err)
	}
	return inventory
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
