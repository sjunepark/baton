package delivery

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const bootstrapPlanVersion = 1

type GenesisCheckpointInput struct {
	LedgerID   string
	Repository RepositoryIdentity
	Issue      ResourceIdentity
	StagingSHA string
	BaseSHA    string
	Writer     WriterProvenance
	ObservedAt string
}

// NewGenesisCheckpoint constructs the first committed checkpoint. It does not
// need a comment identity, so callers can create and pin the checkpoint comment
// after reviewing the exact value.
func NewGenesisCheckpoint(input GenesisCheckpointInput) (DeliveryCheckpoint, error) {
	input.LedgerID = strings.TrimSpace(input.LedgerID)
	if input.LedgerID == "" {
		return DeliveryCheckpoint{}, errors.New("delivery ledger identity is empty")
	}
	if err := validateRepository(input.Repository); err != nil {
		return DeliveryCheckpoint{}, err
	}
	if err := validateResource(input.Issue, "ledger issue"); err != nil {
		return DeliveryCheckpoint{}, err
	}
	if !validSHA(input.StagingSHA) {
		return DeliveryCheckpoint{}, errors.New("genesis staging revision is invalid")
	}
	if !validSHA(input.BaseSHA) {
		return DeliveryCheckpoint{}, errors.New("genesis base revision is invalid")
	}
	if strings.TrimSpace(input.Writer.Workflow) == "" || input.Writer.RunID <= 0 {
		return DeliveryCheckpoint{}, errors.New("genesis writer provenance is incomplete")
	}
	if err := validateTimestamp(input.ObservedAt); err != nil {
		return DeliveryCheckpoint{}, fmt.Errorf("genesis observation time: %w", err)
	}

	genesis := genesisDigest(input.LedgerID, input.Repository.NodeID)
	cursor := finalizeCursor(PromotionCursor{ThroughDigest: genesis})
	coverage := finalizeCoverage(StagingCoverage{
		Repository: input.Repository, StagingSHA: input.StagingSHA,
		RecordDigest: genesis, CursorDigest: cursor.Digest,
		Writer: input.Writer, ObservedAt: input.ObservedAt,
	})
	return finalizeCheckpoint(DeliveryCheckpoint{
		LedgerID: input.LedgerID, Repository: input.Repository, Issue: input.Issue,
		Generation: 1, GenesisBaseSHA: strings.TrimSpace(input.BaseSHA), WindowDigest: genesis, HeadDigest: genesis,
		Cursor: cursor, Coverage: coverage, ActiveRecords: []RecordReference{},
	}), nil
}

type StagedWorkAppendInput struct {
	PullRequest   ResourceIdentity
	StagingBranch string
	BaseSHA       string
	HeadSHA       string
	MergeRevision string
	MergedAt      string
	Issues        []ManagedIssueReference
	Writer        WriterProvenance
}

type CheckpointPrecondition struct {
	Generation     uint64 `json:"generation"`
	Digest         string `json:"digest"`
	HeadSequence   uint64 `json:"headSequence"`
	HeadDigest     string `json:"headDigest"`
	CursorDigest   string `json:"cursorDigest"`
	CoverageDigest string `json:"coverageDigest"`
}

type PendingRecordReference struct {
	Kind     RecordKind `json:"kind"`
	Sequence uint64     `json:"sequence"`
	Digest   string     `json:"digest"`
	RetryID  string     `json:"retryId"`
}

type CheckpointAppendTemplate struct {
	Checkpoint    DeliveryCheckpoint     `json:"checkpoint"`
	PendingRecord PendingRecordReference `json:"pendingRecord"`
}

type StagedWorkRetryMatch struct {
	Record    StagedWorkRecord `json:"record"`
	Reference RecordReference  `json:"reference"`
}

type StagedWorkCommentMatch struct {
	Comment CommentIdentity  `json:"comment"`
	Record  StagedWorkRecord `json:"record"`
}

// FindStagedWorkRetry searches a bounded newest-comment acquisition for one
// exact trusted retry. It rejects duplicate matches and malformed trusted
// delivery markers instead of choosing an arbitrary orphan.
func FindStagedWorkRetry(comments []StoredComment, retryID string) (*StagedWorkCommentMatch, error) {
	if len(comments) > MaxRetryComments {
		return nil, fmt.Errorf("staged-work retry lookup acquired %d comments; cap is %d", len(comments), MaxRetryComments)
	}
	retryID = strings.TrimSpace(retryID)
	if retryID == "" {
		return nil, errors.New("staged-work retry identity is empty")
	}
	var match *StagedWorkCommentMatch
	for _, comment := range comments {
		if !strings.Contains(comment.Body, "<!-- "+RecordMarkerV1+" ") {
			continue
		}
		if err := validateTrustedComment(comment); err != nil {
			continue
		}
		parsed, err := parseRecord(comment)
		if err != nil {
			return nil, fmt.Errorf("delivery retry comment %d: %w", comment.Comment.DatabaseID, err)
		}
		record, staged := parsed.value.(StagedWorkRecord)
		if !staged || record.RetryID != retryID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("staged-work retry identity %q is ambiguous", retryID)
		}
		value := StagedWorkCommentMatch{Comment: comment.Comment, Record: record}
		match = &value
	}
	return match, nil
}

type StagedWorkAppendPlan struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Kind          string                    `json:"kind"`
	Applicable    bool                      `json:"applicable"`
	Locator       DeliveryStoreLocator      `json:"locator"`
	Precondition  CheckpointPrecondition    `json:"precondition"`
	Record        *StagedWorkRecord         `json:"record,omitempty"`
	Checkpoint    *CheckpointAppendTemplate `json:"checkpoint,omitempty"`
	Existing      *StagedWorkRetryMatch     `json:"existing,omitempty"`
}

type StagedWorkAppendCommit struct {
	Record          StagedWorkRecord   `json:"record"`
	RecordBody      string             `json:"recordBody"`
	RecordReference RecordReference    `json:"recordReference"`
	Checkpoint      DeliveryCheckpoint `json:"checkpoint"`
	CheckpointBody  string             `json:"checkpointBody"`
}

// LookupStagedWorkRetry finds an already committed semantic append. Snapshot
// acquisition is bounded by the checkpoint, so absence is meaningful only for
// the supplied complete Snapshot.
func LookupStagedWorkRetry(snapshot Snapshot, retryID string) (*StagedWorkRetryMatch, error) {
	retryID = strings.TrimSpace(retryID)
	if retryID == "" {
		return nil, errors.New("staged-work retry identity is empty")
	}
	var match *StagedWorkRetryMatch
	for _, record := range snapshot.StagedWork {
		if record.RetryID != retryID {
			continue
		}
		if err := validateStagedWork(record); err != nil {
			return nil, fmt.Errorf("staged-work retry %q is invalid: %w", retryID, err)
		}
		reference, found := checkpointRecordReference(snapshot.Checkpoint, record.Digest)
		if !found {
			return nil, fmt.Errorf("staged-work retry %q is not committed by the checkpoint", retryID)
		}
		if match != nil {
			return nil, fmt.Errorf("staged-work retry identity %q is ambiguous", retryID)
		}
		value := StagedWorkRetryMatch{Record: record, Reference: reference}
		match = &value
	}
	return match, nil
}

