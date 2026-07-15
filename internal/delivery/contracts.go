package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion = 1
	// The active window carries at most 250 routine records plus one reserved
	// synchronization record and one promotion seal needed to drain it.
	MaxActiveRecords          = 252
	MaxOperationalRecords     = MaxActiveRecords - 2
	MaxSynchronizationRecords = MaxActiveRecords - 1
	MaxRetryComments          = 1000
	TrustedAuthorLogin        = "github-actions[bot]"
	TrustedAuthorType         = "Bot"
	CheckpointMarkerV1        = "baton-delivery-checkpoint:v1"
	RecordMarkerV1            = "baton-delivery-record:v1"
	DeliveryStateLabel        = "baton:delivery-state"
)

type RecordKind string

const (
	RecordStagedWork      RecordKind = "stagedWork"
	RecordPromotionPlan   RecordKind = "promotionPlan"
	RecordExclusion       RecordKind = "exclusion"
	RecordBaseIntegration RecordKind = "baseIntegration"
)

type RepositoryIdentity struct {
	Host     string `json:"host"`
	FullName string `json:"fullName"`
	NodeID   string `json:"nodeId"`
}

type ResourceIdentity struct {
	Number int    `json:"number"`
	NodeID string `json:"nodeId"`
}

type CommentIdentity struct {
	DatabaseID int64  `json:"databaseId"`
	NodeID     string `json:"nodeId"`
}

type DeliveryStoreLocator struct {
	Repository RepositoryIdentity `json:"repository"`
	Issue      ResourceIdentity   `json:"issue"`
	Checkpoint CommentIdentity    `json:"checkpoint"`
}

type WriterProvenance struct {
	Workflow string `json:"workflow"`
	RunID    int64  `json:"runId"`
}

type ManagedIssueReference struct {
	Number          int    `json:"number"`
	NodeID          string `json:"nodeId"`
	OwnershipDigest string `json:"ownershipDigest"`
}

type RecordReference struct {
	Comment  CommentIdentity `json:"comment"`
	Kind     RecordKind      `json:"kind"`
	Sequence uint64          `json:"sequence"`
	Digest   string          `json:"digest"`
	RetryID  string          `json:"retryId"`
}

type RecordHeader struct {
	Version        int                `json:"version"`
	Kind           RecordKind         `json:"kind"`
	LedgerID       string             `json:"ledgerId"`
	Repository     RepositoryIdentity `json:"repository"`
	Sequence       uint64             `json:"sequence"`
	PreviousDigest string             `json:"previousDigest"`
	RetryID        string             `json:"retryId"`
	Writer         WriterProvenance   `json:"writer"`
	Digest         string             `json:"digest"`
}

type StagedWorkRecord struct {
	RecordHeader
	PullRequest          ResourceIdentity        `json:"pullRequest"`
	StagingBranch        string                  `json:"stagingBranch"`
	BaseSHA              string                  `json:"baseSha"`
	HeadSHA              string                  `json:"headSha"`
	MergeRevision        string                  `json:"mergeRevision"`
	MergedAt             string                  `json:"mergedAt"`
	ObservedCursorDigest string                  `json:"observedCursorDigest"`
	Issues               []ManagedIssueReference `json:"issues"`
}

type PromotionPlanRecord struct {
	RecordHeader
	PullRequest    ResourceIdentity  `json:"pullRequest"`
	BaseSHA        string            `json:"baseSha"`
	HeadSHA        string            `json:"headSha"`
	CursorDigest   string            `json:"cursorDigest"`
	CoverageDigest string            `json:"coverageDigest"`
	Included       []RecordReference `json:"included"`
	Exclusions     []RecordReference `json:"exclusions"`
	SealedAt       string            `json:"sealedAt"`
}

type ExclusionRecord struct {
	RecordHeader
	StagedRecord  RecordReference  `json:"stagedRecord"`
	PullRequest   ResourceIdentity `json:"pullRequest"`
	HeadSHA       string           `json:"headSha"`
	CursorDigest  string           `json:"cursorDigest"`
	Reason        string           `json:"reason"`
	RequestedBy   string           `json:"requestedBy"`
	RequestRunID  int64            `json:"requestRunId"`
	RequestURL    string           `json:"requestUrl"`
	RequestedAt   string           `json:"requestedAt"`
	ApprovedBy    string           `json:"approvedBy"`
	ReviewID      int64            `json:"reviewId"`
	ReviewNodeID  string           `json:"reviewNodeId"`
	ReviewURL     string           `json:"reviewUrl"`
	ReviewedAt    string           `json:"reviewedAt"`
	ReviewHeadSHA string           `json:"reviewHeadSha"`
}

type IntegrationSource string

const (
	IntegrationPromotion       IntegrationSource = "promotion"
	IntegrationSynchronization IntegrationSource = "synchronization"
)

type PromotionMethod string

