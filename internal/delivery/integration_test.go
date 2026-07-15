package delivery

import (
	"strings"
	"testing"
)

func TestBaseIntegrationClassificationDoesNotUseRawBaseToStagingAncestry(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	checkpoint.GenesisBaseSHA = strings.Repeat("1", 40)
	checkpoint = finalizeCheckpoint(checkpoint)
	snapshot := Snapshot{Locator: locator, Checkpoint: checkpoint}

	integrated, err := ClassifyBaseIntegration(snapshot, BaseIntegrationObservation{
		BaseSHA: strings.Repeat("1", 40), StagingSHA: strings.Repeat("2", 40),
		BaseRelation: RevisionIdentical, StagingRelation: RevisionAhead,
	})
	if err != nil || integrated.State != BaseIntegrated {
		t.Fatalf("integrated facts = %+v, err = %v", integrated, err)
	}
	pending, err := ClassifyBaseIntegration(snapshot, BaseIntegrationObservation{
		BaseSHA: strings.Repeat("3", 40), StagingSHA: strings.Repeat("3", 40),
		BaseRelation: RevisionAhead, StagingRelation: RevisionAhead,
	})
	if err != nil || pending.State != BaseDirectWorkPending {
		t.Fatalf("pending facts = %+v, err = %v", pending, err)
	}
}

func TestGenesisClassificationDoesNotTreatAdvancedCoverageAsGenesis(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	checkpoint.Coverage.StagingSHA = strings.Repeat("c", 40)
	checkpoint.Coverage = finalizeCoverage(checkpoint.Coverage)
	checkpoint = finalizeCheckpoint(checkpoint)
	facts, err := ClassifyBaseIntegration(Snapshot{Locator: locator, Checkpoint: checkpoint}, BaseIntegrationObservation{
		BaseSHA: checkpoint.GenesisBaseSHA, StagingSHA: checkpoint.Coverage.StagingSHA,
		BaseRelation: RevisionIdentical, StagingRelation: RevisionDiverged,
	})
	if err != nil || facts.State != BaseIntegrationDiverged {
		t.Fatalf("facts = %+v, err = %v", facts, err)
	}
}

func TestRecordedPromotionResultIsIntegratedForEveryMergeMethod(t *testing.T) {
	for index, method := range []PromotionMethod{PromotionMerge, PromotionSquash, PromotionRebase} {
		base := strings.Repeat(string(rune('a'+index)), 40)
		store, _ := fixtureStore(t, adapterFixtureScenario{Name: string(method), Lifecycle: "consumed", Method: method, BaseResultSHA: base})
		snapshot, err := ParseStoreSnapshot(store)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := ClassifyBaseIntegration(snapshot, BaseIntegrationObservation{BaseSHA: base, StagingSHA: strings.Repeat("9", 40), BaseRelation: RevisionIdentical, StagingRelation: RevisionAhead})
		if err != nil || facts.State != BaseIntegrated {
			t.Fatalf("method %s facts = %+v, err = %v", method, facts, err)
		}
	}
}

func TestPromotionIntegrationRejectsStagingRewriteBeforePendingBaseWork(t *testing.T) {
	base := strings.Repeat("a", 40)
	store, _ := fixtureStore(t, adapterFixtureScenario{Name: "staging-rewrite", Lifecycle: "consumed", Method: PromotionSquash, BaseResultSHA: base})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := ClassifyBaseIntegration(snapshot, BaseIntegrationObservation{
		BaseSHA: strings.Repeat("b", 40), StagingSHA: strings.Repeat("c", 40), BaseRelation: RevisionAhead, StagingRelation: RevisionDiverged,
	})
	if err != nil || facts.State != BaseIntegrationDiverged {
		t.Fatalf("facts = %+v, err = %v", facts, err)
	}
}

