package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sjunepark/baton/internal/delivery"
)

const ManagedWorkEvidenceMarkerV1 = "baton-managed-work-evidence:v1"

type ManagedWorkPolicyEvidence struct {
	Version             int                              `json:"version"`
	Kind                string                           `json:"kind"`
	Repository          delivery.RepositoryIdentity      `json:"repository"`
	PullRequest         delivery.ResourceIdentity        `json:"pullRequest"`
	BaseBranch          string                           `json:"baseBranch"`
	WorkBranch          string                           `json:"workBranch"`
	BaseSHA             string                           `json:"baseSha"`
	HeadSHA             string                           `json:"headSha"`
	ProseDigest         string                           `json:"proseDigest"`
	PolicySchemaVersion int                              `json:"policySchemaVersion"`
	Allowed             bool                             `json:"allowed"`
	Issues              []delivery.ManagedIssueReference `json:"issues"`
	Writer              delivery.WriterProvenance        `json:"writer"`
	Digest              string                           `json:"digest"`
}

type ManagedWorkEvidenceComment struct {
	ID          int64
	NodeID      string
	IssueURL    string
	Body        string
	AuthorLogin string
	AuthorType  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ManagedWorkEvidenceMatch struct {
	Evidence ManagedWorkPolicyEvidence
	Comment  ManagedWorkEvidenceComment
}

var managedWorkEvidencePattern = regexp.MustCompile(`^<!-- baton-managed-work-evidence:v1 (\{[^\r\n]*\}) -->$`)

func NewManagedWorkPolicyEvidence(evidence ManagedWorkPolicyEvidence) (ManagedWorkPolicyEvidence, error) {
	evidence.Version = 1
	evidence.Kind = "managedWorkPolicyEvidence"
	evidence.BaseBranch = strings.TrimSpace(evidence.BaseBranch)
	evidence.WorkBranch = strings.TrimSpace(evidence.WorkBranch)
	evidence.BaseSHA = strings.TrimSpace(evidence.BaseSHA)
	evidence.HeadSHA = strings.TrimSpace(evidence.HeadSHA)
	evidence.ProseDigest = strings.TrimSpace(evidence.ProseDigest)
	evidence.Issues = append([]delivery.ManagedIssueReference(nil), evidence.Issues...)
	sort.Slice(evidence.Issues, func(i, j int) bool { return evidence.Issues[i].Number < evidence.Issues[j].Number })
	evidence.Digest = ""
	evidence.Digest = managedWorkEvidenceDigest(evidence)
	if err := validateManagedWorkPolicyEvidence(evidence); err != nil {
		return ManagedWorkPolicyEvidence{}, err
	}
	return evidence, nil
}

func ManagedWorkProseDigest(title, body string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(title) + "\x00" + strings.TrimSpace(body)))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func RenderManagedWorkPolicyEvidence(evidence ManagedWorkPolicyEvidence) (string, error) {
	if err := validateManagedWorkPolicyEvidence(evidence); err != nil {
		return "", err
	}
	content, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	return "<!-- " + ManagedWorkEvidenceMarkerV1 + " " + string(content) + " -->", nil
}

func FindTrustedManagedWorkPolicyEvidence(comments []ManagedWorkEvidenceComment) (*ManagedWorkEvidenceMatch, error) {
	var match *ManagedWorkEvidenceMatch
	for _, comment := range comments {
		body := strings.TrimSpace(comment.Body)
		if !strings.Contains(body, "baton-managed-work-evidence:") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(comment.AuthorLogin), ManagedIssueTrustedLogin) || !strings.EqualFold(strings.TrimSpace(comment.AuthorType), "Bot") {
			continue
		}
		parts := managedWorkEvidencePattern.FindStringSubmatch(body)
		if len(parts) != 2 {
			return nil, fmt.Errorf("trusted pull-request comment %d contains malformed managed-work policy evidence", comment.ID)
		}
		decoder := json.NewDecoder(bytes.NewReader([]byte(parts[1])))
		decoder.DisallowUnknownFields()
		var evidence ManagedWorkPolicyEvidence
		if err := decoder.Decode(&evidence); err != nil {
			return nil, fmt.Errorf("trusted pull-request comment %d contains unreadable managed-work policy evidence", comment.ID)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("trusted pull-request comment %d contains trailing managed-work policy evidence data", comment.ID)
		}
		if err := validateManagedWorkPolicyEvidence(evidence); err != nil {
			return nil, fmt.Errorf("trusted pull-request comment %d: %w", comment.ID, err)
		}
		candidate := &ManagedWorkEvidenceMatch{Evidence: evidence, Comment: comment}
		if match == nil || managedWorkEvidenceCommentAfter(comment, match.Comment) {
			match = candidate
		}
	}
	return match, nil
}

func managedWorkEvidenceCommentAfter(left, right ManagedWorkEvidenceComment) bool {
	leftTime, rightTime := left.UpdatedAt, right.UpdatedAt
	if leftTime.IsZero() {
		leftTime = left.CreatedAt
	}
	if rightTime.IsZero() {
		rightTime = right.CreatedAt
	}
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	return left.ID > right.ID
}

func validateManagedWorkPolicyEvidence(evidence ManagedWorkPolicyEvidence) error {
	if evidence.Version != 1 || evidence.Kind != "managedWorkPolicyEvidence" {
		return errors.New("managed-work policy evidence version or kind is unsupported")
	}
	if strings.TrimSpace(evidence.Repository.Host) == "" || strings.TrimSpace(evidence.Repository.FullName) == "" || strings.TrimSpace(evidence.Repository.NodeID) == "" {
		return errors.New("managed-work policy evidence repository identity is incomplete")
	}
	if evidence.PullRequest.Number <= 0 || strings.TrimSpace(evidence.PullRequest.NodeID) == "" || evidence.BaseBranch == "" || evidence.WorkBranch == "" {
		return errors.New("managed-work policy evidence pull-request identity is incomplete")
	}
	if !validEvidenceSHA(evidence.BaseSHA) || !validEvidenceSHA(evidence.HeadSHA) || !validEvidenceDigest(evidence.ProseDigest) {
		return errors.New("managed-work policy evidence revision is incomplete")
	}
	if evidence.PolicySchemaVersion <= 0 || strings.TrimSpace(evidence.Writer.Workflow) == "" || evidence.Writer.RunID <= 0 {
		return errors.New("managed-work policy evidence provenance is incomplete")
	}
	seen := map[int]struct{}{}
	for _, issue := range evidence.Issues {
		if issue.Number <= 0 || strings.TrimSpace(issue.NodeID) == "" || !validEvidenceDigest(issue.OwnershipDigest) {
			return errors.New("managed-work policy evidence issue identity is incomplete")
		}
		if _, duplicate := seen[issue.Number]; duplicate {
			return fmt.Errorf("managed-work policy evidence repeats issue #%d", issue.Number)
		}
		seen[issue.Number] = struct{}{}
	}
	if evidence.Allowed && len(evidence.Issues) == 0 {
		return errors.New("allowed managed-work policy evidence has no managed issues")
	}
	if !evidence.Allowed && len(evidence.Issues) != 0 {
		return errors.New("rejected managed-work policy evidence must not authorize issues")
	}
	if evidence.Digest != managedWorkEvidenceDigest(evidence) {
		return errors.New("managed-work policy evidence digest is invalid")
	}
	return nil
}

func managedWorkEvidenceDigest(evidence ManagedWorkPolicyEvidence) string {
	evidence.Digest = ""
	content, err := json.Marshal(evidence)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validEvidenceSHA(value string) bool {
	if len(strings.TrimSpace(value)) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validEvidenceDigest(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
