package delivery

import (
	"errors"
	"fmt"
	"strings"
)

type BaseIntegrationState string

const (
	BaseIntegrated          BaseIntegrationState = "integrated"
	BaseDirectWorkPending   BaseIntegrationState = "direct-base-work-pending"
	BaseIntegrationDiverged BaseIntegrationState = "diverged"
	BaseIntegrationUnknown  BaseIntegrationState = "unknown"
)

type RevisionRelation string

const (
	RevisionIdentical RevisionRelation = "identical"
	RevisionAhead     RevisionRelation = "ahead"
	RevisionBehind    RevisionRelation = "behind"
	RevisionDiverged  RevisionRelation = "diverged"
	RevisionUnknown   RevisionRelation = "unknown"
)

// BaseIntegrationObservation binds one classification to immutable branch and
// ledger revisions. Relations are always from the recorded revision to the
// observed revision; raw base-to-staging ancestry is intentionally absent.
type BaseIntegrationObservation struct {
	BaseSHA         string           `json:"baseSha"`
	StagingSHA      string           `json:"stagingSha"`
	BaseRelation    RevisionRelation `json:"baseRelation"`
	StagingRelation RevisionRelation `json:"stagingRelation"`
}

type BaseIntegrationFacts struct {
	State                   BaseIntegrationState `json:"state"`
	ObservedBaseSHA         string               `json:"observedBaseSha"`
	ObservedStagingSHA      string               `json:"observedStagingSha"`
	IntegrationRecordDigest string               `json:"integrationRecordDigest,omitempty"`
	IntegrationSource       IntegrationSource    `json:"integrationSource,omitempty"`
	IntegratedBaseSHA       string               `json:"integratedBaseSha,omitempty"`
	IntegratedStagingSHA    string               `json:"integratedStagingSha,omitempty"`
	Reason                  string               `json:"reason"`
}

func RelationFromComparison(status string) RevisionRelation {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "identical":
		return RevisionIdentical
	case "ahead":
		return RevisionAhead
	case "behind":
		return RevisionBehind
	case "diverged":
		return RevisionDiverged
	default:
		return RevisionUnknown
	}
}