// PlanStagedWorkAppend binds a new immutable record and next-checkpoint
// template to the exact supplied checkpoint. The checkpoint template has one
// intentional hole: the immutable comment identity returned by transport.
func PlanStagedWorkAppend(snapshot Snapshot, input StagedWorkAppendInput) (StagedWorkAppendPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return StagedWorkAppendPlan{}, fmt.Errorf("staged-work append checkpoint: %w", err)
	}
	if checkpoint.PendingRechecks != nil {
		return StagedWorkAppendPlan{}, errors.New("pending promotion rechecks must be cleared before staged work is appended")
	}
	precondition := checkpointPrecondition(checkpoint)
	basePlan := StagedWorkAppendPlan{
		SchemaVersion: SchemaVersion, Kind: "stagedWorkAppendPlan", Applicable: true,
		Locator: snapshot.Locator, Precondition: precondition,
	}
	record := finalizeStagedWork(StagedWorkRecord{
		RecordHeader: RecordHeader{
			LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository,
			Sequence: checkpoint.HeadSequence + 1, PreviousDigest: checkpoint.HeadDigest,
			Writer: input.Writer,
		},
		PullRequest: input.PullRequest, StagingBranch: strings.TrimSpace(input.StagingBranch),
		BaseSHA: input.BaseSHA, HeadSHA: input.HeadSHA, MergeRevision: input.MergeRevision,
		MergedAt: input.MergedAt, ObservedCursorDigest: checkpoint.Cursor.Digest,
		Issues: append([]ManagedIssueReference(nil), input.Issues...),
	})
	if err := validateStagedWork(record); err != nil {
		return StagedWorkAppendPlan{}, err
	}
	if existing, err := LookupStagedWorkRetry(snapshot, record.RetryID); err != nil {
		return StagedWorkAppendPlan{}, err
	} else if existing != nil {
		basePlan.Applicable = false
		basePlan.Existing = existing
		return basePlan, nil
	}
	if len(checkpoint.ActiveRecords) >= MaxOperationalRecords {
		return StagedWorkAppendPlan{}, fmt.Errorf("delivery checkpoint operational-record cap reached: %d; synchronization and promotion-seal slots are reserved", MaxOperationalRecords)
	}

	next := stagedWorkCheckpointTemplate(checkpoint, record)

	pending := PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}
	basePlan.Record = &record
	basePlan.Checkpoint = &CheckpointAppendTemplate{Checkpoint: next, PendingRecord: pending}
	return basePlan, nil
}

// FinalizeStagedWorkAppend fills the immutable comment identity and produces
// the exact canonical record and checkpoint bodies. current must still be the
// checkpoint the plan was built from.
func FinalizeStagedWorkAppend(plan StagedWorkAppendPlan, current DeliveryCheckpoint, comment CommentIdentity) (StagedWorkAppendCommit, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil {
		return StagedWorkAppendCommit{}, errors.New("staged-work append plan has no pending append")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return StagedWorkAppendCommit{}, fmt.Errorf("staged-work append current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition {
		return StagedWorkAppendCommit{}, errors.New("staged-work append plan is stale")
	}
	if err := validateCommentIdentity(comment); err != nil {
		return StagedWorkAppendCommit{}, fmt.Errorf("staged-work record comment: %w", err)
	}
	for _, reference := range referencedComments(current) {
		if reference.Comment == comment || reference.Comment.DatabaseID == comment.DatabaseID || reference.Comment.NodeID == comment.NodeID {
			return StagedWorkAppendCommit{}, errors.New("staged-work record comment identity is already committed")
		}
	}
	record := *plan.Record
	if err := validateStagedWork(record); err != nil {
		return StagedWorkAppendCommit{}, fmt.Errorf("staged-work append record: %w", err)
	}
	if record.LedgerID != current.LedgerID || record.Repository != current.Repository || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.ObservedCursorDigest != current.Cursor.Digest {
		return StagedWorkAppendCommit{}, errors.New("staged-work append record is not bound to the current checkpoint")
	}
	pending := plan.Checkpoint.PendingRecord
	if pending != (PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}) {
		return StagedWorkAppendCommit{}, errors.New("staged-work append template does not match its record")
	}
	reference := recordReference(comment, record.RecordHeader)
	expectedTemplate := stagedWorkCheckpointTemplate(current, record)
	if canonicalDigest(plan.Checkpoint.Checkpoint) != canonicalDigest(expectedTemplate) {
		return StagedWorkAppendCommit{}, errors.New("staged-work checkpoint template is not bound to the current checkpoint")
	}
	next := expectedTemplate
	next.ActiveRecords = append(append([]RecordReference(nil), next.ActiveRecords...), reference)
	next = finalizeCheckpoint(next)
	if err := validateCheckpoint(next, plan.Locator); err != nil {
		return StagedWorkAppendCommit{}, fmt.Errorf("staged-work append checkpoint template: %w", err)
	}
	checkpointBody, err := RenderCheckpointIndex(plan.Locator, next)
	if err != nil {
		return StagedWorkAppendCommit{}, err
	}
	return StagedWorkAppendCommit{
		Record: record, RecordBody: renderStagedWork(record), RecordReference: reference,
		Checkpoint: next, CheckpointBody: checkpointBody,
	}, nil
}

// AdoptStagedWorkRetry replaces attempt-local writer metadata with the exact
// trusted orphan found by semantic retry identity. The orphan must still extend
// the checkpoint the plan was built from.
func AdoptStagedWorkRetry(plan StagedWorkAppendPlan, current DeliveryCheckpoint, record StagedWorkRecord) (StagedWorkAppendPlan, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || record.RetryID != plan.Record.RetryID {
		return StagedWorkAppendPlan{}, errors.New("staged-work retry does not match the append plan")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return StagedWorkAppendPlan{}, fmt.Errorf("staged-work retry current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.ObservedCursorDigest != current.Cursor.Digest {
		return StagedWorkAppendPlan{}, errors.New("staged-work retry is stale")
	}
	if err := validateStagedWork(record); err != nil {
		return StagedWorkAppendPlan{}, err
	}
	next := stagedWorkCheckpointTemplate(current, record)
	plan.Record = &record
	plan.Checkpoint = &CheckpointAppendTemplate{
		Checkpoint: next,
		PendingRecord: PendingRecordReference{
			Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID,
		},
	}
	return plan, nil
}

func stagedWorkCheckpointTemplate(checkpoint DeliveryCheckpoint, record StagedWorkRecord) DeliveryCheckpoint {
	next := checkpoint
	next.Generation++
	next.HeadSequence, next.HeadDigest = record.Sequence, record.Digest
	next.ActiveRecords = append([]RecordReference(nil), checkpoint.ActiveRecords...)
	next.ActivePlan = nil
	next.Coverage = finalizeCoverage(StagingCoverage{
		Repository: checkpoint.Repository, StagingSHA: record.MergeRevision,
		RecordSequence: record.Sequence, RecordDigest: record.Digest,
		CursorDigest: checkpoint.Cursor.Digest, Writer: record.Writer, ObservedAt: record.MergedAt,
	})
	next.Digest = ""
	return next
}

func checkpointPrecondition(checkpoint DeliveryCheckpoint) CheckpointPrecondition {
	return CheckpointPrecondition{
		Generation: checkpoint.Generation, Digest: checkpoint.Digest,
		HeadSequence: checkpoint.HeadSequence, HeadDigest: checkpoint.HeadDigest,
		CursorDigest: checkpoint.Cursor.Digest, CoverageDigest: checkpoint.Coverage.Digest,
	}
}

type CoverageAdvanceInput struct {
	StagingSHA string
	Writer     WriterProvenance
	ObservedAt string
}

type CoverageAdvancePlan struct {
	SchemaVersion  int                    `json:"schemaVersion"`
	Kind           string                 `json:"kind"`
	Applicable     bool                   `json:"applicable"`
	Locator        DeliveryStoreLocator   `json:"locator"`
	Precondition   CheckpointPrecondition `json:"precondition"`
	Checkpoint     *DeliveryCheckpoint    `json:"checkpoint,omitempty"`
	CheckpointBody string                 `json:"checkpointBody,omitempty"`
}

// PlanCoverageAdvance commits a complete staging scan that found no
// additional managed work records. It advances only the coverage boundary;
// the record chain and promotion cursor remain unchanged.
func PlanCoverageAdvance(snapshot Snapshot, input CoverageAdvanceInput) (CoverageAdvancePlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return CoverageAdvancePlan{}, fmt.Errorf("coverage advance checkpoint: %w", err)
	}
	if checkpoint.PendingRechecks != nil {
		return CoverageAdvancePlan{}, errors.New("pending promotion rechecks must be cleared before coverage advances")
	}
	plan := CoverageAdvancePlan{
		SchemaVersion: SchemaVersion,
		Kind:          "coverageAdvancePlan",
		Applicable:    checkpoint.Coverage.StagingSHA != input.StagingSHA,
		Locator:       snapshot.Locator,
		Precondition:  checkpointPrecondition(checkpoint),
	}
	if !validSHA(input.StagingSHA) {
		return CoverageAdvancePlan{}, errors.New("coverage staging revision is invalid")
	}
	if strings.TrimSpace(input.Writer.Workflow) == "" || input.Writer.RunID <= 0 {
		return CoverageAdvancePlan{}, errors.New("coverage writer provenance is incomplete")
	}
	if err := validateTimestamp(input.ObservedAt); err != nil {
		return CoverageAdvancePlan{}, fmt.Errorf("coverage observation time: %w", err)
	}
	if !plan.Applicable {
		return plan, nil
	}
	next := checkpoint
	next.Generation++
	next.ActivePlan = nil
	next.Coverage = finalizeCoverage(StagingCoverage{
		Repository:     checkpoint.Repository,
		StagingSHA:     input.StagingSHA,
		RecordSequence: checkpoint.HeadSequence,
		RecordDigest:   checkpoint.HeadDigest,
		CursorDigest:   checkpoint.Cursor.Digest,
		Writer:         input.Writer,
		ObservedAt:     input.ObservedAt,
	})
	next.Digest = ""
	next = finalizeCheckpoint(next)
	if err := validateCheckpoint(next, snapshot.Locator); err != nil {
		return CoverageAdvancePlan{}, fmt.Errorf("coverage advance result: %w", err)
	}
	body, err := RenderCheckpointIndex(snapshot.Locator, next)
	if err != nil {
		return CoverageAdvancePlan{}, err
	}
	plan.Checkpoint = &next
	plan.CheckpointBody = body
	return plan, nil
}

