package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
)

func renderCheckpoint(checkpoint DeliveryCheckpoint) string {
	return renderMarker(CheckpointMarkerV1, checkpoint)
}

// ParseCheckpointIndex parses and validates the pinned, trusted checkpoint
// comment without acquiring any of the immutable records it references.
func ParseCheckpointIndex(locator DeliveryStoreLocator, stored StoredComment) (DeliveryCheckpoint, error) {
	if err := validateLocator(locator); err != nil {
		return DeliveryCheckpoint{}, err
	}
	if stored.Comment != locator.Checkpoint {
		return DeliveryCheckpoint{}, errors.New("delivery checkpoint identity does not match the pinned locator")
	}
	if err := validateTrustedComment(stored); err != nil {
		return DeliveryCheckpoint{}, fmt.Errorf("delivery checkpoint: %w", err)
	}
	payload, err := extractMarker(stored.Body, CheckpointMarkerV1)
	if err != nil {
		return DeliveryCheckpoint{}, fmt.Errorf("delivery checkpoint: %w", err)
	}
	var checkpoint DeliveryCheckpoint
	if err := strictUnmarshal(payload, &checkpoint); err != nil {
		return DeliveryCheckpoint{}, fmt.Errorf("delivery checkpoint is unreadable: %w", err)
	}
	if err := validateCheckpoint(checkpoint, locator); err != nil {
		return DeliveryCheckpoint{}, err
	}
	return checkpoint, nil
}

// RenderCheckpointIndex validates a checkpoint against its pinned locator
// before returning the canonical marker body used for the mutable index.
func RenderCheckpointIndex(locator DeliveryStoreLocator, checkpoint DeliveryCheckpoint) (string, error) {
	if err := validateLocator(locator); err != nil {
		return "", err
	}
	if err := validateCheckpoint(checkpoint, locator); err != nil {
		return "", err
	}
	return renderCheckpoint(checkpoint), nil
}

func renderStagedWork(record StagedWorkRecord) string {
	return renderMarker(RecordMarkerV1, record)
}

// RenderStagedWorkRecord validates and encodes an immutable staged-work
// comment. Writers should use this instead of constructing ledger markers.
func RenderStagedWorkRecord(record StagedWorkRecord) (string, error) {
	if err := validateStagedWork(record); err != nil {
		return "", err
	}
	return renderStagedWork(record), nil
}

// ParseStagedWorkComment parses one trusted immutable staged-work comment.
// It is intentionally independent of checkpoint membership for bounded
// ambiguous-create recovery; callers must still commit the returned comment
// through a checkpoint before it becomes authoritative.
func ParseStagedWorkComment(stored StoredComment) (StagedWorkRecord, error) {
	if err := validateTrustedComment(stored); err != nil {
		return StagedWorkRecord{}, fmt.Errorf("staged-work comment: %w", err)
	}
	parsed, err := parseRecord(stored)
	if err != nil {
		return StagedWorkRecord{}, fmt.Errorf("staged-work comment: %w", err)
	}
	record, ok := parsed.value.(StagedWorkRecord)
	if !ok {
		return StagedWorkRecord{}, errors.New("delivery comment is not a staged-work record")
	}
	return record, nil
}

func renderPromotionPlan(record PromotionPlanRecord) string {
	return renderMarker(RecordMarkerV1, record)
}

func RenderPromotionPlanRecord(record PromotionPlanRecord) (string, error) {
	if err := validatePromotionPlan(record); err != nil {
		return "", err
	}
	return renderPromotionPlan(record), nil
}

func ParsePromotionPlanComment(stored StoredComment) (PromotionPlanRecord, error) {
	if err := validateTrustedComment(stored); err != nil {
		return PromotionPlanRecord{}, err
	}
	parsed, err := parseRecord(stored)
	if err != nil {
		return PromotionPlanRecord{}, err
	}
	record, ok := parsed.value.(PromotionPlanRecord)
	if !ok {
		return PromotionPlanRecord{}, errors.New("delivery comment is not a promotion-plan record")
	}
	return record, nil
}

func renderExclusion(record ExclusionRecord) string {
	return renderMarker(RecordMarkerV1, record)
}

func renderBaseIntegration(record BaseIntegrationRecord) string {
	return renderMarker(RecordMarkerV1, record)
}

func RenderBaseIntegrationRecord(record BaseIntegrationRecord) (string, error) {
	if err := validateBaseIntegration(record); err != nil {
		return "", err
	}
	return renderBaseIntegration(record), nil
}

func ParseBaseIntegrationComment(stored StoredComment) (BaseIntegrationRecord, error) {
	if err := validateTrustedComment(stored); err != nil {
		return BaseIntegrationRecord{}, err
	}
	parsed, err := parseRecord(stored)
	if err != nil {
		return BaseIntegrationRecord{}, err
	}
	record, ok := parsed.value.(BaseIntegrationRecord)
	if !ok {
		return BaseIntegrationRecord{}, errors.New("delivery comment is not a base-integration record")
	}
	return record, nil
}