func ClassifyBaseIntegration(snapshot Snapshot, observation BaseIntegrationObservation) (BaseIntegrationFacts, error) {
	facts := BaseIntegrationFacts{
		State: BaseIntegrationUnknown, ObservedBaseSHA: strings.TrimSpace(observation.BaseSHA),
		ObservedStagingSHA: strings.TrimSpace(observation.StagingSHA), Reason: "integration evidence is incomplete",
	}
	if !validSHA(facts.ObservedBaseSHA) || !validSHA(facts.ObservedStagingSHA) {
		return facts, nil
	}
	if snapshot.Checkpoint.BaseIntegration == nil {
		if validSHA(snapshot.Checkpoint.GenesisBaseSHA) {
			facts.IntegrationRecordDigest = snapshot.Checkpoint.WindowDigest
			facts.IntegratedBaseSHA = snapshot.Checkpoint.GenesisBaseSHA
			facts.IntegratedStagingSHA = snapshot.Checkpoint.Coverage.StagingSHA
			stagingRelation := observation.StagingRelation
			if facts.ObservedStagingSHA == snapshot.Checkpoint.Coverage.StagingSHA {
				stagingRelation = RevisionIdentical
			}
			switch stagingRelation {
			case RevisionBehind, RevisionDiverged:
				facts.State, facts.Reason = BaseIntegrationDiverged, "observed staging no longer contains the reviewed genesis boundary"
				return facts, nil
			case RevisionUnknown:
				facts.Reason = "staging relationship to the reviewed genesis boundary is unknown"
				return facts, nil
			}
			if facts.ObservedBaseSHA == snapshot.Checkpoint.GenesisBaseSHA {
				facts.State, facts.Reason = BaseIntegrated, "observed revisions descend from the reviewed genesis boundary"
				return facts, nil
			}
			switch observation.BaseRelation {
			case RevisionAhead:
				facts.State, facts.Reason = BaseDirectWorkPending, "base advanced beyond the reviewed genesis boundary"
			case RevisionBehind, RevisionDiverged:
				facts.State, facts.Reason = BaseIntegrationDiverged, "observed base does not descend from the reviewed genesis boundary"
			default:
				facts.Reason = "base relationship to the reviewed genesis boundary is unknown"
			}
			return facts, nil
		}
		facts.Reason = "delivery checkpoint has no acknowledged base integration"
		return facts, nil
	}
	record, found := currentBaseIntegration(snapshot)
	if !found {
		return facts, errors.New("checkpoint base-integration reference has no acquired record")
	}
	facts.IntegrationRecordDigest = record.Digest
	facts.IntegrationSource = record.Source
	facts.IntegratedBaseSHA = record.BaseSHA
	facts.IntegratedStagingSHA = record.StagingSHA
	stagingRelation := observation.StagingRelation
	if facts.ObservedStagingSHA == record.StagingSHA {
		stagingRelation = RevisionIdentical
	}
	switch stagingRelation {
	case RevisionBehind, RevisionDiverged:
		facts.State, facts.Reason = BaseIntegrationDiverged, "observed staging no longer contains the recorded integration boundary"
		return facts, nil
	case RevisionUnknown:
		facts.Reason = "staging relationship to the recorded integration boundary is unknown"
		return facts, nil
	}

	if facts.ObservedBaseSHA == record.BaseSHA {
		if record.Source == IntegrationPromotion {
			facts.State, facts.Reason = BaseIntegrated, "observed base is the recorded promotion result"
			return facts, nil
		}
		facts.State, facts.Reason = BaseIntegrated, "observed staging contains the recorded synchronization result"
		return facts, nil
	}

	switch observation.BaseRelation {
	case RevisionAhead:
		facts.State, facts.Reason = BaseDirectWorkPending, "base advanced beyond the acknowledged integration revision"
	case RevisionBehind, RevisionDiverged:
		facts.State, facts.Reason = BaseIntegrationDiverged, "observed base does not descend from the acknowledged integration revision"
	default:
		facts.Reason = "base relationship to the acknowledged integration revision is unknown"
	}
	return facts, nil
}

func currentBaseIntegration(snapshot Snapshot) (BaseIntegrationRecord, bool) {
	if snapshot.Checkpoint.BaseIntegration == nil {
		return BaseIntegrationRecord{}, false
	}
	for _, record := range snapshot.BaseIntegrations {
		if record.Digest == snapshot.Checkpoint.BaseIntegration.Digest {
			return record, true
		}
	}
	return BaseIntegrationRecord{}, false
}

type SynchronizationInput struct {
	PullRequest     ResourceIdentity `json:"pullRequest"`
	PriorStagingSHA string           `json:"priorStagingSha"`
	BaseSHA         string           `json:"baseSha"`
	StagingSHA      string           `json:"stagingSha"`
	Writer          WriterProvenance `json:"writer"`
	RecordedAt      string           `json:"recordedAt"`
}

type SynchronizationPlan struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Kind          string                     `json:"kind"`
	Applicable    bool                       `json:"applicable"`
	Locator       DeliveryStoreLocator       `json:"locator"`
	Precondition  CheckpointPrecondition     `json:"precondition"`
	Record        *BaseIntegrationRecord     `json:"record,omitempty"`
	Checkpoint    *CheckpointAppendTemplate  `json:"checkpoint,omitempty"`
	Existing      *BaseIntegrationRetryMatch `json:"existing,omitempty"`
}

type SynchronizationCommit = PromotionConsumptionCommit