func checkpointRecordReference(checkpoint DeliveryCheckpoint, digest string) (RecordReference, bool) {
	for _, reference := range checkpoint.ActiveRecords {
		if reference.Digest == digest {
			return reference, true
		}
	}
	if checkpoint.CursorBoundary != nil && checkpoint.CursorBoundary.Digest == digest {
		return *checkpoint.CursorBoundary, true
	}
	if checkpoint.BaseIntegration != nil && checkpoint.BaseIntegration.Digest == digest {
		return *checkpoint.BaseIntegration, true
	}
	if checkpoint.PromotionIntegration != nil && checkpoint.PromotionIntegration.Digest == digest {
		return *checkpoint.PromotionIntegration, true
	}
	return RecordReference{}, false
}

type PromotionSealInput struct {
	Revision PromotionRevision `json:"revision"`
	Writer   WriterProvenance  `json:"writer"`
	SealedAt string            `json:"sealedAt"`
}

type PromotionPlanRetryMatch struct {
	Record    PromotionPlanRecord `json:"record"`
	Reference RecordReference     `json:"reference"`
}

type PromotionPlanCommentMatch struct {
	Comment CommentIdentity     `json:"comment"`
	Record  PromotionPlanRecord `json:"record"`
}

type PromotionSealPlan struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	Kind           string                    `json:"kind"`
	Applicable     bool                      `json:"applicable"`
	Locator        DeliveryStoreLocator      `json:"locator"`
	Precondition   CheckpointPrecondition    `json:"precondition"`
	Work           []PromotionWork           `json:"work"`
	ExpectedIssues []ManagedIssueReference   `json:"expectedIssues"`
	Record         *PromotionPlanRecord      `json:"record,omitempty"`
	Checkpoint     *CheckpointAppendTemplate `json:"checkpoint,omitempty"`
	Existing       *PromotionPlanRetryMatch  `json:"existing,omitempty"`
}

type PromotionSealCommit struct {
	Record          PromotionPlanRecord `json:"record"`
	RecordBody      string              `json:"recordBody"`
	RecordReference RecordReference     `json:"recordReference"`
	Checkpoint      DeliveryCheckpoint  `json:"checkpoint"`
	CheckpointBody  string              `json:"checkpointBody"`
}

// FindPromotionPlanRetry searches only the bounded newest ledger window for
// one exact orphaned promotion-plan append.
func FindPromotionPlanRetry(comments []StoredComment, retryID string) (*PromotionPlanCommentMatch, error) {
	if len(comments) > MaxRetryComments {
		return nil, fmt.Errorf("promotion-plan retry lookup acquired %d comments; cap is %d", len(comments), MaxRetryComments)
	}
	retryID = strings.TrimSpace(retryID)
	if retryID == "" {
		return nil, errors.New("promotion-plan retry identity is empty")
	}
	var match *PromotionPlanCommentMatch
	for _, comment := range comments {
		if !strings.Contains(comment.Body, "<!-- "+RecordMarkerV1+" ") {
			continue
		}
		if err := validateTrustedComment(comment); err != nil {
			continue
		}
		parsed, err := parseRecord(comment)
		if err != nil {
			return nil, fmt.Errorf("delivery retry comment %d: %w", comment.Comment.DatabaseID, err)
		}
		record, promotion := parsed.value.(PromotionPlanRecord)
		if !promotion || record.RetryID != retryID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("promotion-plan retry identity %q is ambiguous", retryID)
		}
		value := PromotionPlanCommentMatch{Comment: comment.Comment, Record: record}
		match = &value
	}
	return match, nil
}

// PlanPromotionSeal deterministically accounts for every active staged-work
// record as included or covered by one reviewed exclusion. Exact current seals
// are idempotent; changed revisions, cursor, coverage, or records produce a new
// append plan.
func PlanPromotionSeal(snapshot Snapshot, input PromotionSealInput) (PromotionSealPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return PromotionSealPlan{}, fmt.Errorf("promotion seal checkpoint: %w", err)
	}
	if checkpoint.PendingRechecks != nil {
		return PromotionSealPlan{}, errors.New("pending promotion rechecks must be cleared before a promotion is sealed")
	}
	if err := validatePromotionRevision(input.Revision, checkpoint.Repository.NodeID); err != nil {
		return PromotionSealPlan{}, err
	}
	if strings.TrimSpace(input.Writer.Workflow) == "" || input.Writer.RunID <= 0 {
		return PromotionSealPlan{}, errors.New("promotion seal writer provenance is incomplete")
	}
	if err := validateTimestamp(input.SealedAt); err != nil {
		return PromotionSealPlan{}, fmt.Errorf("promotion seal time: %w", err)
	}
	if checkpoint.Coverage.StagingSHA != input.Revision.HeadSHA || checkpoint.Coverage.CursorDigest != checkpoint.Cursor.Digest {
		return PromotionSealPlan{}, errors.New("promotion seal requires complete coverage of the exact promotion head and cursor")
	}

	work, included, exclusions, issues, err := promotionSelection(snapshot, input.Revision)
	if err != nil {
		return PromotionSealPlan{}, err
	}
	base := PromotionSealPlan{
		SchemaVersion: SchemaVersion, Kind: "promotionSealPlan", Applicable: true,
		Locator: snapshot.Locator, Precondition: checkpointPrecondition(checkpoint),
		Work: work, ExpectedIssues: issues,
	}
	if checkpoint.ActivePlan != nil {
		if resolved, resolveErr := ResolveSealedPromotion(snapshot, input.Revision); resolveErr == nil {
			base.Applicable = false
			base.Work = resolved.Work
			base.ExpectedIssues = resolved.ExpectedIssues
			base.Existing = &PromotionPlanRetryMatch{Record: resolved.Plan, Reference: resolved.PlanReference}
			return base, nil
		}
	}
	if len(checkpoint.ActiveRecords) >= MaxActiveRecords {
		return PromotionSealPlan{}, fmt.Errorf("delivery checkpoint active-record cap reached: %d", MaxActiveRecords)
	}
	record := finalizePromotionPlan(PromotionPlanRecord{
		RecordHeader: RecordHeader{
			LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository,
			Sequence: checkpoint.HeadSequence + 1, PreviousDigest: checkpoint.HeadDigest,
			Writer: input.Writer,
		},
		PullRequest: input.Revision.PullRequest, BaseSHA: input.Revision.BaseSHA, HeadSHA: input.Revision.HeadSHA,
		CursorDigest: checkpoint.Cursor.Digest, CoverageDigest: checkpoint.Coverage.Digest,
		Included: included, Exclusions: exclusions, SealedAt: input.SealedAt,
	})
	if err := validatePromotionPlan(record); err != nil {
		return PromotionSealPlan{}, err
	}
	if existing, err := lookupPromotionPlanRetry(snapshot, record.RetryID); err != nil {
		return PromotionSealPlan{}, err
	} else if existing != nil {
		return PromotionSealPlan{}, errors.New("matching promotion plan is committed but is not the active seal")
	}
	next := checkpoint
	next.Generation++
	next.HeadSequence, next.HeadDigest = record.Sequence, record.Digest
	next.ActiveRecords = append([]RecordReference(nil), checkpoint.ActiveRecords...)
	next.ActivePlan = nil
	next.Digest = ""
	base.Record = &record
	base.Checkpoint = &CheckpointAppendTemplate{
		Checkpoint:    next,
		PendingRecord: PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID},
	}
	return base, nil
}

