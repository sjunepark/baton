package install

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sjunepark/baton/internal/config"
	"github.com/sjunepark/baton/internal/operation"
)

//go:embed all:templates
var templatesFS embed.FS

type Plan struct {
	SchemaVersion int               `json:"schemaVersion"`
	Kind          string            `json:"kind"`
	PlanID        string            `json:"planId"`
	Root          string            `json:"root"`
	Changes       []FileChange      `json:"changes"`
	Report        *operation.Report `json:"report,omitempty"`
}

type FileChange struct {
	Path           string       `json:"path"`
	Action         string       `json:"action"`
	Ownership      string       `json:"ownership"`
	Conflict       bool         `json:"conflict"`
	DesiredContent string       `json:"desiredContent"`
	Diff           string       `json:"diff"`
	Precondition   Precondition `json:"precondition"`
}

type Precondition struct {
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
}

type Options struct {
	GoInstall      string
	InstallCommand string
	Policy         *config.RepositoryPolicy
}

const defaultGoInstall = "github.com/sjunepark/baton/cmd/baton@v0.4.4" // x-release-please-version
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
	files, err := managedFiles(root, options)
	if err != nil {
		return Plan{}, err
	}
	return ApplyManagedFiles(root, files, overwrite)
}

func PreviewManagedFiles(root string, files []ManagedFile) (Plan, error) {
	return planFiles(root, files)
}

func ApplyManagedFiles(root string, files []ManagedFile, overwrite bool) (Plan, error) {
	installPlan, err := planFiles(root, files)
	if err != nil {
		return Plan{}, err
	}
	results := make([]operation.Result, len(installPlan.Changes))
	for index, change := range installPlan.Changes {
		results[index] = operation.Result{ID: operationID(index, change.Path), Resource: change.Path, Action: change.Action, Status: operation.StatusNotAttempted}
		if change.Action == "unchanged" {
			results[index].Status = operation.StatusUnchanged
		}
	}
	for index, change := range installPlan.Changes {
		if change.Conflict && !overwrite {
			results[index].Status = operation.StatusRefused
			results[index].Error = &operation.Failure{Category: "conflict", Message: "existing content differs from the compiled repository policy"}
		}
	}
	if report := operation.NewReport(results); report.Status == operation.ReportRefused {
		installPlan.Report = &report
		return installPlan, fmt.Errorf("managed files contain conflicts; rerun with --yes after reviewing the full diff")
	}
	if err := verifyPlanPreconditions(root, installPlan); err != nil {
		for index := range results {
			if results[index].Status == operation.StatusNotAttempted {
				results[index].Status = operation.StatusRefused
				results[index].Error = &operation.Failure{Category: "stale", Message: err.Error()}
			}
		}
		report := operation.NewReport(results)
		installPlan.Report = &report
		return installPlan, err
	}
	type stagedFile struct {
		index  int
		temp   string
		target string
	}
	staged := []stagedFile{}
	cleanup := func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}
	for index, change := range installPlan.Changes {
		if change.Action == "unchanged" {
			continue
		}
		target, err := safeTarget(root, change.Path)
		if err != nil {
			cleanup()
			return failedInstallPlan(installPlan, results, index, "preflight", err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			return failedInstallPlan(installPlan, results, index, "localFile", err)
		}
		temp, err := os.CreateTemp(filepath.Dir(target), ".baton-reconcile-*")
		if err != nil {
			cleanup()
			return failedInstallPlan(installPlan, results, index, "localFile", err)
		}
		tempName := temp.Name()
		if _, err = temp.WriteString(change.DesiredContent); err == nil {
			err = temp.Chmod(0o644)
		}
		if err == nil {
			err = temp.Sync()
		}
		if closeErr := temp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(tempName)
			cleanup()
			return failedInstallPlan(installPlan, results, index, "localFile", err)
		}
		staged = append(staged, stagedFile{index: index, temp: tempName, target: target})
	}
	for position, file := range staged {
		if err := os.Rename(file.temp, file.target); err != nil {
			for _, remaining := range staged[position:] {
				_ = os.Remove(remaining.temp)
			}
			return failedInstallPlan(installPlan, results, file.index, "localFile", err)
		}
		results[file.index].Status = operation.StatusApplied
	}
	report := operation.NewReport(results)
	installPlan.Report = &report
	return installPlan, nil
}

func plan(root string, options Options) (Plan, error) {
	files, err := managedFiles(root, options)
	if err != nil {
		return Plan{}, err
	}
	return planFiles(root, files)
}