func ParseStoreSnapshot(store StoreSnapshot) (Snapshot, error) {
	if !store.Complete {
		return Snapshot{}, errors.New("delivery store acquisition is incomplete")
	}
	checkpoint, err := ParseCheckpointIndex(store.Locator, store.Checkpoint)
	if err != nil {
		return Snapshot{}, err
	}

	expected := referencedComments(checkpoint)
	if len(expected) > MaxActiveRecords+3 {
		return Snapshot{}, fmt.Errorf("delivery checkpoint references %d records; cap is %d active records plus cursor boundary and integration records", len(expected), MaxActiveRecords)
	}
	parsedByID := make(map[int64]parsedRecord, len(store.Records))
	nodeIDs, sequences, retries := map[string]struct{}{}, map[uint64]struct{}{}, map[string]struct{}{}
	result := Snapshot{Locator: store.Locator, Checkpoint: checkpoint}
	for _, comment := range store.Records {
		if _, duplicate := parsedByID[comment.Comment.DatabaseID]; duplicate {
			return Snapshot{}, fmt.Errorf("duplicate delivery comment database ID %d", comment.Comment.DatabaseID)
		}
		if _, duplicate := nodeIDs[comment.Comment.NodeID]; duplicate {
			return Snapshot{}, fmt.Errorf("duplicate delivery comment node ID %q", comment.Comment.NodeID)
		}
		nodeIDs[comment.Comment.NodeID] = struct{}{}
		if err := validateTrustedComment(comment); err != nil {
			return Snapshot{}, fmt.Errorf("delivery record comment %d: %w", comment.Comment.DatabaseID, err)
		}
		parsed, err := parseRecord(comment)
		if err != nil {
			return Snapshot{}, fmt.Errorf("delivery record comment %d: %w", comment.Comment.DatabaseID, err)
		}
		if parsed.header.LedgerID != checkpoint.LedgerID || parsed.header.Repository != checkpoint.Repository {
			return Snapshot{}, fmt.Errorf("delivery record comment %d belongs to another ledger or repository", comment.Comment.DatabaseID)
		}
		if _, duplicate := sequences[parsed.header.Sequence]; duplicate {
			return Snapshot{}, fmt.Errorf("duplicate delivery record sequence %d", parsed.header.Sequence)
		}
		sequences[parsed.header.Sequence] = struct{}{}
		if _, duplicate := retries[parsed.header.RetryID]; duplicate {
			return Snapshot{}, fmt.Errorf("duplicate delivery retry identity %q", parsed.header.RetryID)
		}
		retries[parsed.header.RetryID] = struct{}{}
		parsedByID[comment.Comment.DatabaseID] = parsed
		switch value := parsed.value.(type) {
		case StagedWorkRecord:
			result.StagedWork = append(result.StagedWork, value)
		case PromotionPlanRecord:
			result.PromotionPlans = append(result.PromotionPlans, value)
		case ExclusionRecord:
			result.Exclusions = append(result.Exclusions, value)
		case BaseIntegrationRecord:
			result.BaseIntegrations = append(result.BaseIntegrations, value)
		}
	}
	if len(parsedByID) != len(expected) {
		return Snapshot{}, fmt.Errorf("delivery store returned %d records for %d exact references", len(parsedByID), len(expected))
	}
	for id, reference := range expected {
		parsed, exists := parsedByID[id]
		if !exists {
			return Snapshot{}, fmt.Errorf("delivery record comment %d is missing", id)
		}
		if err := matchReference(reference, parsed); err != nil {
			return Snapshot{}, fmt.Errorf("delivery record comment %d: %w", id, err)
		}
	}
	if err := validateActiveChain(checkpoint, parsedByID); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

func ValidateSealedPlan(snapshot Snapshot, revision PromotionRevision) error {
	_, err := ResolveSealedPromotion(snapshot, revision)
	return err
}

// ResolveStagedWork finds one exact committed active staged-work snapshot for a
// merged work event. Absence is a delayed/missing recorder failure, never a
// reason to recover issue relationships from the pull-request body.
func ResolveStagedWork(snapshot Snapshot, revision StagedWorkRevision) (ResolvedStagedWork, error) {
	if err := validateResource(revision.PullRequest, "work pull request"); err != nil {
		return ResolvedStagedWork{}, err
	}
	if !validSHA(revision.BaseSHA) || !validSHA(revision.HeadSHA) || !validSHA(revision.MergeRevision) {
		return ResolvedStagedWork{}, errors.New("staged-work revision is incomplete")
	}
	active := make(map[string]RecordReference, len(snapshot.Checkpoint.ActiveRecords))
	for _, reference := range snapshot.Checkpoint.ActiveRecords {
		if reference.Kind == RecordStagedWork {
			active[reference.Digest] = reference
		}
	}
	var result *ResolvedStagedWork
	for _, record := range snapshot.StagedWork {
		if record.PullRequest != revision.PullRequest || record.BaseSHA != revision.BaseSHA || record.HeadSHA != revision.HeadSHA || record.MergeRevision != revision.MergeRevision {
			continue
		}
		reference, committed := active[record.Digest]
		if !committed {
			continue
		}
		if reference.Sequence > snapshot.Checkpoint.Coverage.RecordSequence {
			return ResolvedStagedWork{}, errors.New("staged-work record is not covered by the committed staging watermark")
		}
		if result != nil {
			return ResolvedStagedWork{}, errors.New("staged-work revision has multiple committed delivery records")
		}
		value := ResolvedStagedWork{Record: record, Reference: reference}
		result = &value
	}
	if result == nil {
		return ResolvedStagedWork{}, errors.New("staged-work revision has no committed delivery record")
	}
	return *result, nil
}

// ResolveSealedPromotion validates the active seal and returns its durable work
// and issue projection. Callers never need to recover associations from mutable
// pull-request text.
func ResolveSealedPromotion(snapshot Snapshot, revision PromotionRevision) (ResolvedPromotion, error) {
	if snapshot.Checkpoint.ActivePlan == nil {
		return ResolvedPromotion{}, errors.New("promotion plan is not sealed")
	}
	plan, found := promotionPlanByDigest(snapshot.PromotionPlans, snapshot.Checkpoint.ActivePlan.Digest)
	if !found {
		return ResolvedPromotion{}, errors.New("sealed promotion plan record is missing")
	}
	if strings.TrimSpace(revision.RepositoryNodeID) == "" || revision.RepositoryNodeID != snapshot.Checkpoint.Repository.NodeID {
		return ResolvedPromotion{}, errors.New("sealed promotion repository identity is stale")
	}
	if plan.PullRequest != revision.PullRequest {
		return ResolvedPromotion{}, errors.New("sealed promotion pull request identity is stale")
	}
	if plan.BaseSHA != revision.BaseSHA || plan.HeadSHA != revision.HeadSHA {
		return ResolvedPromotion{}, errors.New("sealed promotion base or head revision is stale")
	}
	if plan.CursorDigest != snapshot.Checkpoint.Cursor.Digest {
		return ResolvedPromotion{}, errors.New("sealed promotion cursor is stale")
	}
	if plan.CoverageDigest != snapshot.Checkpoint.Coverage.Digest || snapshot.Checkpoint.Coverage.StagingSHA != revision.HeadSHA {
		return ResolvedPromotion{}, errors.New("sealed promotion does not cover the exact staging revision")
	}

	staged := make(map[string]StagedWorkRecord, len(snapshot.StagedWork))
	activeReferences := make(map[string]RecordReference, len(snapshot.Checkpoint.ActiveRecords))
	for _, reference := range snapshot.Checkpoint.ActiveRecords {
		activeReferences[reference.Digest] = reference
	}
	for _, record := range snapshot.StagedWork {
		if reference, active := activeReferences[record.Digest]; active && reference.Kind == RecordStagedWork {
			staged[record.Digest] = record
		}
	}
	accounted := map[string]struct{}{}
	for _, reference := range plan.Included {
		if reference.Kind != RecordStagedWork {
			return ResolvedPromotion{}, errors.New("sealed promotion includes a non-staged-work record")
		}
		if expected, exists := activeReferences[reference.Digest]; !exists || expected != reference {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion included record %q does not match its active reference", reference.Digest)
		}
		if _, exists := staged[reference.Digest]; !exists {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion included staged record %q is missing", reference.Digest)
		}
		if _, duplicate := accounted[reference.Digest]; duplicate {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion accounts for staged record %q more than once", reference.Digest)
		}
		accounted[reference.Digest] = struct{}{}
	}
	for _, reference := range plan.Exclusions {
		if reference.Kind != RecordExclusion {
			return ResolvedPromotion{}, errors.New("sealed promotion exclusion pointer has the wrong kind")
		}
		if expected, exists := activeReferences[reference.Digest]; !exists || expected != reference {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion exclusion %q does not match its active reference", reference.Digest)
		}
		exclusion, exists := exclusionByDigest(snapshot.Exclusions, reference.Digest)
		if !exists {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion exclusion %q is missing", reference.Digest)
		}
		if exclusion.PullRequest != plan.PullRequest || exclusion.HeadSHA != plan.HeadSHA || exclusion.ReviewHeadSHA != plan.HeadSHA || exclusion.CursorDigest != plan.CursorDigest {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion exclusion %q is stale", reference.Digest)
		}
		if expected, exists := activeReferences[exclusion.StagedRecord.Digest]; !exists || expected != exclusion.StagedRecord {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion exclusion %q has a mismatched staged-work reference", reference.Digest)
		}
		if _, exists := staged[exclusion.StagedRecord.Digest]; !exists {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion exclusion %q references missing staged work", reference.Digest)
		}
		if _, duplicate := accounted[exclusion.StagedRecord.Digest]; duplicate {
			return ResolvedPromotion{}, fmt.Errorf("sealed promotion accounts for staged record %q more than once", exclusion.StagedRecord.Digest)
		}
		accounted[exclusion.StagedRecord.Digest] = struct{}{}
	}
	if len(accounted) != len(staged) {
		return ResolvedPromotion{}, fmt.Errorf("sealed promotion accounts for %d of %d staged-work records", len(accounted), len(staged))
	}
	return resolvedPromotion(snapshot, plan), nil
}

func resolvedPromotion(snapshot Snapshot, plan PromotionPlanRecord) ResolvedPromotion {
	stagedByDigest := make(map[string]StagedWorkRecord, len(snapshot.StagedWork))
	for _, record := range snapshot.StagedWork {
		stagedByDigest[record.Digest] = record
	}
	excludedByStaged := make(map[string]struct{}, len(plan.Exclusions))
	for _, reference := range plan.Exclusions {
		if exclusion, found := exclusionByDigest(snapshot.Exclusions, reference.Digest); found {
			excludedByStaged[exclusion.StagedRecord.Digest] = struct{}{}
		}
	}
	work := make([]PromotionWork, 0, len(plan.Included)+len(plan.Exclusions))
	issuesByNumber := map[int]ManagedIssueReference{}
	for _, reference := range snapshot.Checkpoint.ActiveRecords {
		record, found := stagedByDigest[reference.Digest]
		if !found {
			continue
		}
		_, excluded := excludedByStaged[reference.Digest]
		work = append(work, PromotionWork{PullRequest: record.PullRequest, Record: reference, Issues: append([]ManagedIssueReference(nil), record.Issues...), Excluded: excluded})
		if excluded {
			continue
		}
		for _, issue := range record.Issues {
			issuesByNumber[issue.Number] = issue
		}
	}
	issues := make([]ManagedIssueReference, 0, len(issuesByNumber))
	for _, issue := range issuesByNumber {
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	return ResolvedPromotion{Plan: plan, PlanReference: *snapshot.Checkpoint.ActivePlan, Work: work, ExpectedIssues: issues}
}

type parsedRecord struct {
	comment CommentIdentity
	header  RecordHeader
	value   any
}

func parseRecord(comment StoredComment) (parsedRecord, error) {
	payload, err := extractMarker(comment.Body, RecordMarkerV1)
	if err != nil {
		return parsedRecord{}, err
	}
	var discriminator struct {
		Version int        `json:"version"`
		Kind    RecordKind `json:"kind"`
	}
	if err := json.Unmarshal(payload, &discriminator); err != nil {
		return parsedRecord{}, err
	}
	if discriminator.Version != SchemaVersion {
		return parsedRecord{}, fmt.Errorf("unsupported delivery record version %d", discriminator.Version)
	}
	switch discriminator.Kind {
	case RecordStagedWork:
		var record StagedWorkRecord
		if err := strictUnmarshal(payload, &record); err != nil {
			return parsedRecord{}, err
		}
		if err := validateStagedWork(record); err != nil {
			return parsedRecord{}, err
		}
		return parsedRecord{comment: comment.Comment, header: record.RecordHeader, value: record}, nil
	case RecordPromotionPlan:
		var record PromotionPlanRecord
		if err := strictUnmarshal(payload, &record); err != nil {
			return parsedRecord{}, err
		}
		if err := validatePromotionPlan(record); err != nil {
			return parsedRecord{}, err
		}
		return parsedRecord{comment: comment.Comment, header: record.RecordHeader, value: record}, nil
	case RecordExclusion:
		var record ExclusionRecord
		if err := strictUnmarshal(payload, &record); err != nil {
			return parsedRecord{}, err
		}
		if err := validateExclusion(record); err != nil {
			return parsedRecord{}, err
		}
		return parsedRecord{comment: comment.Comment, header: record.RecordHeader, value: record}, nil
	case RecordBaseIntegration:
		var record BaseIntegrationRecord
		if err := strictUnmarshal(payload, &record); err != nil {
			return parsedRecord{}, err
		}
		if err := validateBaseIntegration(record); err != nil {
			return parsedRecord{}, err
		}
		return parsedRecord{comment: comment.Comment, header: record.RecordHeader, value: record}, nil
	default:
		return parsedRecord{}, fmt.Errorf("unsupported delivery record kind %q", discriminator.Kind)
	}
}

func validateActiveChain(checkpoint DeliveryCheckpoint, records map[int64]parsedRecord) error {
	active := append([]RecordReference(nil), checkpoint.ActiveRecords...)
	sortRecordReferences(active)
	sequence, digest := checkpoint.WindowSequence, checkpoint.WindowDigest
	for _, reference := range active {
		parsed := records[reference.Comment.DatabaseID]
		if reference.Sequence != sequence+1 || parsed.header.PreviousDigest != digest {
			return fmt.Errorf("delivery record sequence %d does not extend the committed chain", reference.Sequence)
		}
		sequence, digest = reference.Sequence, reference.Digest
	}
	if checkpoint.HeadSequence != sequence || checkpoint.HeadDigest != digest {
		return errors.New("delivery checkpoint head does not match the active record chain")
	}
	if checkpoint.ActivePlan != nil {
		if len(active) == 0 || active[len(active)-1] != *checkpoint.ActivePlan || checkpoint.ActivePlan.Kind != RecordPromotionPlan {
			return errors.New("delivery checkpoint active plan is not the active-chain head")
		}
	}
	if checkpoint.CursorBoundary != nil {
		parsed := records[checkpoint.CursorBoundary.Comment.DatabaseID]
		if checkpoint.CursorBoundary.Kind != RecordStagedWork || parsed.header.Sequence != checkpoint.Cursor.ThroughSequence || parsed.header.Digest != checkpoint.Cursor.ThroughDigest {
			return errors.New("cursor boundary does not identify the committed staged-work record")
		}
	}
	if checkpoint.BaseIntegration != nil {
		parsed := records[checkpoint.BaseIntegration.Comment.DatabaseID]
		if checkpoint.BaseIntegration.Kind != RecordBaseIntegration || parsed.header.Sequence != checkpoint.BaseIntegration.Sequence || parsed.header.Digest != checkpoint.BaseIntegration.Digest {
			return errors.New("base-integration record does not match its committed reference")
		}
		integration := parsed.value.(BaseIntegrationRecord)
		if integration.CommittedCursorDigest != checkpoint.Cursor.Digest {
			return errors.New("base-integration record does not match the committed cursor")
		}
		if integration.Source == IntegrationPromotion && integration.PlanDigest != checkpoint.Cursor.ConsumedPlanDigest {
			return errors.New("base-integration record does not match the consumed promotion plan")
		}
	}
	if checkpoint.PromotionIntegration != nil {
		parsed := records[checkpoint.PromotionIntegration.Comment.DatabaseID]
		integration, ok := parsed.value.(BaseIntegrationRecord)
		if !ok || checkpoint.PromotionIntegration.Kind != RecordBaseIntegration || integration.Source != IntegrationPromotion || integration.CommittedCursorDigest != checkpoint.Cursor.Digest || integration.PlanDigest != checkpoint.Cursor.ConsumedPlanDigest {
			return errors.New("promotion-integration record does not match the committed cursor")
		}
	} else if checkpoint.Cursor.Position > 0 {
		if checkpoint.BaseIntegration == nil {
			return errors.New("committed promotion cursor has no promotion-integration record")
		}
		parsed := records[checkpoint.BaseIntegration.Comment.DatabaseID]
		integration, ok := parsed.value.(BaseIntegrationRecord)
		if !ok || integration.Source != IntegrationPromotion || integration.CommittedCursorDigest != checkpoint.Cursor.Digest || integration.PlanDigest != checkpoint.Cursor.ConsumedPlanDigest {
			return errors.New("committed promotion cursor has no promotion-integration record")
		}
	}
	return nil
}

func validateLocator(locator DeliveryStoreLocator) error {
	if err := validateRepository(locator.Repository); err != nil {
		return fmt.Errorf("delivery locator: %w", err)
	}
	if err := validateResource(locator.Issue, "ledger issue"); err != nil {
		return fmt.Errorf("delivery locator: %w", err)
	}
	if err := validateCommentIdentity(locator.Checkpoint); err != nil {
		return fmt.Errorf("delivery locator checkpoint: %w", err)
	}
	return nil
}

func validateCheckpoint(checkpoint DeliveryCheckpoint, locator DeliveryStoreLocator) error {
	if checkpoint.Version != SchemaVersion || checkpoint.Kind != "deliveryCheckpoint" {
		return errors.New("delivery checkpoint version or kind is unsupported")
	}
	if strings.TrimSpace(checkpoint.LedgerID) == "" || checkpoint.Repository != locator.Repository || checkpoint.Issue != locator.Issue {
		return errors.New("delivery checkpoint identity does not match the pinned locator")
	}
	if checkpoint.Generation == 0 {
		return errors.New("delivery checkpoint generation must be positive")
	}
	if checkpoint.GenesisBaseSHA != "" && !validSHA(checkpoint.GenesisBaseSHA) {
		return errors.New("delivery checkpoint genesis base revision is invalid")
	}
	if len(checkpoint.ActiveRecords) > MaxActiveRecords {
		return fmt.Errorf("delivery checkpoint active-record cap exceeded: %d > %d", len(checkpoint.ActiveRecords), MaxActiveRecords)
	}
	if err := validateCursor(checkpoint.Cursor, checkpoint.LedgerID, checkpoint.Repository.NodeID); err != nil {
		return err
	}
	if err := validateCoverage(checkpoint.Coverage, checkpoint); err != nil {
		return err
	}
	if !validDigest(checkpoint.WindowDigest) || checkpoint.Cursor.ThroughSequence > checkpoint.WindowSequence || checkpoint.WindowSequence > checkpoint.HeadSequence {
		return errors.New("delivery checkpoint record window is invalid")
	}
	if checkpoint.WindowSequence == 0 && checkpoint.WindowDigest != genesisDigest(checkpoint.LedgerID, checkpoint.Repository.NodeID) {
		return errors.New("delivery checkpoint has an invalid genesis record window")
	}
	seenComments, seenSequences, seenRetries := map[int64]struct{}{}, map[uint64]struct{}{}, map[string]struct{}{}
	for _, reference := range checkpoint.ActiveRecords {
		if err := validateRecordReference(reference); err != nil {
			return err
		}
		if _, duplicate := seenComments[reference.Comment.DatabaseID]; duplicate {
			return fmt.Errorf("duplicate active delivery comment %d", reference.Comment.DatabaseID)
		}
		seenComments[reference.Comment.DatabaseID] = struct{}{}
		if _, duplicate := seenSequences[reference.Sequence]; duplicate {
			return fmt.Errorf("duplicate active delivery sequence %d", reference.Sequence)
		}
		seenSequences[reference.Sequence] = struct{}{}
		if _, duplicate := seenRetries[reference.RetryID]; duplicate {
			return fmt.Errorf("duplicate active delivery retry identity %q", reference.RetryID)
		}
		seenRetries[reference.RetryID] = struct{}{}
	}
	if checkpoint.BaseIntegration != nil {
		if err := validateRecordReference(*checkpoint.BaseIntegration); err != nil {
			return fmt.Errorf("base-integration reference: %w", err)
		}
		if _, duplicate := seenComments[checkpoint.BaseIntegration.Comment.DatabaseID]; duplicate {
			matched := false
			for _, reference := range checkpoint.ActiveRecords {
				if reference == *checkpoint.BaseIntegration {
					matched = true
					break
				}
			}
			if !matched {
				return errors.New("base-integration comment conflicts with the active record window")
			}
		} else {
			seenComments[checkpoint.BaseIntegration.Comment.DatabaseID] = struct{}{}
		}
	}
	if checkpoint.PromotionIntegration != nil {
		if err := validateRecordReference(*checkpoint.PromotionIntegration); err != nil {
			return fmt.Errorf("promotion-integration reference: %w", err)
		}
		if _, duplicate := seenComments[checkpoint.PromotionIntegration.Comment.DatabaseID]; duplicate {
			matched := checkpoint.BaseIntegration != nil && *checkpoint.BaseIntegration == *checkpoint.PromotionIntegration
			if !matched {
				for _, reference := range checkpoint.ActiveRecords {
					if reference == *checkpoint.PromotionIntegration {
						matched = true
						break
					}
				}
			}
			if !matched {
				return errors.New("promotion-integration comment conflicts with another checkpoint reference")
			}
		} else {
			seenComments[checkpoint.PromotionIntegration.Comment.DatabaseID] = struct{}{}
		}
	}
	if checkpoint.Cursor.ThroughSequence == 0 {
		if checkpoint.CursorBoundary != nil {
			return errors.New("genesis promotion cursor cannot have a staged-work boundary")
		}
	} else {
		if checkpoint.CursorBoundary == nil {
			return errors.New("committed promotion cursor has no staged-work boundary")
		}
		if err := validateRecordReference(*checkpoint.CursorBoundary); err != nil || checkpoint.CursorBoundary.Kind != RecordStagedWork || checkpoint.CursorBoundary.Sequence != checkpoint.Cursor.ThroughSequence || checkpoint.CursorBoundary.Digest != checkpoint.Cursor.ThroughDigest {
			return errors.New("promotion cursor staged-work boundary is invalid")
		}
		if _, duplicate := seenComments[checkpoint.CursorBoundary.Comment.DatabaseID]; duplicate {
			return errors.New("cursor-boundary comment duplicates another checkpoint reference")
		}
	}
	if checkpoint.Digest != checkpointDigest(checkpoint) {
		return errors.New("delivery checkpoint digest is invalid")
	}
	return nil
}

func validateCursor(cursor PromotionCursor, ledgerID, repositoryNodeID string) error {
	if cursor.Version != SchemaVersion || cursor.Kind != "promotionCursor" {
		return errors.New("promotion cursor version or kind is unsupported")
	}
	if cursor.Position == 0 {
		if cursor.ThroughSequence != 0 || cursor.ThroughDigest != genesisDigest(ledgerID, repositoryNodeID) || cursor.ConsumedPlanDigest != "" {
			return errors.New("genesis promotion cursor is invalid")
		}
	} else if !validDigest(cursor.ThroughDigest) || !validDigest(cursor.ConsumedPlanDigest) {
		return errors.New("committed promotion cursor is incomplete")
	}
	if cursor.ThroughSequence == 0 && cursor.ThroughDigest != genesisDigest(ledgerID, repositoryNodeID) {
		return errors.New("promotion cursor has an invalid genesis staged-work boundary")
	}
	if cursor.RetryID != cursorRetryID(cursor) || cursor.Digest != cursorDigest(cursor) {
		return errors.New("promotion cursor digest or retry identity is invalid")
	}
	return nil
}

func validateStagedWork(record StagedWorkRecord) error {
	if err := validateHeader(record.RecordHeader, RecordStagedWork); err != nil {
		return err
	}
	if err := validateResource(record.PullRequest, "work pull request"); err != nil {
		return err
	}
	if strings.TrimSpace(record.StagingBranch) == "" || !validSHA(record.BaseSHA) || !validSHA(record.HeadSHA) || !validSHA(record.MergeRevision) || !validDigest(record.ObservedCursorDigest) {
		return errors.New("staged-work revision preconditions are incomplete")
	}
	if err := validateTimestamp(record.MergedAt); err != nil {
		return fmt.Errorf("staged-work merge time: %w", err)
	}
	if len(record.Issues) == 0 {
		return errors.New("staged-work record has no managed issues")
	}
	previous := 0
	for _, issue := range record.Issues {
		if issue.Number <= previous || strings.TrimSpace(issue.NodeID) == "" || !validDigest(issue.OwnershipDigest) {
			return errors.New("staged-work managed-issue references are invalid or unsorted")
		}
		previous = issue.Number
	}
	if record.RetryID != stagedWorkRetryID(record) || record.Digest != stagedWorkDigest(record) {
		return errors.New("staged-work digest or retry identity is invalid")
	}
	return nil
}

func validatePromotionPlan(record PromotionPlanRecord) error {
	if err := validateHeader(record.RecordHeader, RecordPromotionPlan); err != nil {
		return err
	}
	if err := validateResource(record.PullRequest, "promotion pull request"); err != nil {
		return err
	}
	if !validSHA(record.BaseSHA) || !validSHA(record.HeadSHA) || !validDigest(record.CursorDigest) || !validDigest(record.CoverageDigest) {
		return errors.New("promotion-plan revision preconditions are incomplete")
	}
	if err := validateTimestamp(record.SealedAt); err != nil {
		return fmt.Errorf("promotion-plan seal time: %w", err)
	}
	if err := validateSortedReferences(record.Included, RecordStagedWork); err != nil {
		return fmt.Errorf("promotion-plan included records: %w", err)
	}
	if err := validateSortedReferences(record.Exclusions, RecordExclusion); err != nil {
		return fmt.Errorf("promotion-plan exclusions: %w", err)
	}
	if record.RetryID != promotionPlanRetryID(record) || record.Digest != promotionPlanDigest(record) {
		return errors.New("promotion-plan digest or retry identity is invalid")
	}
	return nil
}

func validateExclusion(record ExclusionRecord) error {
	if err := validateHeader(record.RecordHeader, RecordExclusion); err != nil {
		return err
	}
	if err := validateRecordReference(record.StagedRecord); err != nil || record.StagedRecord.Kind != RecordStagedWork {
		return errors.New("exclusion staged-record reference is invalid")
	}
	if err := validateResource(record.PullRequest, "promotion pull request"); err != nil {
		return err
	}
	if !validSHA(record.HeadSHA) || !validDigest(record.CursorDigest) || record.ReviewHeadSHA != record.HeadSHA || strings.TrimSpace(record.Reason) == "" || strings.TrimSpace(record.RequestedBy) == "" || strings.TrimSpace(record.ApprovedBy) == "" || strings.EqualFold(strings.TrimSpace(record.RequestedBy), strings.TrimSpace(record.ApprovedBy)) || record.RequestRunID <= 0 || record.ReviewID <= 0 || strings.TrimSpace(record.ReviewNodeID) == "" {
		return errors.New("exclusion review evidence or revision preconditions are incomplete")
	}
	requestedAt, err := canonicalTimestamp(record.RequestedAt)
	if err != nil {
		return fmt.Errorf("exclusion request time: %w", err)
	}
	reviewedAt, err := canonicalTimestamp(record.ReviewedAt)
	if err != nil || !reviewedAt.After(requestedAt) {
		return errors.New("exclusion review must occur after the canonical request time")
	}
	_, err = evidenceURL(record.RequestURL, record.Repository, fmt.Sprintf("/%s/actions/runs/%d", record.Repository.FullName, record.RequestRunID), "")
	if err != nil {
		return fmt.Errorf("exclusion request URL: %w", err)
	}
	_, err = evidenceURL(record.ReviewURL, record.Repository, fmt.Sprintf("/%s/pull/%d", record.Repository.FullName, record.PullRequest.Number), fmt.Sprintf("pullrequestreview-%d", record.ReviewID))
	if err != nil {
		return errors.New("exclusion review URL is not exactly bound to the GitHub repository, promotion pull request, and review")
	}
	if record.RetryID != exclusionRetryID(record) || record.Digest != exclusionDigest(record) {
		return errors.New("exclusion digest or retry identity is invalid")
	}
	return nil
}

func validateBaseIntegration(record BaseIntegrationRecord) error {
	if err := validateHeader(record.RecordHeader, RecordBaseIntegration); err != nil {
		return err
	}
	if !validSHA(record.BaseSHA) || !validSHA(record.StagingSHA) || !validDigest(record.PriorCursorDigest) || !validDigest(record.CommittedCursorDigest) {
		return errors.New("base-integration revision preconditions are incomplete")
	}
	switch record.Source {
	case IntegrationPromotion:
		if record.Method != PromotionMerge && record.Method != PromotionSquash && record.Method != PromotionRebase && record.Method != PromotionUnknown {
			return errors.New("promotion integration method is unsupported")
		}
		if err := validateResource(record.PullRequest, "promotion pull request"); err != nil {
			return err
		}
		if !validSHA(record.PromotionBaseSHA) || record.PriorStagingSHA != "" || !validDigest(record.PlanDigest) {
			return errors.New("promotion integration has no sealed-plan digest")
		}
	case IntegrationSynchronization:
		if record.Method != PromotionSync || validateResource(record.PullRequest, "synchronization pull request") != nil || record.PromotionBaseSHA != "" || !validSHA(record.PriorStagingSHA) || record.PlanDigest != "" || record.PriorCursorDigest != record.CommittedCursorDigest {
			return errors.New("synchronization integration contract is invalid")
		}
	default:
		return fmt.Errorf("base-integration source %q is unsupported", record.Source)
	}
	if err := validateTimestamp(record.RecordedAt); err != nil {
		return fmt.Errorf("base-integration record time: %w", err)
	}
	if record.RetryID != baseIntegrationRetryID(record) || record.Digest != baseIntegrationDigest(record) {
		return errors.New("base-integration digest or retry identity is invalid")
	}
	return nil
}

func validateHeader(header RecordHeader, kind RecordKind) error {
	if header.Version != SchemaVersion || header.Kind != kind || strings.TrimSpace(header.LedgerID) == "" {
		return errors.New("delivery record version, kind, or ledger identity is invalid")
	}
	if err := validateRepository(header.Repository); err != nil {
		return err
	}
	if header.Sequence == 0 || !validDigest(header.PreviousDigest) || strings.TrimSpace(header.RetryID) == "" || !validDigest(header.Digest) || strings.TrimSpace(header.Writer.Workflow) == "" || header.Writer.RunID <= 0 {
		return errors.New("delivery record header is incomplete")
	}
	return nil
}

func validateCoverage(coverage StagingCoverage, checkpoint DeliveryCheckpoint) error {
	if coverage.Version != SchemaVersion || coverage.Kind != "stagingCoverage" || coverage.Repository != checkpoint.Repository || !validSHA(coverage.StagingSHA) || !validDigest(coverage.RecordDigest) || coverage.CursorDigest != checkpoint.Cursor.Digest || strings.TrimSpace(coverage.Writer.Workflow) == "" || coverage.Writer.RunID <= 0 {
		return errors.New("staging coverage identity or preconditions are incomplete")
	}
	if err := validateTimestamp(coverage.ObservedAt); err != nil {
		return fmt.Errorf("staging coverage time: %w", err)
	}
	if coverage.RecordSequence < checkpoint.WindowSequence || coverage.RecordSequence > checkpoint.HeadSequence {
		return errors.New("staging coverage is outside the verifiable delivery record window")
	}
	if coverage.RecordSequence == checkpoint.WindowSequence {
		if coverage.RecordDigest != checkpoint.WindowDigest {
			return errors.New("staging coverage does not match the committed record window")
		}
	} else if coverage.RecordSequence > checkpoint.WindowSequence {
		matched := false
		for _, reference := range checkpoint.ActiveRecords {
			if reference.Sequence == coverage.RecordSequence && reference.Digest == coverage.RecordDigest {
				matched = true
				break
			}
		}
		if !matched {
			return errors.New("staging coverage does not match an active record boundary")
		}
	}
	if coverage.Digest != coverageDigest(coverage) {
		return errors.New("staging coverage digest is invalid")
	}
	return nil
}

func validateRepository(repository RepositoryIdentity) error {
	if strings.TrimSpace(repository.Host) == "" || strings.Contains(repository.Host, "://") || strings.Contains(repository.Host, "/") || strings.TrimSpace(repository.FullName) == "" || !strings.Contains(repository.FullName, "/") || strings.TrimSpace(repository.NodeID) == "" {
		return errors.New("repository identity is incomplete")
	}
	return nil
}

func validateResource(resource ResourceIdentity, name string) error {
	if resource.Number <= 0 || strings.TrimSpace(resource.NodeID) == "" {
		return fmt.Errorf("%s identity is incomplete", name)
	}
	return nil
}

func validateCommentIdentity(comment CommentIdentity) error {
	if comment.DatabaseID <= 0 || strings.TrimSpace(comment.NodeID) == "" {
		return errors.New("comment identity is incomplete")
	}
	return nil
}

func validateRecordReference(reference RecordReference) error {
	if err := validateCommentIdentity(reference.Comment); err != nil {
		return err
	}
	if reference.Sequence == 0 || strings.TrimSpace(string(reference.Kind)) == "" || !validDigest(reference.Digest) || strings.TrimSpace(reference.RetryID) == "" {
		return errors.New("delivery record reference is incomplete")
	}
	return nil
}

func validateSortedReferences(references []RecordReference, kind RecordKind) error {
	var previous uint64
	for _, reference := range references {
		if err := validateRecordReference(reference); err != nil {
			return err
		}
		if reference.Kind != kind || reference.Sequence <= previous {
			return errors.New("record references have the wrong kind, duplicate sequence, or order")
		}
		previous = reference.Sequence
	}
	return nil
}

func validateTrustedComment(comment StoredComment) error {
	if err := validateCommentIdentity(comment.Comment); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(comment.AuthorLogin), TrustedAuthorLogin) || !strings.EqualFold(strings.TrimSpace(comment.AuthorType), TrustedAuthorType) {
		return errors.New("comment author is not trusted delivery automation")
	}
	return nil
}

func matchReference(reference RecordReference, parsed parsedRecord) error {
	if reference.Comment != parsed.comment || reference.Kind != parsed.header.Kind || reference.Sequence != parsed.header.Sequence || reference.Digest != parsed.header.Digest || reference.RetryID != parsed.header.RetryID {
		return errors.New("record content does not match its checkpoint reference")
	}
	return nil
}

func referencedComments(checkpoint DeliveryCheckpoint) map[int64]RecordReference {
	result := make(map[int64]RecordReference, len(checkpoint.ActiveRecords)+2)
	for _, reference := range checkpoint.ActiveRecords {
		result[reference.Comment.DatabaseID] = reference
	}
	if checkpoint.BaseIntegration != nil {
		result[checkpoint.BaseIntegration.Comment.DatabaseID] = *checkpoint.BaseIntegration
	}
	if checkpoint.PromotionIntegration != nil {
		result[checkpoint.PromotionIntegration.Comment.DatabaseID] = *checkpoint.PromotionIntegration
	}
	if checkpoint.CursorBoundary != nil {
		result[checkpoint.CursorBoundary.Comment.DatabaseID] = *checkpoint.CursorBoundary
	}
	return result
}

func extractMarker(body, wanted string) ([]byte, error) {
	if strings.Count(body, "<!-- baton-delivery-") != 1 {
		return nil, errors.New("expected exactly one delivery marker")
	}
	prefix := "<!-- " + wanted + " "
	start := strings.Index(body, prefix)
	if start < 0 {
		return nil, fmt.Errorf("expected %s marker", wanted)
	}
	rest := body[start+len(prefix):]
	end := strings.Index(rest, " -->")
	if end < 0 || strings.Contains(rest[:end], "\n") || strings.Contains(rest[:end], "\r") {
		return nil, errors.New("delivery marker is malformed")
	}
	return []byte(rest[:end]), nil
}

func renderMarker(marker string, value any) string {
	content, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return "<!-- " + marker + " " + string(content) + " -->"
}

func strictUnmarshal(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("unexpected trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	canonical, err := json.Marshal(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, canonical) {
		return errors.New("delivery JSON is not in canonical form")
	}
	return nil
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validateTimestamp(value string) error {
	_, err := canonicalTimestamp(value)
	return err
}

func canonicalTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp is not RFC3339")
	}
	if parsed.Format(time.RFC3339) != value || !strings.HasSuffix(value, "Z") {
		return time.Time{}, errors.New("timestamp must use canonical UTC RFC3339 form")
	}
	return parsed, nil
}