func FinalizePromotionSeal(plan PromotionSealPlan, current DeliveryCheckpoint, comment CommentIdentity) (PromotionSealCommit, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil {
		return PromotionSealCommit{}, errors.New("promotion seal plan has no pending append")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return PromotionSealCommit{}, fmt.Errorf("promotion seal current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition {
		return PromotionSealCommit{}, errors.New("promotion seal plan is stale")
	}
	if err := validateCommentIdentity(comment); err != nil {
		return PromotionSealCommit{}, fmt.Errorf("promotion plan comment: %w", err)
	}
	for _, reference := range referencedComments(current) {
		if reference.Comment == comment || reference.Comment.DatabaseID == comment.DatabaseID || reference.Comment.NodeID == comment.NodeID {
			return PromotionSealCommit{}, errors.New("promotion plan comment identity is already committed")
		}
	}
	record := *plan.Record
	if err := validatePromotionPlan(record); err != nil {
		return PromotionSealCommit{}, err
	}
	if record.LedgerID != current.LedgerID || record.Repository != current.Repository || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.CursorDigest != current.Cursor.Digest || record.CoverageDigest != current.Coverage.Digest {
		return PromotionSealCommit{}, errors.New("promotion plan record is not bound to the current checkpoint")
	}
	pending := plan.Checkpoint.PendingRecord
	if pending != (PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}) {
		return PromotionSealCommit{}, errors.New("promotion seal template does not match its record")
	}
	expected := current
	expected.Generation++
	expected.HeadSequence, expected.HeadDigest = record.Sequence, record.Digest
	expected.ActiveRecords = append([]RecordReference(nil), current.ActiveRecords...)
	expected.ActivePlan = nil
	expected.Digest = ""
	if canonicalDigest(plan.Checkpoint.Checkpoint) != canonicalDigest(expected) {
		return PromotionSealCommit{}, errors.New("promotion seal checkpoint template is not bound to the current checkpoint")
	}
	reference := recordReference(comment, record.RecordHeader)
	next := expected
	next.ActiveRecords = append(next.ActiveRecords, reference)
	next.ActivePlan = &reference
	next = finalizeCheckpoint(next)
	if err := validateCheckpoint(next, plan.Locator); err != nil {
		return PromotionSealCommit{}, fmt.Errorf("promotion seal checkpoint: %w", err)
	}
	body, err := RenderCheckpointIndex(plan.Locator, next)
	if err != nil {
		return PromotionSealCommit{}, err
	}
	return PromotionSealCommit{Record: record, RecordBody: renderPromotionPlan(record), RecordReference: reference, Checkpoint: next, CheckpointBody: body}, nil
}