const (
	PromotionMerge  PromotionMethod = "merge"
	PromotionSquash PromotionMethod = "squash"
	PromotionRebase PromotionMethod = "rebase"
	// PromotionUnknown is used when GitHub reports the exact resulting base
	// commit but does not expose whether a linear merge was squash or rebase.
	PromotionUnknown PromotionMethod = "unknown"
	PromotionSync    PromotionMethod = "synchronization"
)

type BaseIntegrationRecord struct {
	RecordHeader
	Source                IntegrationSource `json:"source"`
	Method                PromotionMethod   `json:"method"`
	PullRequest           ResourceIdentity  `json:"pullRequest,omitempty"`
	PromotionBaseSHA      string            `json:"promotionBaseSha,omitempty"`
	PriorStagingSHA       string            `json:"priorStagingSha,omitempty"`
	BaseSHA               string            `json:"baseSha"`
	StagingSHA            string            `json:"stagingSha"`
	PriorCursorDigest     string            `json:"priorCursorDigest"`
	CommittedCursorDigest string            `json:"committedCursorDigest"`
	PlanDigest            string            `json:"planDigest,omitempty"`
	RecordedAt            string            `json:"recordedAt"`
}

type PromotionCursor struct {
	Version            int    `json:"version"`
	Kind               string `json:"kind"`
	Position           uint64 `json:"position"`
	ThroughSequence    uint64 `json:"throughSequence"`
	ThroughDigest      string `json:"throughDigest"`
	ConsumedPlanDigest string `json:"consumedPlanDigest,omitempty"`
	RetryID            string `json:"retryId"`
	Digest             string `json:"digest"`
}

type StagingCoverage struct {
	Version        int                `json:"version"`
	Kind           string             `json:"kind"`
	Repository     RepositoryIdentity `json:"repository"`
	StagingSHA     string             `json:"stagingSha"`
	RecordSequence uint64             `json:"recordSequence"`
	RecordDigest   string             `json:"recordDigest"`
	CursorDigest   string             `json:"cursorDigest"`
	Writer         WriterProvenance   `json:"writer"`
	ObservedAt     string             `json:"observedAt"`
	Digest         string             `json:"digest"`
}

type PromotionRecheckTarget struct {
	PullRequest ResourceIdentity `json:"pullRequest"`
	HeadSHA     string           `json:"headSha"`
}

type PendingPromotionRechecks struct {
	Cause   RecordReference          `json:"cause"`
	Targets []PromotionRecheckTarget `json:"targets"`
}

// CheckpointLink binds an epoch boundary to an exact checkpoint in another
// locked ledger issue. It makes rollover auditable without carrying old record
// references into the successor's active window.
type CheckpointLink struct {
	Locator DeliveryStoreLocator `json:"locator"`
	Digest  string               `json:"digest"`
}

type DeliveryCheckpoint struct {
	Version              int                       `json:"version"`
	Kind                 string                    `json:"kind"`
	LedgerID             string                    `json:"ledgerId"`
	Repository           RepositoryIdentity        `json:"repository"`
	Issue                ResourceIdentity          `json:"issue"`
	Generation           uint64                    `json:"generation"`
	GenesisBaseSHA       string                    `json:"genesisBaseSha,omitempty"`
	GenesisStagingSHA    string                    `json:"genesisStagingSha,omitempty"`
	WindowSequence       uint64                    `json:"windowSequence"`
	WindowDigest         string                    `json:"windowDigest"`
	HeadSequence         uint64                    `json:"headSequence"`
	HeadDigest           string                    `json:"headDigest"`
	Cursor               PromotionCursor           `json:"cursor"`
	Coverage             StagingCoverage           `json:"coverage"`
	CursorBoundary       *RecordReference          `json:"cursorBoundary,omitempty"`
	ActiveRecords        []RecordReference         `json:"activeRecords"`
	ActivePlan           *RecordReference          `json:"activePlan,omitempty"`
	BaseIntegration      *RecordReference          `json:"baseIntegration,omitempty"`
	PromotionIntegration *RecordReference          `json:"promotionIntegration,omitempty"`
	PendingRechecks      *PendingPromotionRechecks `json:"pendingPromotionRechecks,omitempty"`
	Predecessor          *CheckpointLink           `json:"predecessor,omitempty"`
	Successor            *CheckpointLink           `json:"successor,omitempty"`
	Digest               string                    `json:"digest"`
}

type StoredComment struct {
	Comment     CommentIdentity `json:"comment"`
	Body        string          `json:"body"`
	AuthorLogin string          `json:"authorLogin"`
	AuthorType  string          `json:"authorType"`
}

type StoreSnapshot struct {
	Locator    DeliveryStoreLocator `json:"locator"`
	Checkpoint StoredComment        `json:"checkpoint"`
	Records    []StoredComment      `json:"records"`
	Complete   bool                 `json:"complete"`
}

