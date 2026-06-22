package lease

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Manager struct {
	StateRoot string
}

type AcquireRequest struct {
	SourceRepoPath string
	Purpose        string
	BaseRef        string
	HeadRef        string
	NewBranch      string
	Repo           string
	Now            time.Time
	TTL            time.Duration
}

type Record struct {
	SchemaVersion  int       `json:"schemaVersion"`
	Kind           string    `json:"kind"`
	ID             string    `json:"id"`
	Repo           string    `json:"repo"`
	SourceRepoPath string    `json:"sourceRepoPath"`
	Path           string    `json:"path,omitempty"`
	WorktreePath   string    `json:"worktreePath"`
	Purpose        string    `json:"purpose"`
	BaseRef        string    `json:"baseRef"`
	HeadRef        string    `json:"headRef"`
	Owner          Owner     `json:"owner"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Status         string    `json:"status"`
}

type Owner struct {
	PID      int    `json:"pid"`
	Hostname string `json:"hostname"`
}

type ReleaseResult struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Lease         Record   `json:"lease"`
	Dirty         bool     `json:"dirty"`
	ChangedFiles  []string `json:"changedFiles"`
}

type PrunePlan struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Count         int      `json:"count"`
	Candidates    []Record `json:"candidates"`
	Help          []string `json:"help,omitempty"`
}

type PruneResult struct {
	SchemaVersion int         `json:"schemaVersion"`
	Kind          string      `json:"kind"`
	Counts        PruneCounts `json:"counts"`
	Removed       []Record    `json:"removed"`
	Skipped       []PruneSkip `json:"skipped"`
	Help          []string    `json:"help,omitempty"`
}

type PruneCounts struct {
	Removed int `json:"removed"`
	Skipped int `json:"skipped"`
}

type PruneSkip struct {
	Lease        Record   `json:"lease"`
	Reason       string   `json:"reason"`
	ChangedFiles []string `json:"changedFiles,omitempty"`
}

func DefaultStateRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".baton"
	}
	return filepath.Join(home, ".baton")
}

func NewManager(root string) Manager {
	if root == "" {
		root = DefaultStateRoot()
	}
	return Manager{StateRoot: root}
}

func (m Manager) Acquire(req AcquireRequest) (Record, error) {
	unlock, err := m.lock()
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if req.TTL == 0 {
		req.TTL = 8 * time.Hour
	}
	if req.Purpose == "" {
		return Record{}, fmt.Errorf("purpose is required")
	}
	if req.SourceRepoPath == "" {
		root, err := gitOutput("", "rev-parse", "--show-toplevel")
		if err != nil {
			return Record{}, err
		}
		req.SourceRepoPath = strings.TrimSpace(root)
	}
	if req.Repo == "" {
		req.Repo = filepath.Base(req.SourceRepoPath)
	}
	headRef := firstNonEmpty(req.HeadRef, req.NewBranch)
	if headRef == "" {
		return Record{}, fmt.Errorf("head ref or new branch is required")
	}
	active, err := m.List()
	if err != nil {
		return Record{}, err
	}
	for _, lease := range active {
		if lease.Status == "active" && lease.HeadRef == headRef {
			return Record{}, fmt.Errorf("active lease %s already owns branch %s", lease.ID, headRef)
		}
	}
	id := req.Now.UTC().Format("20060102T150405Z") + "-" + slug(req.Purpose)
	repoSlug := slug(req.Repo)
	worktreePath := filepath.Join(m.StateRoot, "worktrees", repoSlug, id, req.Repo)
	if _, err := os.Stat(worktreePath); err == nil {
		return Record{}, fmt.Errorf("worktree path already exists: %s", worktreePath)
	} else if !os.IsNotExist(err) {
		return Record{}, err
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return Record{}, err
	}
	args := []string{"worktree", "add"}
	if req.NewBranch != "" {
		if req.BaseRef == "" {
			return Record{}, fmt.Errorf("base ref is required when creating a new branch")
		}
		args = append(args, "-b", req.NewBranch, worktreePath, req.BaseRef)
	} else {
		args = append(args, worktreePath, req.HeadRef)
	}
	if _, err := gitOutput(req.SourceRepoPath, args...); err != nil {
		return Record{}, err
	}
	host, _ := os.Hostname()
	record := Record{
		SchemaVersion:  1,
		Kind:           "lease",
		ID:             id,
		Repo:           req.Repo,
		SourceRepoPath: req.SourceRepoPath,
		Path:           worktreePath,
		WorktreePath:   worktreePath,
		Purpose:        req.Purpose,
		BaseRef:        req.BaseRef,
		HeadRef:        headRef,
		Owner:          Owner{PID: os.Getpid(), Hostname: host},
		CreatedAt:      req.Now.UTC(),
		ExpiresAt:      req.Now.UTC().Add(req.TTL),
		Status:         "active",
	}
	if err := m.write(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (m Manager) lock() (func(), error) {
	if err := os.MkdirAll(m.StateRoot, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(m.StateRoot, "leases.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lease state is locked: %w", err)
	}
	file.Close()
	return func() {
		_ = os.Remove(path)
	}, nil
}

func (m Manager) ReleaseByID(id string, keepDirty bool) (ReleaseResult, error) {
	record, err := m.FindByID(id)
	if err != nil {
		return ReleaseResult{}, err
	}
	return m.release(record, keepDirty)
}

func (m Manager) ReleaseByPath(path string, keepDirty bool) (ReleaseResult, error) {
	records, err := m.List()
	if err != nil {
		return ReleaseResult{}, err
	}
	for _, record := range records {
		if record.WorktreePath == path {
			return m.release(record, keepDirty)
		}
	}
	return ReleaseResult{}, fmt.Errorf("no lease found for path %s", path)
}

func (m Manager) release(record Record, keepDirty bool) (ReleaseResult, error) {
	changed, err := changedFiles(record.WorktreePath)
	if err != nil {
		return ReleaseResult{}, err
	}
	if len(changed) > 0 && !keepDirty {
		return ReleaseResult{SchemaVersion: 1, Kind: "releaseResult", Lease: record, Dirty: true, ChangedFiles: changed}, fmt.Errorf("lease worktree is dirty")
	}
	record.Status = "released"
	if err := m.write(record); err != nil {
		return ReleaseResult{}, err
	}
	return ReleaseResult{SchemaVersion: 1, Kind: "releaseResult", Lease: record, Dirty: len(changed) > 0, ChangedFiles: changed}, nil
}

func (m Manager) List() ([]Record, error) {
	dir := filepath.Join(m.StateRoot, "leases")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var record Record
		if err := json.Unmarshal(content, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (m Manager) FindByID(id string) (Record, error) {
	content, err := os.ReadFile(m.recordPath(id))
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(content, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (m Manager) PruneDryRun(now time.Time) (PrunePlan, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	records, err := m.List()
	if err != nil {
		return PrunePlan{}, err
	}
	candidates := []Record{}
	for _, record := range records {
		if record.Status != "active" || now.After(record.ExpiresAt) {
			candidates = append(candidates, record)
		}
	}
	return PrunePlan{SchemaVersion: 1, Kind: "prunePlan", Count: len(candidates), Candidates: candidates, Help: prunePlanHelp(candidates)}, nil
}

func (m Manager) Prune(now time.Time) (PruneResult, error) {
	unlock, err := m.lock()
	if err != nil {
		return PruneResult{}, err
	}
	defer unlock()
	plan, err := m.PruneDryRun(now)
	if err != nil {
		return PruneResult{}, err
	}
	result := PruneResult{SchemaVersion: 1, Kind: "pruneResult", Removed: []Record{}, Skipped: []PruneSkip{}}
	for _, record := range plan.Candidates {
		if !m.isManagedWorktree(record.WorktreePath) {
			result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: "worktree path is not under Baton state root"})
			continue
		}
		if record.Status == "active" && processAlive(record.Owner) {
			result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: "active lease owner process is still running"})
			continue
		}
		if _, err := os.Stat(record.WorktreePath); err == nil {
			changed, err := changedFiles(record.WorktreePath)
			if err != nil {
				result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: err.Error()})
				continue
			}
			if len(changed) > 0 {
				result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: "worktree is dirty", ChangedFiles: changed})
				continue
			}
			if _, err := gitOutput(record.SourceRepoPath, "worktree", "remove", record.WorktreePath); err != nil {
				result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: err.Error()})
				continue
			}
		} else if !os.IsNotExist(err) {
			result.Skipped = append(result.Skipped, PruneSkip{Lease: record, Reason: err.Error()})
			continue
		}
		record.Status = "pruned"
		if err := m.write(record); err != nil {
			return PruneResult{}, err
		}
		result.Removed = append(result.Removed, record)
	}
	result.Counts = PruneCounts{Removed: len(result.Removed), Skipped: len(result.Skipped)}
	result.Help = pruneResultHelp(result.Counts)
	return result, nil
}

func (m Manager) write(record Record) error {
	if err := os.MkdirAll(filepath.Join(m.StateRoot, "leases"), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(m.recordPath(record.ID), content, 0o600)
}

func (m Manager) recordPath(id string) string {
	return filepath.Join(m.StateRoot, "leases", id+".json")
}

func (m Manager) isManagedWorktree(path string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(filepath.Join(m.StateRoot, "worktrees"))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, cleanPath)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func processAlive(owner Owner) bool {
	host, _ := os.Hostname()
	if owner.PID <= 0 || owner.Hostname == "" || owner.Hostname != host {
		return false
	}
	process, err := os.FindProcess(owner.PID)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func changedFiles(path string) ([]string, error) {
	out, err := gitOutput(path, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func slug(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "lease"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func prunePlanHelp(candidates []Record) []string {
	if len(candidates) == 0 {
		return []string{"No prune candidates found."}
	}
	return []string{"Run `baton prune --yes --json` to remove clean managed candidates."}
}

func pruneResultHelp(counts PruneCounts) []string {
	if counts.Skipped > 0 {
		return []string{"Inspect skipped leases before rerunning prune."}
	}
	return []string{"Run `baton leases --json` to inspect remaining leases."}
}