// AdoptPromotionPlanRetry replaces attempt-local provenance and seal time with
// the exact trusted orphan found by semantic retry identity.
func AdoptPromotionPlanRetry(plan PromotionSealPlan, current DeliveryCheckpoint, record PromotionPlanRecord) (PromotionSealPlan, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || record.RetryID != plan.Record.RetryID {
		return PromotionSealPlan{}, errors.New("promotion-plan retry does not match the seal plan")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return PromotionSealPlan{}, fmt.Errorf("promotion-plan retry current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.CursorDigest != current.Cursor.Digest || record.CoverageDigest != current.Coverage.Digest {
		return PromotionSealPlan{}, errors.New("promotion-plan retry is stale")
	}
	if err := validatePromotionPlan(record); err != nil {
		return PromotionSealPlan{}, err
	}
	next := current
	next.Generation++
	next.HeadSequence, next.HeadDigest = record.Sequence, record.Digest
	next.ActiveRecords = append([]RecordReference(nil), current.ActiveRecords...)
	next.ActivePlan = nil
	next.Digest = ""
	plan.Record = &record
	plan.Checkpoint = &CheckpointAppendTemplate{
		Checkpoint: next,
		PendingRecord: PendingRecordReference{
			Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID,
		},
	}
	return plan, nil
}

func promotionSelection(snapshot Snapshot, revision PromotionRevision) ([]PromotionWork, []RecordReference, []RecordReference, []ManagedIssueReference, error) {
	active := make(map[string]RecordReference, len(snapshot.Checkpoint.ActiveRecords))
	for _, reference := range snapshot.Checkpoint.ActiveRecords {
		active[reference.Digest] = reference
	}
	staged := make(map[string]StagedWorkRecord, len(snapshot.StagedWork))
	for _, record := range snapshot.StagedWork {
		if reference, found := active[record.Digest]; found && reference.Kind == RecordStagedWork {
			staged[record.Digest] = record
		}
	}
	exclusionByStaged := map[string]RecordReference{}
	for _, exclusion := range snapshot.Exclusions {
		reference, committed := active[exclusion.Digest]
		if !committed || reference.Kind != RecordExclusion {
			continue
		}
		if exclusion.PullRequest != revision.PullRequest || exclusion.HeadSHA != revision.HeadSHA || exclusion.ReviewHeadSHA != revision.HeadSHA || exclusion.CursorDigest != snapshot.Checkpoint.Cursor.Digest {
			continue
		}
		if expected, found := active[exclusion.StagedRecord.Digest]; !found || expected != exclusion.StagedRecord {
			return nil, nil, nil, nil, fmt.Errorf("promotion exclusion %q does not reference active staged work", exclusion.Digest)
		}
		if _, found := staged[exclusion.StagedRecord.Digest]; !found {
			return nil, nil, nil, nil, fmt.Errorf("promotion exclusion %q references missing staged work", exclusion.Digest)
		}
		if _, duplicate := exclusionByStaged[exclusion.StagedRecord.Digest]; duplicate {
			return nil, nil, nil, nil, fmt.Errorf("active staged work %q has multiple reviewed exclusions", exclusion.StagedRecord.Digest)
		}
		exclusionByStaged[exclusion.StagedRecord.Digest] = reference
	}
	references := make([]RecordReference, 0, len(staged))
	for digest := range staged {
		references = append(references, active[digest])
	}
	sortRecordReferences(references)
	work := make([]PromotionWork, 0, len(references))
	included := make([]RecordReference, 0, len(references))
	exclusions := make([]RecordReference, 0, len(exclusionByStaged))
	issuesByNumber := map[int]ManagedIssueReference{}
	for _, reference := range references {
		record := staged[reference.Digest]
		exclusion, excluded := exclusionByStaged[reference.Digest]
		work = append(work, PromotionWork{PullRequest: record.PullRequest, Record: reference, Issues: append([]ManagedIssueReference(nil), record.Issues...), Excluded: excluded})
		if excluded {
			exclusions = append(exclusions, exclusion)
			continue
		}
		included = append(included, reference)
		for _, issue := range record.Issues {
			if previous, duplicate := issuesByNumber[issue.Number]; duplicate && previous != issue {
				return nil, nil, nil, nil, fmt.Errorf("managed issue #%d has contradictory delivery identities", issue.Number)
			}
			issuesByNumber[issue.Number] = issue
		}
	}
	sortRecordReferences(exclusions)
	issues := make([]ManagedIssueReference, 0, len(issuesByNumber))
	for _, issue := range issuesByNumber {
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	return work, included, exclusions, issues, nil
}

func validatePromotionRevision(revision PromotionRevision, repositoryNodeID string) error {
	if strings.TrimSpace(revision.RepositoryNodeID) == "" || revision.RepositoryNodeID != repositoryNodeID {
		return errors.New("promotion revision repository identity is stale")
	}
	if err := validateResource(revision.PullRequest, "promotion pull request"); err != nil {
		return err
	}
	if !validSHA(revision.BaseSHA) || !validSHA(revision.HeadSHA) {
		return errors.New("promotion revision base or head is invalid")
	}
	return nil
}

func lookupPromotionPlanRetry(snapshot Snapshot, retryID string) (*PromotionPlanRetryMatch, error) {
	var match *PromotionPlanRetryMatch
	for _, record := range snapshot.PromotionPlans {
		if record.RetryID != retryID {
			continue
		}
		reference, found := checkpointRecordReference(snapshot.Checkpoint, record.Digest)
		if !found {
			return nil, fmt.Errorf("promotion-plan retry %q is not committed by the checkpoint", retryID)
		}
		if match != nil {
			return nil, fmt.Errorf("promotion-plan retry identity %q is ambiguous", retryID)
		}
		value := PromotionPlanRetryMatch{Record: record, Reference: reference}
		match = &value
	}
	return match, nil
}

type PromotionConsumptionInput struct {
	Revision      PromotionRevision `json:"revision"`
	MergeRevision string            `json:"mergeRevision"`
	Method        PromotionMethod   `json:"method"`
	Writer        WriterProvenance  `json:"writer"`
	RecordedAt    string            `json:"recordedAt"`
}

type BaseIntegrationRetryMatch struct {
	Record    BaseIntegrationRecord `json:"record"`
	Reference RecordReference       `json:"reference"`
}

type BaseIntegrationCommentMatch struct {
	Comment CommentIdentity       `json:"comment"`
	Record  BaseIntegrationRecord `json:"record"`
}

type PromotionConsumptionPlan struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Kind           string                     `json:"kind"`
	Applicable     bool                       `json:"applicable"`
	Locator        DeliveryStoreLocator       `json:"locator"`
	Precondition   CheckpointPrecondition     `json:"precondition"`
	PlanDigest     string                     `json:"planDigest"`
	ExpectedIssues []ManagedIssueReference    `json:"expectedIssues"`
	Record         *BaseIntegrationRecord     `json:"record,omitempty"`
	Checkpoint     *CheckpointAppendTemplate  `json:"checkpoint,omitempty"`
	Existing       *BaseIntegrationRetryMatch `json:"existing,omitempty"`
}

type PromotionConsumptionCommit struct {
	Record          BaseIntegrationRecord `json:"record"`
	RecordBody      string                `json:"recordBody"`
	RecordReference RecordReference       `json:"recordReference"`
	Checkpoint      DeliveryCheckpoint    `json:"checkpoint"`
	CheckpointBody  string                `json:"checkpointBody"`
}

// PlanPromotionConsumption converts one exact active seal into a recoverable
// base-integration append and a cursor checkpoint. A checkpoint that already
// names the same promotion event returns a total no-op without resolving the
// old plan or exposing its issues.
func PlanPromotionConsumption(snapshot Snapshot, input PromotionConsumptionInput) (PromotionConsumptionPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return PromotionConsumptionPlan{}, fmt.Errorf("promotion consumption checkpoint: %w", err)
	}
	if checkpoint.PendingRechecks != nil {
		return PromotionConsumptionPlan{}, errors.New("pending promotion rechecks must be cleared before promotion consumption")
	}
	if err := validatePromotionRevision(input.Revision, checkpoint.Repository.NodeID); err != nil {
		return PromotionConsumptionPlan{}, err
	}
	if !validSHA(input.MergeRevision) {
		return PromotionConsumptionPlan{}, errors.New("promotion result revision is invalid")
	}
	if strings.TrimSpace(input.Writer.Workflow) == "" || input.Writer.RunID <= 0 {
		return PromotionConsumptionPlan{}, errors.New("promotion consumption writer provenance is incomplete")
	}
	if err := validateTimestamp(input.RecordedAt); err != nil {
		return PromotionConsumptionPlan{}, fmt.Errorf("promotion consumption record time: %w", err)
	}
	if input.Method != PromotionMerge && input.Method != PromotionSquash && input.Method != PromotionRebase && input.Method != PromotionUnknown {
		return PromotionConsumptionPlan{}, errors.New("promotion consumption method is unsupported")
	}
	base := PromotionConsumptionPlan{
		SchemaVersion: SchemaVersion, Kind: "promotionConsumptionPlan", Applicable: true,
		Locator: snapshot.Locator, Precondition: checkpointPrecondition(checkpoint), ExpectedIssues: []ManagedIssueReference{},
	}
	if existing, err := committedPromotionConsumption(snapshot, input); err != nil {
		return PromotionConsumptionPlan{}, err
	} else if existing != nil {
		base.Applicable = false
		base.PlanDigest = existing.Record.PlanDigest
		base.Existing = existing
		return base, nil
	}

	resolved, err := ResolveSealedPromotion(snapshot, input.Revision)
	if err != nil {
		return PromotionConsumptionPlan{}, fmt.Errorf("promotion consumption requires an exact active seal: %w", err)
	}
	if resolved.Plan.CursorDigest != checkpoint.Cursor.Digest {
		return PromotionConsumptionPlan{}, errors.New("promotion consumption cursor is stale")
	}
	throughSequence, throughDigest := checkpoint.Cursor.ThroughSequence, checkpoint.Cursor.ThroughDigest
	var boundary *RecordReference
	if checkpoint.CursorBoundary != nil {
		value := *checkpoint.CursorBoundary
		boundary = &value
	}
	for _, work := range resolved.Work {
		if work.Record.Sequence > throughSequence {
			throughSequence, throughDigest = work.Record.Sequence, work.Record.Digest
			value := work.Record
			boundary = &value
		}
	}
	cursor := finalizeCursor(PromotionCursor{
		Position: checkpoint.Cursor.Position + 1, ThroughSequence: throughSequence,
		ThroughDigest: throughDigest, ConsumedPlanDigest: resolved.Plan.Digest,
	})
	record := finalizeBaseIntegration(BaseIntegrationRecord{
		RecordHeader: RecordHeader{
			LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository,
			Sequence: checkpoint.HeadSequence + 1, PreviousDigest: checkpoint.HeadDigest, Writer: input.Writer,
		},
		Source: IntegrationPromotion, Method: input.Method, PullRequest: input.Revision.PullRequest,
		PromotionBaseSHA: input.Revision.BaseSHA, BaseSHA: input.MergeRevision, StagingSHA: input.Revision.HeadSHA,
		PriorCursorDigest: checkpoint.Cursor.Digest, CommittedCursorDigest: cursor.Digest,
		PlanDigest: resolved.Plan.Digest, RecordedAt: input.RecordedAt,
	})
	if err := validateBaseIntegration(record); err != nil {
		return PromotionConsumptionPlan{}, err
	}
	next := promotionConsumptionCheckpointTemplate(checkpoint, record, cursor, boundary)
	base.PlanDigest = resolved.Plan.Digest
	base.ExpectedIssues = append([]ManagedIssueReference(nil), resolved.ExpectedIssues...)
	base.Record = &record
	base.Checkpoint = &CheckpointAppendTemplate{
		Checkpoint:    next,
		PendingRecord: PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID},
	}
	return base, nil
}