func TestSynchronizationReplayAfterPromotionIsRejected(t *testing.T) {
	base := strings.Repeat("a", 40)
	store, _ := fixtureStore(t, adapterFixtureScenario{Name: "sync-replay", Lifecycle: "consumed", Method: PromotionMerge, BaseResultSHA: base})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	promotion := snapshot.BaseIntegrations[0]
	_, err = PlanSynchronization(snapshot, SynchronizationInput{
		PullRequest: ResourceIdentity{Number: 30, NodeID: "PR_30"}, PriorStagingSHA: strings.Repeat("1", 40),
		BaseSHA: strings.Repeat("2", 40), StagingSHA: promotion.StagingSHA, Writer: fixtureWriter(), RecordedAt: "2026-07-15T01:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("replay error = %v", err)
	}
}

func TestSynchronizationPreservesCommittedPromotionIdempotence(t *testing.T) {
	store, _ := fixtureStore(t, adapterFixtureScenario{Name: "promotion-then-sync", Lifecycle: "consumed", Method: PromotionMerge, BaseResultSHA: strings.Repeat("a", 40)})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	promotion := snapshot.BaseIntegrations[0]
	syncPlan, err := PlanSynchronization(snapshot, SynchronizationInput{
		PullRequest: ResourceIdentity{Number: 30, NodeID: "PR_30"}, PriorStagingSHA: promotion.StagingSHA,
		BaseSHA: strings.Repeat("b", 40), StagingSHA: strings.Repeat("c", 40), Writer: fixtureWriter(), RecordedAt: "2026-07-15T01:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	syncCommit, err := FinalizeSynchronization(syncPlan, snapshot.Checkpoint, CommentIdentity{DatabaseID: 999, NodeID: "IC_999"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Checkpoint = syncCommit.Checkpoint
	snapshot.BaseIntegrations = append(snapshot.BaseIntegrations, syncCommit.Record)
	duplicate, err := PlanPromotionConsumption(snapshot, PromotionConsumptionInput{
		Revision:      PromotionRevision{RepositoryNodeID: snapshot.Checkpoint.Repository.NodeID, PullRequest: promotion.PullRequest, BaseSHA: promotion.PromotionBaseSHA, HeadSHA: promotion.StagingSHA},
		MergeRevision: promotion.BaseSHA, Method: promotion.Method, Writer: fixtureWriter(), RecordedAt: "2026-07-15T02:00:00Z",
	})
	if err != nil || duplicate.Applicable || duplicate.Existing == nil || duplicate.Existing.Record.Digest != promotion.Digest {
		t.Fatalf("duplicate promotion = %+v, err = %v", duplicate, err)
	}
}

func TestSynchronizationPreservesActiveWorkAndCursor(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	appendPlan, err := PlanStagedWorkAppend(Snapshot{Locator: locator, Checkpoint: checkpoint}, plannerStagedInput(20, 7))
	if err != nil {
		t.Fatal(err)
	}
	staged, err := FinalizeStagedWorkAppend(appendPlan, checkpoint, CommentIdentity{DatabaseID: 101, NodeID: "IC_101"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Locator: locator, Checkpoint: staged.Checkpoint, StagedWork: []StagedWorkRecord{staged.Record}}
	plan, err := PlanSynchronization(snapshot, SynchronizationInput{
		PullRequest: ResourceIdentity{Number: 30, NodeID: "PR_30"}, PriorStagingSHA: strings.Repeat("3", 40),
		BaseSHA: strings.Repeat("4", 40), StagingSHA: strings.Repeat("5", 40), Writer: fixtureWriter(), RecordedAt: "2026-07-15T01:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := FinalizeSynchronization(plan, staged.Checkpoint, CommentIdentity{DatabaseID: 102, NodeID: "IC_102"})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Checkpoint.Cursor != staged.Checkpoint.Cursor || len(commit.Checkpoint.ActiveRecords) != 2 || commit.Checkpoint.BaseIntegration == nil || *commit.Checkpoint.BaseIntegration != commit.Checkpoint.ActiveRecords[1] {
		t.Fatalf("synchronization checkpoint = %+v", commit.Checkpoint)
	}
	parsed, err := ParseStoreSnapshot(StoreSnapshot{
		Locator:    locator,
		Checkpoint: fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, commit.CheckpointBody),
		Records: []StoredComment{
			fixtureComment(101, "IC_101", staged.RecordBody),
			fixtureComment(102, "IC_102", commit.RecordBody),
		}, Complete: true,
	})
	if err != nil || len(parsed.StagedWork) != 1 || len(parsed.BaseIntegrations) != 1 {
		t.Fatalf("parsed synchronized store = %+v, err = %v", parsed, err)
	}
	if _, err := FinalizeSynchronization(plan, commit.Checkpoint, CommentIdentity{DatabaseID: 103, NodeID: "IC_103"}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale finalization error = %v", err)
	}
}

func TestSynchronizationHasASeparateReserveBeforePromotionSeal(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	checkpoint.ActiveRecords = make([]RecordReference, MaxOperationalRecords)
	for index := range checkpoint.ActiveRecords {
		checkpoint.ActiveRecords[index] = RecordReference{
			Comment: CommentIdentity{DatabaseID: int64(1000 + index), NodeID: "IC_" + strings.Repeat("x", index+1)},
			Kind:    RecordStagedWork, Sequence: uint64(index + 1), Digest: fixtureDigest("record-" + strings.Repeat("x", index+1)), RetryID: fixtureDigest("retry-" + strings.Repeat("x", index+1)),
		}
	}
	checkpoint.HeadSequence = uint64(len(checkpoint.ActiveRecords))
	checkpoint.HeadDigest = checkpoint.ActiveRecords[len(checkpoint.ActiveRecords)-1].Digest
	checkpoint = finalizeCheckpoint(checkpoint)
	snapshot := Snapshot{Locator: locator, Checkpoint: checkpoint}
	input := SynchronizationInput{
		PullRequest: ResourceIdentity{Number: 30, NodeID: "PR_30"}, PriorStagingSHA: strings.Repeat("1", 40),
		BaseSHA: strings.Repeat("2", 40), StagingSHA: strings.Repeat("3", 40), Writer: fixtureWriter(), RecordedAt: "2026-07-15T01:00:00Z",
	}
	plan, err := PlanSynchronization(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := FinalizeSynchronization(plan, checkpoint, CommentIdentity{DatabaseID: 2000, NodeID: "IC_2000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(commit.Checkpoint.ActiveRecords) != MaxSynchronizationRecords || len(commit.Checkpoint.ActiveRecords) >= MaxActiveRecords {
		t.Fatalf("synchronization reserve = %d active records", len(commit.Checkpoint.ActiveRecords))
	}
	snapshot.Checkpoint = commit.Checkpoint
	snapshot.BaseIntegrations = append(snapshot.BaseIntegrations, commit.Record)
	input.PullRequest = ResourceIdentity{Number: 31, NodeID: "PR_31"}
	input.PriorStagingSHA, input.BaseSHA, input.StagingSHA = input.StagingSHA, strings.Repeat("4", 40), strings.Repeat("5", 40)
	if _, err := PlanSynchronization(snapshot, input); err == nil || !strings.Contains(err.Error(), "one slot is reserved") {
		t.Fatalf("second synchronization at reserve error = %v", err)
	}
}