type Snapshot struct {
	Locator          DeliveryStoreLocator
	Checkpoint       DeliveryCheckpoint
	StagedWork       []StagedWorkRecord
	PromotionPlans   []PromotionPlanRecord
	Exclusions       []ExclusionRecord
	BaseIntegrations []BaseIntegrationRecord
}

type PromotionRevision struct {
	RepositoryNodeID string
	PullRequest      ResourceIdentity
	BaseSHA          string
	HeadSHA          string
}

type StagedWorkRevision struct {
	PullRequest   ResourceIdentity
	BaseSHA       string
	HeadSHA       string
	MergeRevision string
}

type ResolvedStagedWork struct {
	Record    StagedWorkRecord `json:"record"`
	Reference RecordReference  `json:"reference"`
}

// PromotionWork is the durable projection of one staged-work record selected
// by a promotion plan. Issue relationships come from the immutable record, not
// current pull-request prose.
type PromotionWork struct {
	PullRequest ResourceIdentity        `json:"pullRequest"`
	Record      RecordReference         `json:"record"`
	Issues      []ManagedIssueReference `json:"issues"`
	Excluded    bool                    `json:"excluded"`
}

// ResolvedPromotion is the complete authoritative view of one active sealed
// promotion plan.
type ResolvedPromotion struct {
	Plan           PromotionPlanRecord     `json:"plan"`
	PlanReference  RecordReference         `json:"planReference"`
	Work           []PromotionWork         `json:"work"`
	ExpectedIssues []ManagedIssueReference `json:"expectedIssues"`
}

func genesisDigest(ledgerID, repositoryNodeID string) string {
	return digestBytes([]byte("baton-delivery-genesis:v1\x00" + strings.TrimSpace(ledgerID) + "\x00" + strings.TrimSpace(repositoryNodeID)))
}

func finalizeCursor(cursor PromotionCursor) PromotionCursor {
	cursor.Version = SchemaVersion
	cursor.Kind = "promotionCursor"
	cursor.RetryID = ""
	cursor.Digest = ""
	cursor.RetryID = cursorRetryID(cursor)
	cursor.Digest = cursorDigest(cursor)
	return cursor
}

func finalizeCoverage(coverage StagingCoverage) StagingCoverage {
	coverage.Version = SchemaVersion
	coverage.Kind = "stagingCoverage"
	coverage.Digest = ""
	coverage.Digest = coverageDigest(coverage)
	return coverage
}

func finalizeStagedWork(record StagedWorkRecord) StagedWorkRecord {
	record.Version, record.Kind = SchemaVersion, RecordStagedWork
	sort.Slice(record.Issues, func(i, j int) bool { return record.Issues[i].Number < record.Issues[j].Number })
	record.RetryID, record.Digest = "", ""
	record.RetryID = stagedWorkRetryID(record)
	record.Digest = stagedWorkDigest(record)
	return record
}

func finalizePromotionPlan(record PromotionPlanRecord) PromotionPlanRecord {
	record.Version, record.Kind = SchemaVersion, RecordPromotionPlan
	sortRecordReferences(record.Included)
	sortRecordReferences(record.Exclusions)
	record.RetryID, record.Digest = "", ""
	record.RetryID = promotionPlanRetryID(record)
	record.Digest = promotionPlanDigest(record)
	return record
}

func finalizeExclusion(record ExclusionRecord) ExclusionRecord {
	record.Version, record.Kind = SchemaVersion, RecordExclusion
	record.RetryID, record.Digest = "", ""
	record.RetryID = exclusionRetryID(record)
	record.Digest = exclusionDigest(record)
	return record
}

func finalizeBaseIntegration(record BaseIntegrationRecord) BaseIntegrationRecord {
	record.Version, record.Kind = SchemaVersion, RecordBaseIntegration
	record.RetryID, record.Digest = "", ""
	record.RetryID = baseIntegrationRetryID(record)
	record.Digest = baseIntegrationDigest(record)
	return record
}

func finalizeCheckpoint(checkpoint DeliveryCheckpoint) DeliveryCheckpoint {
	checkpoint.Version = SchemaVersion
	checkpoint.Kind = "deliveryCheckpoint"
	sortRecordReferences(checkpoint.ActiveRecords)
	checkpoint.Digest = ""
	checkpoint.Digest = checkpointDigest(checkpoint)
	return checkpoint
}

func recordReference(comment CommentIdentity, header RecordHeader) RecordReference {
	return RecordReference{Comment: comment, Kind: header.Kind, Sequence: header.Sequence, Digest: header.Digest, RetryID: header.RetryID}
}

func canonicalDigest(value any) string {
	content, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("delivery contract cannot be encoded: %v", err))
	}
	return digestBytes(content)
}

func digestBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func sortRecordReferences(records []RecordReference) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Sequence != records[j].Sequence {
			return records[i].Sequence < records[j].Sequence
		}
		return records[i].Comment.DatabaseID < records[j].Comment.DatabaseID
	})
}
