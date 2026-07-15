package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/sjunepark/baton/internal/config"
)

const (
	ManagedIssueIndexLabel   = "baton:managed"
	ManagedIssueTrustedLogin = "github-actions[bot]"
)

type IssueOwnershipSource string

const (
	IssueOwnershipNone              IssueOwnershipSource = "none"
	IssueOwnershipUnavailable       IssueOwnershipSource = "unavailable"
	IssueOwnershipRecord            IssueOwnershipSource = "recordV1"
	IssueOwnershipLegacyFingerprint IssueOwnershipSource = "legacyFingerprint"
	IssueOwnershipInvalid           IssueOwnershipSource = "invalid"
)

type ManagedIssueRecord struct {
	Version     int    `json:"version"`
	Kind        string `json:"kind"`
	IssueNodeID string `json:"issueNodeId"`
	IssueNumber int    `json:"issueNumber"`
	Digest      string `json:"digest"`
}

type IssueOwnershipComment struct {
	ID          int64
	Body        string
	AuthorLogin string
	AuthorType  string
}

type IssueOwnershipInput struct {
	IssueNodeID         string
	IssueNumber         int
	Body                string
	Labels              []string
	Comments            []IssueOwnershipComment
	CommentsUnavailable bool
	Policy              config.IssuePolicy
}

type IssueOwnershipDecision struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Kind          string               `json:"kind"`
	Managed       bool                 `json:"managed"`
	Source        IssueOwnershipSource `json:"source"`
	Record        *ManagedIssueRecord  `json:"record,omitempty"`
	Diagnostics   []string             `json:"diagnostics"`
	Errors        []string             `json:"errors"`
}

var managedIssueRecordPattern = regexp.MustCompile(`<!-- baton-managed-issue:v1 (\{[^\r\n]*\}) -->`)

func ClassifyIssueOwnership(input IssueOwnershipInput) IssueOwnershipDecision {
	decision := IssueOwnershipDecision{
		SchemaVersion: 1, Kind: "managedIssueOwnership", Source: IssueOwnershipNone,
		Diagnostics: []string{}, Errors: []string{},
	}
	records := []ManagedIssueRecord{}
	for _, comment := range input.Comments {
		if !trustedManagedIssueComment(comment) {
			if strings.Contains(comment.Body, "<!-- baton-managed-issue:") {
				decision.Diagnostics = append(decision.Diagnostics, fmt.Sprintf("ignored untrusted managed-issue marker in comment %d", comment.ID))
			}
			continue
		}
		matches := managedIssueRecordPattern.FindAllStringSubmatch(comment.Body, -1)
		if strings.Contains(comment.Body, "<!-- baton-managed-issue:") && len(matches) == 0 {
			decision.Errors = append(decision.Errors, fmt.Sprintf("trusted issue comment %d contains a malformed managed-issue record", comment.ID))
			continue
		}
		for _, match := range matches {
			var record ManagedIssueRecord
			if err := json.Unmarshal([]byte(match[1]), &record); err != nil {
				decision.Errors = append(decision.Errors, fmt.Sprintf("trusted issue comment %d contains an unreadable managed-issue record", comment.ID))
				continue
			}
			if err := validateManagedIssueRecord(record, input.IssueNodeID, input.IssueNumber); err != nil {
				decision.Errors = append(decision.Errors, fmt.Sprintf("trusted issue comment %d: %v", comment.ID, err))
				continue
			}
			records = append(records, record)
		}
	}
	if len(records) > 1 {
		for _, record := range records[1:] {
			if record != records[0] {
				decision.Errors = append(decision.Errors, "trusted managed-issue records contradict each other")
				break
			}
		}
		if len(decision.Errors) == 0 {
			decision.Diagnostics = append(decision.Diagnostics, "duplicate equivalent managed-issue records exist")
		}
	}
	if len(decision.Errors) > 0 {
		decision.Source = IssueOwnershipInvalid
		return decision
	}
	if len(records) > 0 {
		decision.Managed = true
		decision.Source = IssueOwnershipRecord
		decision.Record = &records[0]
		return decision
	}
	if input.CommentsUnavailable {
		decision.Source = IssueOwnershipUnavailable
		decision.Diagnostics = append(decision.Diagnostics, "trusted ownership comments were not acquired")
		if MatchesIssueFormFingerprint(input.Body, input.Policy) {
			decision.Managed = true
			decision.Source = IssueOwnershipLegacyFingerprint
			decision.Diagnostics = append(decision.Diagnostics, "legacy form fingerprint is temporary ownership evidence and requires record backfill")
		}
		return decision
	}
	if hasLabelFold(input.Labels, ManagedIssueIndexLabel) {
		decision.Source = IssueOwnershipInvalid
		decision.Errors = append(decision.Errors, "managed-issue index exists without its trusted ownership record")
		return decision
	}
	if MatchesIssueFormFingerprint(input.Body, input.Policy) {
		decision.Managed = true
		decision.Source = IssueOwnershipLegacyFingerprint
		decision.Diagnostics = append(decision.Diagnostics, "legacy form fingerprint is temporary ownership evidence and requires record backfill")
		return decision
	}
	return decision
}

func NewManagedIssueRecord(issueNodeID string, issueNumber int) ManagedIssueRecord {
	record := ManagedIssueRecord{Version: 1, Kind: "managedIssueOwnership", IssueNodeID: strings.TrimSpace(issueNodeID), IssueNumber: issueNumber}
	record.Digest = managedIssueRecordDigest(record)
	return record
}

func RenderManagedIssueRecord(record ManagedIssueRecord) string {
	content, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return "<!-- baton-managed-issue:v1 " + string(content) + " -->"
}

func validateManagedIssueRecord(record ManagedIssueRecord, issueNodeID string, issueNumber int) error {
	if record.Version != 1 || record.Kind != "managedIssueOwnership" {
		return fmt.Errorf("managed-issue record version or kind is unsupported")
	}
	if strings.TrimSpace(record.IssueNodeID) == "" || record.IssueNodeID != strings.TrimSpace(issueNodeID) || record.IssueNumber != issueNumber {
		return fmt.Errorf("managed-issue record identity does not match issue #%d", issueNumber)
	}
	if record.Digest != managedIssueRecordDigest(record) {
		return fmt.Errorf("managed-issue record digest is invalid")
	}
	return nil
}

func managedIssueRecordDigest(record ManagedIssueRecord) string {
	payload := fmt.Sprintf("v%d\x00%s\x00%s\x00%d", record.Version, record.Kind, strings.TrimSpace(record.IssueNodeID), record.IssueNumber)
	digest := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func trustedManagedIssueComment(comment IssueOwnershipComment) bool {
	return strings.EqualFold(strings.TrimSpace(comment.AuthorLogin), ManagedIssueTrustedLogin) && strings.EqualFold(strings.TrimSpace(comment.AuthorType), "Bot")
}

func hasLabelFold(labels []string, wanted string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), wanted) {
			return true
		}
	}
	return false
}