func evidenceURL(raw string, repository RepositoryIdentity, exactPath, exactFragment string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, repository.Host) || parsed.Path != exactPath || parsed.Fragment != exactFragment || parsed.RawQuery != "" {
		return nil, errors.New("URL is not an https GitHub repository resource")
	}
	return parsed, nil
}

func promotionPlanByDigest(records []PromotionPlanRecord, digest string) (PromotionPlanRecord, bool) {
	for _, record := range records {
		if record.Digest == digest {
			return record, true
		}
	}
	return PromotionPlanRecord{}, false
}

func exclusionByDigest(records []ExclusionRecord, digest string) (ExclusionRecord, bool) {
	for _, record := range records {
		if record.Digest == digest {
			return record, true
		}
	}
	return ExclusionRecord{}, false
}

func checkpointDigest(checkpoint DeliveryCheckpoint) string {
	checkpoint.Digest = ""
	return canonicalDigest(checkpoint)
}

func cursorRetryID(cursor PromotionCursor) string {
	cursor.RetryID, cursor.Digest = "", ""
	return canonicalDigest(cursor)
}

func cursorDigest(cursor PromotionCursor) string {
	cursor.Digest = ""
	return canonicalDigest(cursor)
}

func stagedWorkRetryID(record StagedWorkRecord) string {
	record.RetryID, record.Digest = "", ""
	record.Writer = WriterProvenance{}
	record.Sequence, record.PreviousDigest = 0, ""
	return canonicalDigest(record)
}