func PlanSynchronization(snapshot Snapshot, input SynchronizationInput) (SynchronizationPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return SynchronizationPlan{}, fmt.Errorf("synchronization checkpoint: %w", err)
	}
	if err := validateResource(input.PullRequest, "synchronization pull request"); err != nil {
		return SynchronizationPlan{}, err
	}
	if !validSHA(input.PriorStagingSHA) || !validSHA(input.BaseSHA) || !validSHA(input.StagingSHA) {
		return SynchronizationPlan{}, errors.New("synchronization revisions are incomplete")
	}
	if strings.TrimSpace(input.Writer.Workflow) == "" || input.Writer.RunID <= 0 {
		return SynchronizationPlan{}, errors.New("synchronization writer provenance is incomplete")
	}
	if err := validateTimestamp(input.RecordedAt); err != nil {
		return SynchronizationPlan{}, fmt.Errorf("synchronization record time: %w", err)
	}
	if current, found := currentBaseIntegration(snapshot); found && current.Source == IntegrationPromotion && current.StagingSHA == input.StagingSHA && current.BaseSHA != input.BaseSHA {
		return SynchronizationPlan{}, errors.New("synchronization event predates the current promotion integration")
	}
	plan := SynchronizationPlan{SchemaVersion: SchemaVersion, Kind: "synchronizationPlan", Applicable: true, Locator: snapshot.Locator, Precondition: checkpointPrecondition(checkpoint)}
	record := finalizeBaseIntegration(BaseIntegrationRecord{
		RecordHeader: RecordHeader{LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository, Sequence: checkpoint.HeadSequence + 1, PreviousDigest: checkpoint.HeadDigest, Writer: input.Writer},
		Source:       IntegrationSynchronization, Method: PromotionSync, PullRequest: input.PullRequest,
		PriorStagingSHA: input.PriorStagingSHA, BaseSHA: input.BaseSHA, StagingSHA: input.StagingSHA,
		PriorCursorDigest: checkpoint.Cursor.Digest, CommittedCursorDigest: checkpoint.Cursor.Digest, RecordedAt: input.RecordedAt,
	})
	if err := validateBaseIntegration(record); err != nil {
		return SynchronizationPlan{}, err
	}
	if existing, err := committedSynchronization(snapshot, input); err != nil {
		return SynchronizationPlan{}, err
	} else if existing != nil {
		plan.Applicable = false
		plan.Existing = existing
		return plan, nil
	}
	if len(checkpoint.ActiveRecords) >= MaxActiveRecords {
		return SynchronizationPlan{}, fmt.Errorf("delivery checkpoint active-record cap reached: %d", MaxActiveRecords)
	}
	next := synchronizationCheckpointTemplate(checkpoint, record)
	plan.Record = &record
	plan.Checkpoint = &CheckpointAppendTemplate{Checkpoint: next, PendingRecord: PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}}
	return plan, nil
}

func committedSynchronization(snapshot Snapshot, input SynchronizationInput) (*BaseIntegrationRetryMatch, error) {
	var match *BaseIntegrationRetryMatch
	for _, record := range snapshot.BaseIntegrations {
		if record.Source != IntegrationSynchronization || record.PullRequest != input.PullRequest || record.PriorStagingSHA != input.PriorStagingSHA || record.BaseSHA != input.BaseSHA || record.StagingSHA != input.StagingSHA {
			continue
		}
		reference, found := checkpointRecordReference(snapshot.Checkpoint, record.Digest)
		if !found || snapshot.Checkpoint.BaseIntegration == nil || *snapshot.Checkpoint.BaseIntegration != reference || record.CommittedCursorDigest != snapshot.Checkpoint.Cursor.Digest {
			return nil, errors.New("matching synchronization is not durably committed")
		}
		if match != nil {
			return nil, errors.New("synchronization event has multiple committed base integrations")
		}
		value := BaseIntegrationRetryMatch{Record: record, Reference: reference}
		match = &value
	}
	return match, nil
}

func synchronizationCheckpointTemplate(checkpoint DeliveryCheckpoint, record BaseIntegrationRecord) DeliveryCheckpoint {
	next := checkpoint
	if next.PromotionIntegration == nil && next.BaseIntegration != nil && next.Cursor.Position > 0 {
		next.PromotionIntegration = next.BaseIntegration
	}
	next.Generation++
	next.HeadSequence, next.HeadDigest = record.Sequence, record.Digest
	next.ActiveRecords = append([]RecordReference(nil), checkpoint.ActiveRecords...)
	next.ActivePlan = nil
	next.BaseIntegration = nil
	next.Coverage = finalizeCoverage(StagingCoverage{
		Repository: checkpoint.Repository, StagingSHA: record.StagingSHA, RecordSequence: record.Sequence,
		RecordDigest: record.Digest, CursorDigest: checkpoint.Cursor.Digest, Writer: record.Writer, ObservedAt: record.RecordedAt,
	})
	next.Digest = ""
	return next
}

