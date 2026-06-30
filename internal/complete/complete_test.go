package complete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCompletion(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	record, err := Write(root, "Implemented change", "go test ./...", now)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "20260622T103000Z" {
		t.Fatalf("record = %#v", record)
	}
	if _, err := os.Stat(filepath.Join(root, "completions", "20260622T103000Z.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCommentBody(t *testing.T) {
	record := Record{Summary: "Implemented change", Validation: "go test ./..."}
	body := CommentBody(record)
	if !strings.Contains(body, "Implemented change") || !strings.Contains(body, "go test ./...") {
		t.Fatalf("body = %q", body)
	}
	if strings.Contains(body, "lease") {
		t.Fatalf("body = %q, want no lease reference", body)
	}
}