func stagedWorkDigest(record StagedWorkRecord) string {
	record.Digest = ""
	return canonicalDigest(record)
}

func promotionPlanRetryID(record PromotionPlanRecord) string {
	record.RetryID, record.Digest = "", ""
	record.Writer = WriterProvenance{}
	record.Sequence, record.PreviousDigest = 0, ""
	record.SealedAt = ""
	return canonicalDigest(record)
}

func promotionPlanDigest(record PromotionPlanRecord) string {
	record.Digest = ""
	return canonicalDigest(record)
}

func exclusionRetryID(record ExclusionRecord) string {
	record.RetryID, record.Digest = "", ""
	record.Writer = WriterProvenance{}
	record.Sequence, record.PreviousDigest = 0, ""
	return canonicalDigest(record)
}

func exclusionDigest(record ExclusionRecord) string {
	record.Digest = ""
	return canonicalDigest(record)
}

func baseIntegrationRetryID(record BaseIntegrationRecord) string {
	record.RetryID, record.Digest = "", ""
	record.Writer = WriterProvenance{}
	record.Sequence, record.PreviousDigest = 0, ""
	record.RecordedAt = ""
	return canonicalDigest(record)
}

func baseIntegrationDigest(record BaseIntegrationRecord) string {
	record.Digest = ""
	return canonicalDigest(record)
}

func coverageDigest(coverage StagingCoverage) string {
	coverage.Digest = ""
	return canonicalDigest(coverage)
}