func FinalizeSynchronization(plan SynchronizationPlan, current DeliveryCheckpoint, comment CommentIdentity) (SynchronizationCommit, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil {
		return SynchronizationCommit{}, errors.New("synchronization plan has no pending append")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return SynchronizationCommit{}, fmt.Errorf("synchronization current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition {
		return SynchronizationCommit{}, errors.New("synchronization plan is stale")
	}
	if err := validateCommentIdentity(comment); err != nil {
		return SynchronizationCommit{}, fmt.Errorf("synchronization comment: %w", err)
	}
	for _, reference := range referencedComments(current) {
		if reference.Comment == comment || reference.Comment.DatabaseID == comment.DatabaseID || reference.Comment.NodeID == comment.NodeID {
			return SynchronizationCommit{}, errors.New("synchronization comment identity is already committed")
		}
	}
	record := *plan.Record
	if err := validateBaseIntegration(record); err != nil {
		return SynchronizationCommit{}, err
	}
	if record.LedgerID != current.LedgerID || record.Repository != current.Repository || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.PriorCursorDigest != current.Cursor.Digest || record.CommittedCursorDigest != current.Cursor.Digest {
		return SynchronizationCommit{}, errors.New("synchronization record is not bound to the current checkpoint")
	}
	pending := plan.Checkpoint.PendingRecord
	if pending != (PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}) {
		return SynchronizationCommit{}, errors.New("synchronization template does not match its record")
	}
	expected := synchronizationCheckpointTemplate(current, record)
	if canonicalDigest(plan.Checkpoint.Checkpoint) != canonicalDigest(expected) {
		return SynchronizationCommit{}, errors.New("synchronization checkpoint template is not bound to the current checkpoint")
	}
	reference := recordReference(comment, record.RecordHeader)
	next := expected
	next.ActiveRecords = append(next.ActiveRecords, reference)
	next.BaseIntegration = &reference
	next = finalizeCheckpoint(next)
	if err := validateCheckpoint(next, plan.Locator); err != nil {
		return SynchronizationCommit{}, fmt.Errorf("synchronization checkpoint: %w", err)
	}
	body, err := RenderCheckpointIndex(plan.Locator, next)
	if err != nil {
		return SynchronizationCommit{}, err
	}
	return SynchronizationCommit{Record: record, RecordBody: renderBaseIntegration(record), RecordReference: reference, Checkpoint: next, CheckpointBody: body}, nil
}

func AdoptSynchronizationRetry(plan SynchronizationPlan, current DeliveryCheckpoint, record BaseIntegrationRecord) (SynchronizationPlan, error) {
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || record.RetryID != plan.Record.RetryID || record.Source != IntegrationSynchronization {
		return SynchronizationPlan{}, errors.New("base-integration retry does not match the synchronization plan")
	}
	if err := validateCheckpoint(current, plan.Locator); err != nil {
		return SynchronizationPlan{}, fmt.Errorf("synchronization retry current checkpoint: %w", err)
	}
	if checkpointPrecondition(current) != plan.Precondition || record.Sequence != current.HeadSequence+1 || record.PreviousDigest != current.HeadDigest || record.PriorCursorDigest != current.Cursor.Digest || record.CommittedCursorDigest != current.Cursor.Digest {
		return SynchronizationPlan{}, errors.New("synchronization retry is stale")
	}
	if err := validateBaseIntegration(record); err != nil {
		return SynchronizationPlan{}, err
	}
	next := synchronizationCheckpointTemplate(current, record)
	plan.Record = &record
	plan.Checkpoint = &CheckpointAppendTemplate{Checkpoint: next, PendingRecord: PendingRecordReference{Kind: record.Kind, Sequence: record.Sequence, Digest: record.Digest, RetryID: record.RetryID}}
	return plan, nil
}