func committedPromotionConsumption(snapshot Snapshot, input PromotionConsumptionInput) (*BaseIntegrationRetryMatch, error) {
	var match *BaseIntegrationRetryMatch
	for _, record := range snapshot.BaseIntegrations {
		if record.Source != IntegrationPromotion || record.PullRequest != input.Revision.PullRequest || record.PromotionBaseSHA != input.Revision.BaseSHA || record.StagingSHA != input.Revision.HeadSHA || record.BaseSHA != input.MergeRevision {
			continue
		}
		reference, found := checkpointRecordReference(snapshot.Checkpoint, record.Digest)
		promotionReference := snapshot.Checkpoint.PromotionIntegration
		if promotionReference == nil && snapshot.Checkpoint.BaseIntegration != nil {
			promotionReference = snapshot.Checkpoint.BaseIntegration
		}
		if !found || promotionReference == nil || *promotionReference != reference || record.CommittedCursorDigest != snapshot.Checkpoint.Cursor.Digest || record.PlanDigest != snapshot.Checkpoint.Cursor.ConsumedPlanDigest {
			return nil, errors.New("matching promotion integration is not durably committed")
		}
		if match != nil {
			return nil, errors.New("promotion event has multiple committed base integrations")
		}
		value := BaseIntegrationRetryMatch{Record: record, Reference: reference}
		match = &value
	}
	return match, nil
}

func promotionConsumptionCheckpointTemplate(checkpoint DeliveryCheckpoint, record BaseIntegrationRecord, cursor PromotionCursor, boundary *RecordReference) DeliveryCheckpoint {
	next := checkpoint
	next.Generation++
	next.WindowSequence, next.WindowDigest = record.Sequence, record.Digest
	next.HeadSequence, next.HeadDigest = record.Sequence, record.Digest
	next.Cursor = cursor
	next.CursorBoundary = boundary
	next.ActiveRecords = []RecordReference{}
	next.ActivePlan = nil
	next.BaseIntegration = nil
	next.PromotionIntegration = nil
	next.Coverage = finalizeCoverage(StagingCoverage{
		Repository: checkpoint.Repository, StagingSHA: record.StagingSHA,
		RecordSequence: record.Sequence, RecordDigest: record.Digest,
		CursorDigest: cursor.Digest, Writer: record.Writer, ObservedAt: record.RecordedAt,
	})
	next.Digest = ""
	return next
}

func FinalizePromotionConsumption(plan PromotionConsumptionPlan, current DeliveryCheckpoint, comment CommentIdentity) (PromotionConsumptionCommit, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil {
		return PromotionConsumptionCommit{}, errors.New("promotion consumption plan has no pending append")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return PromotionConsumptionCommit{}, fmt.Errorf("promotion consumption current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition {
		return PromotionConsumptionCommit{}, errors.New("promotion consumption plan is stale")
	}
	if err := validateCommentIdentity(comment); err != nil {
		return PromotionConsumptionCommit{}, fmt.Errorf("base-integration comment: %w", err)
	}
	for _, reference := range referencedComments(current) {
		if reference.Comment == comment || reference.Comment.DatabaseID == comment.DatabaseID || reference.Comment.NodeID == comment.NodeID {
			return PromotionConsumptionCommit{}, errors.New("base-integration comment identity is already committed")
		}
	}
	record := *plan.Record
	if err := validateBaseIntegration(record); err != nil {
		return PromotionConsumptionCommit{}, err
	}
	if record.LedgerID != current.LedgerID || record.Repository != current.Repository || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.PriorCursorDigest != current.Cursor.Digest {
		return PromotionConsumptionCommit{}, errors.New("base-integration record is not bound to the current checkpoint")
	}
	pending := plan.Checkpoint.PendingRecord
	if pending != (PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}) {
		return PromotionConsumptionCommit{}, errors.New("promotion consumption template does not match its record")
	}
	expected := promotionConsumptionCheckpointTemplate(current, record, plan.Checkpoint.Checkpoint.Cursor, plan.Checkpoint.Checkpoint.CursorBoundary)
	if canonicalDigest(plan.Checkpoint.Checkpoint) != canonicalDigest(expected) {
		return PromotionConsumptionCommit{}, errors.New("promotion consumption checkpoint template is not bound to the current checkpoint")
	}
	reference := recordReference(comment, record.RecordHeader)
	next := expected
	next.BaseIntegration = &reference
	next.PromotionIntegration = &reference
	next = finalizeCheckpoint(next)
	if err := validateCheckpoint(next, plan.Locator); err != nil {
		return PromotionConsumptionCommit{}, fmt.Errorf("promotion consumption checkpoint: %w", err)
	}
	body, err := RenderCheckpointIndex(plan.Locator, next)
	if err != nil {
		return PromotionConsumptionCommit{}, err
	}
	return PromotionConsumptionCommit{
		Record: record, RecordBody: renderBaseIntegration(record), RecordReference: reference,
		Checkpoint: next, CheckpointBody: body,
	}, nil
}

// FindBaseIntegrationRetry searches one bounded newest-comment acquisition for
// an orphaned append with the same semantic promotion identity.
func FindBaseIntegrationRetry(comments []StoredComment, retryID string) (*BaseIntegrationCommentMatch, error) {
	if len(comments) > MaxRetryComments {
		return nil, fmt.Errorf("base-integration retry lookup acquired %d comments; cap is %d", len(comments), MaxRetryComments)
	}
	retryID = strings.TrimSpace(retryID)
	if retryID == "" {
		return nil, errors.New("base-integration retry identity is empty")
	}
	var match *BaseIntegrationCommentMatch
	for _, comment := range comments {
		if !strings.Contains(comment.Body, "<!-- "+RecordMarkerV1+" ") {
			continue
		}
		if err := validateTrustedComment(comment); err != nil {
			continue
		}
		parsed, err := parseRecord(comment)
		if err != nil {
			return nil, fmt.Errorf("delivery retry comment %d: %w", comment.Comment.DatabaseID, err)
		}
		record, ok := parsed.value.(BaseIntegrationRecord)
		if !ok || record.RetryID != retryID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("base-integration retry identity %q is ambiguous", retryID)
		}
		value := BaseIntegrationCommentMatch{Comment: comment.Comment, Record: record}
		match = &value
	}
	return match, nil
}

func AdoptBaseIntegrationRetry(plan PromotionConsumptionPlan, current DeliveryCheckpoint, record BaseIntegrationRecord) (PromotionConsumptionPlan, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || record.RetryID != plan.Record.RetryID {
		return PromotionConsumptionPlan{}, errors.New("base-integration retry does not match the consumption plan")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return PromotionConsumptionPlan{}, fmt.Errorf("base-integration retry current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.PriorCursorDigest != current.Cursor.Digest {
		return PromotionConsumptionPlan{}, errors.New("base-integration retry is stale")
	}
	if err := validateBaseIntegration(record); err != nil {
		return PromotionConsumptionPlan{}, err
	}
	cursor := plan.Checkpoint.Checkpoint.Cursor
	if record.CommittedCursorDigest != cursor.Digest {
		return PromotionConsumptionPlan{}, errors.New("base-integration retry commits a different cursor")
	}
	next := promotionConsumptionCheckpointTemplate(current, record, cursor, plan.Checkpoint.Checkpoint.CursorBoundary)
	plan.Record = &record
	plan.Checkpoint = &CheckpointAppendTemplate{
		Checkpoint:    next,
		PendingRecord: PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID},
	}
	return plan, nil
}

