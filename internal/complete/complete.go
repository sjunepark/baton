package complete

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sejunpark/baton/internal/lease"
)

type Record struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	ID            string    `json:"id"`
	LeaseID       string    `json:"leaseId,omitempty"`
	Summary       string    `json:"summary"`
	Validation    string    `json:"validation,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

func CommentBody(record Record) string {
	body := "Baton completion recorded.\n\nSummary:\n" + record.Summary
	if strings.TrimSpace(record.Validation) != "" {
		body += "\n\nValidation:\n" + record.Validation
	}
	if strings.TrimSpace(record.LeaseID) != "" {
		body += "\n\nLease: `" + record.LeaseID + "`"
	}
	return body
}

func Write(stateRoot, leaseID, summary, validation string, now time.Time) (Record, error) {
	if strings.TrimSpace(summary) == "" {
		return Record{}, fmt.Errorf("summary is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if stateRoot == "" {
		stateRoot = lease.DefaultStateRoot()
	}
	record := Record{
		SchemaVersion: 1,
		Kind:          "completion",
		ID:            now.UTC().Format("20060102T150405Z"),
		LeaseID:       leaseID,
		Summary:       summary,
		Validation:    validation,
		CreatedAt:     now.UTC(),
	}
	dir := filepath.Join(stateRoot, "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Record{}, err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return Record{}, err
	}
	content = append(content, '\n')
	if err := os.WriteFile(filepath.Join(dir, record.ID+".json"), content, 0o600); err != nil {
		return Record{}, err
	}
	return record, nil
}