func planFiles(root string, files []ManagedFile) (Plan, error) {
	if err := validateManagedFiles(files); err != nil {
		return Plan{}, err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, err
	}
	changes := make([]FileChange, 0, len(files))
	for _, file := range files {
		target, err := safeTarget(root, file.Path)
		if err != nil {
			return Plan{}, err
		}
		existing, err := os.ReadFile(target)
		action := "create"
		ownership := "absent"
		precondition := Precondition{}
		diff := unifiedContentDiff(file.Path, nil, file.Content)
		if err == nil {
			precondition = Precondition{Exists: true, SHA256: contentDigest(existing)}
			ownership = "repository"
			if !bytes.Contains(existing, []byte("Managed by Baton")) {
				ownership = "unmanaged"
			}
			if string(existing) == string(file.Content) {
				action = "unchanged"
				diff = ""
			} else {
				action = "overwrite"
				diff = unifiedContentDiff(file.Path, existing, file.Content)
			}
		} else if !os.IsNotExist(err) {
			return Plan{}, err
		}
		changes = append(changes, FileChange{Path: file.Path, Action: action, Ownership: ownership, Conflict: action == "overwrite", DesiredContent: string(file.Content), Diff: diff, Precondition: precondition})
	}
	result := Plan{SchemaVersion: 2, Kind: "repositoryReconciliationPlan", Root: absoluteRoot, Changes: changes}
	result.PlanID = planDigest(result)
	return result, nil
}

func validateManagedFiles(files []ManagedFile) error {
	seen := map[string]string{}
	for _, file := range files {
		if filepath.IsAbs(file.Path) {
			return fmt.Errorf("managed path %q must be repository-relative", file.Path)
		}
		clean := filepath.Clean(filepath.FromSlash(file.Path))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("managed path %q escapes the repository", file.Path)
		}
		if prior, duplicate := seen[clean]; duplicate {
			return fmt.Errorf("managed paths %q and %q resolve to the same target", prior, file.Path)
		}
		seen[clean] = file.Path
	}
	return nil
}

func templatePaths() []string {
	files, err := RenderManagedFiles(config.DefaultConfig(), Options{}.withDefaults())
	if err != nil {
		panic(err)
	}
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path
	}
	return paths
}

func templateContent(path string, options Options) ([]byte, error) {
	files, err := RenderManagedFiles(config.DefaultConfig(), options.withDefaults())
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.Path == path {
			return file.Content, nil
		}
	}
	return nil, fmt.Errorf("unknown managed file %s", path)
}

func managedFiles(root string, options Options) ([]ManagedFile, error) {
	var policy config.RepositoryPolicy
	if options.Policy != nil {
		policy = *options.Policy
	} else if loaded, _, err := config.LoadForRepoWithPath(root); err == nil {
		policy = loaded
	} else {
		// Invalid repository policy is itself a reconcile conflict. Render the
		// compiled defaults so planning can report the file instead of failing
		// before the caller can review or replace it.
		policy = config.DefaultConfig()
	}
	return RenderManagedFiles(policy, options)
}

func verifyPlanPreconditions(root string, plan Plan) error {
	for _, change := range plan.Changes {
		target, err := safeTarget(root, change.Path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(target)
		switch {
		case os.IsNotExist(err) && !change.Precondition.Exists:
			continue
		case err != nil:
			return fmt.Errorf("precondition for %s changed: %w", change.Path, err)
		case !change.Precondition.Exists:
			return fmt.Errorf("precondition for %s changed: file now exists", change.Path)
		case contentDigest(content) != change.Precondition.SHA256:
			return fmt.Errorf("precondition for %s changed: content digest differs", change.Path)
		}
	}
	return nil
}

func safeTarget(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("managed path %q must be repository-relative", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("managed path %q escapes the repository", relative)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absoluteRoot, clean)
	for current := absoluteRoot; current != filepath.Dir(target); {
		remainder, err := filepath.Rel(current, filepath.Dir(target))
		if err != nil {
			return "", err
		}
		part := strings.Split(remainder, string(filepath.Separator))[0]
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("managed path %q crosses symlink %s", relative, current)
		}
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("managed path %q is a symlink", relative)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}

func failedInstallPlan(plan Plan, results []operation.Result, index int, category string, cause error) (Plan, error) {
	results[index].Status = operation.StatusFailed
	results[index].Error = &operation.Failure{Category: category, Message: cause.Error()}
	report := operation.NewReport(results)
	plan.Report = &report
	return plan, cause
}

func operationID(index int, resource string) string {
	return fmt.Sprintf("file-%02d-%s", index+1, strings.ReplaceAll(resource, "/", "_"))
}

func contentDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func planDigest(plan Plan) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00", plan.Root)
	for _, change := range plan.Changes {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%t\x00%s\x00%s\x00", change.Path, change.Action, change.Ownership, change.Precondition.Exists, change.Precondition.SHA256, contentDigest([]byte(change.DesiredContent)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func unifiedContentDiff(path string, before, after []byte) string {
	var result strings.Builder
	fmt.Fprintf(&result, "--- a/%s\n+++ b/%s\n", path, path)
	for _, line := range strings.Split(strings.TrimSuffix(string(before), "\n"), "\n") {
		if line != "" || len(before) > 0 {
			result.WriteString("-")
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(after), "\n"), "\n") {
		if line != "" || len(after) > 0 {
			result.WriteString("+")
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}
	return result.String()
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