type BootstrapSourceFact struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Source         string            `json:"source"`
	ObservedDigest string            `json:"observedDigest"`
	Details        map[string]string `json:"details"`
}

type BootstrapRelationship struct {
	Kind            string   `json:"kind"`
	FromFactID      string   `json:"fromFactId"`
	ToFactID        string   `json:"toFactId"`
	EvidenceFactIDs []string `json:"evidenceFactIds"`
}

type BootstrapAmbiguity struct {
	Code                    string   `json:"code"`
	Message                 string   `json:"message"`
	FactIDs                 []string `json:"factIds"`
	RequiresGenesisBoundary bool     `json:"requiresGenesisBoundary"`
	ResolvedBy              string   `json:"resolvedBy,omitempty"`
}

type BootstrapGenesisBoundary struct {
	Explicit      bool     `json:"explicit"`
	StagingSHA    string   `json:"stagingSha"`
	BaseSHA       string   `json:"baseSha,omitempty"`
	SourceFactIDs []string `json:"sourceFactIds"`
	Rationale     string   `json:"rationale"`
}

type BootstrapOwnershipRecord struct {
	Issue         ResourceIdentity `json:"issue"`
	Digest        string           `json:"digest"`
	Body          string           `json:"body"`
	SourceFactIDs []string         `json:"sourceFactIds"`
}

type BootstrapStagedWork struct {
	Order         uint64                `json:"order"`
	SourceFactIDs []string              `json:"sourceFactIds"`
	Input         StagedWorkAppendInput `json:"input"`
}

type PlannedBootstrapStagedWork struct {
	Order           uint64           `json:"order"`
	SourceFactIDs   []string         `json:"sourceFactIds"`
	Record          StagedWorkRecord `json:"record"`
	ExistingComment *CommentIdentity `json:"existingComment,omitempty"`
}

type BootstrapPlanInput struct {
	Genesis           GenesisCheckpointInput
	SourceFacts       []BootstrapSourceFact
	Relationships     []BootstrapRelationship
	Ambiguities       []BootstrapAmbiguity
	GenesisBoundary   *BootstrapGenesisBoundary
	OwnershipRecords  []BootstrapOwnershipRecord
	StagedWork        []BootstrapStagedWork
	RetryComments     []StoredComment
	ShadowComparisons []BootstrapShadowComparison
}

type BootstrapShadowComparison struct {
	PullRequest ResourceIdentity          `json:"pullRequest"`
	BaseSHA     string                    `json:"baseSha"`
	HeadSHA     string                    `json:"headSha"`
	Comparison  PromotionShadowComparison `json:"comparison"`
}

type BootstrapPlan struct {
	SchemaVersion     int                          `json:"schemaVersion"`
	Kind              string                       `json:"kind"`
	PlanID            string                       `json:"planId"`
	Applicable        bool                         `json:"applicable"`
	Repository        RepositoryIdentity           `json:"repository"`
	LedgerID          string                       `json:"ledgerId"`
	Issue             ResourceIdentity             `json:"issue"`
	SourceFacts       []BootstrapSourceFact        `json:"sourceFacts"`
	Relationships     []BootstrapRelationship      `json:"relationships"`
	Ambiguities       []BootstrapAmbiguity         `json:"ambiguities"`
	GenesisBoundary   *BootstrapGenesisBoundary    `json:"genesisBoundary,omitempty"`
	GenesisCheckpoint DeliveryCheckpoint           `json:"genesisCheckpoint"`
	OwnershipRecords  []BootstrapOwnershipRecord   `json:"ownershipRecords"`
	StagedWork        []PlannedBootstrapStagedWork `json:"stagedWork"`
	ShadowComparisons []BootstrapShadowComparison  `json:"shadowComparisons"`
}

// PlanBootstrap builds a deterministic, transport-free bootstrap manifest.
// Ambiguities remain visible in the output; those requiring a genesis choice
// are resolved only by an explicit boundary, and every unresolved ambiguity
// makes the plan inapplicable.
func PlanBootstrap(input BootstrapPlanInput) (BootstrapPlan, error) {
	facts := cloneAndSortBootstrapFacts(input.SourceFacts)
	factIDs, err := validateBootstrapFacts(facts)
	if err != nil {
		return BootstrapPlan{}, err
	}
	relationships := cloneAndSortBootstrapRelationships(input.Relationships)
	if err := validateBootstrapRelationships(relationships, factIDs); err != nil {
		return BootstrapPlan{}, err
	}
	ambiguities := cloneAndSortBootstrapAmbiguities(input.Ambiguities)
	boundary := cloneBootstrapBoundary(input.GenesisBoundary)
	if boundary != nil {
		if err := validateBootstrapBoundary(*boundary, factIDs); err != nil {
			return BootstrapPlan{}, err
		}
		input.Genesis.StagingSHA = boundary.StagingSHA
		input.Genesis.BaseSHA = boundary.BaseSHA
	}
	applicable := true
	ambiguityCodes := map[string]struct{}{}
	for index := range ambiguities {
		if strings.TrimSpace(ambiguities[index].Code) == "" || strings.TrimSpace(ambiguities[index].Message) == "" {
			return BootstrapPlan{}, errors.New("bootstrap ambiguity is incomplete")
		}
		if _, duplicate := ambiguityCodes[ambiguities[index].Code]; duplicate {
			return BootstrapPlan{}, fmt.Errorf("duplicate bootstrap ambiguity %q", ambiguities[index].Code)
		}
		ambiguityCodes[ambiguities[index].Code] = struct{}{}
		if err := validateFactIDs(ambiguities[index].FactIDs, factIDs, "bootstrap ambiguity"); err != nil {
			return BootstrapPlan{}, err
		}
		if ambiguities[index].RequiresGenesisBoundary && boundary != nil && boundary.Explicit {
			ambiguities[index].ResolvedBy = "explicitGenesisBoundary"
		}
		if ambiguities[index].ResolvedBy == "" {
			applicable = false
		}
	}
	genesis, err := NewGenesisCheckpoint(input.Genesis)
	if err != nil {
		return BootstrapPlan{}, err
	}
	ownership := make([]BootstrapOwnershipRecord, len(input.OwnershipRecords))
	copy(ownership, input.OwnershipRecords)
	sort.Slice(ownership, func(i, j int) bool { return ownership[i].Issue.Number < ownership[j].Issue.Number })
	ownershipIssues := map[int]struct{}{}
	for index := range ownership {
		ownership[index].SourceFactIDs = cloneStrings(ownership[index].SourceFactIDs)
		sort.Strings(ownership[index].SourceFactIDs)
		if err := validateResource(ownership[index].Issue, "bootstrap managed issue"); err != nil {
			return BootstrapPlan{}, err
		}
		if !validDigest(ownership[index].Digest) || strings.TrimSpace(ownership[index].Body) == "" {
			return BootstrapPlan{}, fmt.Errorf("bootstrap ownership record for issue #%d is incomplete", ownership[index].Issue.Number)
		}
		if _, duplicate := ownershipIssues[ownership[index].Issue.Number]; duplicate {
			return BootstrapPlan{}, fmt.Errorf("duplicate bootstrap ownership record for issue #%d", ownership[index].Issue.Number)
		}
		ownershipIssues[ownership[index].Issue.Number] = struct{}{}
		if err := validateFactIDs(ownership[index].SourceFactIDs, factIDs, "bootstrap ownership record"); err != nil {
			return BootstrapPlan{}, err
		}
	}
	planned, err := planBootstrapStagedWork(genesis, input.StagedWork, input.RetryComments, factIDs)
	if err != nil {
		return BootstrapPlan{}, err
	}
	plan := BootstrapPlan{
		SchemaVersion: bootstrapPlanVersion, Kind: "deliveryBootstrapPlan", Applicable: applicable,
		Repository: genesis.Repository, LedgerID: genesis.LedgerID, Issue: genesis.Issue,
		SourceFacts: facts, Relationships: relationships, Ambiguities: ambiguities,
		GenesisBoundary: boundary, GenesisCheckpoint: genesis,
		OwnershipRecords: ownership, StagedWork: planned,
		ShadowComparisons: cloneBootstrapShadowComparisons(input.ShadowComparisons),
	}
	plan.PlanID = bootstrapPlanDigest(plan)
	return plan, nil
}

func cloneBootstrapShadowComparisons(values []BootstrapShadowComparison) []BootstrapShadowComparison {
	result := append([]BootstrapShadowComparison(nil), values...)
	for index := range result {
		result[index].Comparison.Ledger = normalizePromotionProjection(result[index].Comparison.Ledger)
		result[index].Comparison.Legacy = normalizePromotionProjection(result[index].Comparison.Legacy)
		result[index].Comparison.Mismatches = append([]PromotionShadowMismatch(nil), result[index].Comparison.Mismatches...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PullRequest.Number < result[j].PullRequest.Number })
	return result
}

func planBootstrapStagedWork(genesis DeliveryCheckpoint, values []BootstrapStagedWork, retryComments []StoredComment, factIDs map[string]struct{}) ([]PlannedBootstrapStagedWork, error) {
	if len(values) > MaxOperationalRecords {
		return nil, fmt.Errorf("bootstrap staged-work cap exceeded: %d > %d with synchronization and promotion-seal slots reserved", len(values), MaxOperationalRecords)
	}
	cloned := make([]BootstrapStagedWork, len(values))
	copy(cloned, values)
	values = cloned
	sort.Slice(values, func(i, j int) bool { return values[i].Order < values[j].Order })
	result := make([]PlannedBootstrapStagedWork, 0, len(values))
	sequence, previous := genesis.HeadSequence, genesis.HeadDigest
	for index, value := range values {
		if value.Order != uint64(index+1) {
			return nil, errors.New("bootstrap staged-work order must be contiguous from one")
		}
		value.SourceFactIDs = cloneStrings(value.SourceFactIDs)
		sort.Strings(value.SourceFactIDs)
		if err := validateFactIDs(value.SourceFactIDs, factIDs, "bootstrap staged-work record"); err != nil {
			return nil, err
		}
		record := finalizeStagedWork(StagedWorkRecord{
			RecordHeader: RecordHeader{
				LedgerID: genesis.LedgerID, Repository: genesis.Repository,
				Sequence: sequence + 1, PreviousDigest: previous, Writer: value.Input.Writer,
			},
			PullRequest: value.Input.PullRequest, StagingBranch: strings.TrimSpace(value.Input.StagingBranch),
			BaseSHA: value.Input.BaseSHA, HeadSHA: value.Input.HeadSHA,
			MergeRevision: value.Input.MergeRevision, MergedAt: value.Input.MergedAt,
			ObservedCursorDigest: genesis.Cursor.Digest,
			Issues:               append([]ManagedIssueReference(nil), value.Input.Issues...),
		})
		if err := validateStagedWork(record); err != nil {
			return nil, fmt.Errorf("bootstrap staged-work order %d: %w", value.Order, err)
		}
		var existingComment *CommentIdentity
		match, err := FindStagedWorkRetry(retryComments, record.RetryID)
		if err != nil {
			return nil, fmt.Errorf("bootstrap staged-work order %d retry: %w", value.Order, err)
		}
		if match != nil {
			if match.Record.LedgerID != genesis.LedgerID || match.Record.Repository != genesis.Repository || match.Record.Sequence != sequence+1 || match.Record.PreviousDigest != previous || match.Record.ObservedCursorDigest != genesis.Cursor.Digest {
				return nil, fmt.Errorf("bootstrap staged-work order %d retry does not extend the reviewed chain", value.Order)
			}
			record = match.Record
			identity := match.Comment
			existingComment = &identity
		}
		result = append(result, PlannedBootstrapStagedWork{Order: value.Order, SourceFactIDs: value.SourceFactIDs, Record: record, ExistingComment: existingComment})
		sequence, previous = record.Sequence, record.Digest
	}
	return result, nil
}

func cloneAndSortBootstrapFacts(values []BootstrapSourceFact) []BootstrapSourceFact {
	result := make([]BootstrapSourceFact, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Details = make(map[string]string, len(value.Details))
		for key, detail := range value.Details {
			result[index].Details[key] = detail
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func validateBootstrapFacts(facts []BootstrapSourceFact) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		if strings.TrimSpace(fact.ID) == "" || strings.TrimSpace(fact.Kind) == "" || strings.TrimSpace(fact.Source) == "" || !validDigest(fact.ObservedDigest) {
			return nil, errors.New("bootstrap source fact is incomplete")
		}
		if _, duplicate := ids[fact.ID]; duplicate {
			return nil, fmt.Errorf("duplicate bootstrap source fact %q", fact.ID)
		}
		ids[fact.ID] = struct{}{}
		for key := range fact.Details {
			if strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("bootstrap source fact %q has an empty detail name", fact.ID)
			}
		}
	}
	return ids, nil
}

func cloneAndSortBootstrapRelationships(values []BootstrapRelationship) []BootstrapRelationship {
	result := make([]BootstrapRelationship, len(values))
	copy(result, values)
	for index := range result {
		result[index].EvidenceFactIDs = cloneStrings(result[index].EvidenceFactIDs)
		sort.Strings(result[index].EvidenceFactIDs)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return left.FromFactID+"\x00"+left.ToFactID+"\x00"+left.Kind < right.FromFactID+"\x00"+right.ToFactID+"\x00"+right.Kind
	})
	return result
}

func validateBootstrapRelationships(values []BootstrapRelationship, factIDs map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, relationship := range values {
		if strings.TrimSpace(relationship.Kind) == "" {
			return errors.New("bootstrap relationship kind is empty")
		}
		if err := validateFactIDs([]string{relationship.FromFactID, relationship.ToFactID}, factIDs, "bootstrap relationship endpoints"); err != nil {
			return err
		}
		if err := validateFactIDs(relationship.EvidenceFactIDs, factIDs, "bootstrap relationship evidence"); err != nil {
			return err
		}
		key := relationship.FromFactID + "\x00" + relationship.ToFactID + "\x00" + relationship.Kind
		if _, duplicate := seen[key]; duplicate {
			return errors.New("duplicate bootstrap relationship")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func cloneAndSortBootstrapAmbiguities(values []BootstrapAmbiguity) []BootstrapAmbiguity {
	result := make([]BootstrapAmbiguity, len(values))
	copy(result, values)
	for index := range result {
		result[index].FactIDs = cloneStrings(result[index].FactIDs)
		sort.Strings(result[index].FactIDs)
		result[index].ResolvedBy = ""
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}

func cloneBootstrapBoundary(value *BootstrapGenesisBoundary) *BootstrapGenesisBoundary {
	if value == nil {
		return nil
	}
	result := *value
	result.SourceFactIDs = cloneStrings(value.SourceFactIDs)
	sort.Strings(result.SourceFactIDs)
	return &result
}

func cloneStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func validateBootstrapBoundary(boundary BootstrapGenesisBoundary, factIDs map[string]struct{}) error {
	if !validSHA(boundary.StagingSHA) || strings.TrimSpace(boundary.Rationale) == "" {
		return errors.New("bootstrap genesis boundary is incomplete")
	}
	if boundary.BaseSHA != "" && !validSHA(boundary.BaseSHA) {
		return errors.New("bootstrap genesis base revision is invalid")
	}
	return validateFactIDs(boundary.SourceFactIDs, factIDs, "bootstrap genesis boundary")
}

func validateFactIDs(values []string, available map[string]struct{}, name string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, exists := available[value]; !exists {
			return fmt.Errorf("%s references unknown source fact %q", name, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s repeats source fact %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func bootstrapPlanDigest(plan BootstrapPlan) string {
	plan.PlanID = ""
	return canonicalDigest(plan)
}
