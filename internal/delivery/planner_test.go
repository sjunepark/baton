package delivery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckpointIndexAndGenesisRoundTrip(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	body, err := RenderCheckpointIndex(locator, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCheckpointIndex(locator, fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, body))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Digest != checkpoint.Digest || parsed.Generation != 1 || parsed.HeadDigest != parsed.WindowDigest || parsed.Cursor.Position != 0 {
		t.Fatalf("parsed genesis checkpoint = %+v", parsed)
	}

	untrusted := fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, body)
	untrusted.AuthorLogin = "contributor"
	if _, err := ParseCheckpointIndex(locator, untrusted); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("untrusted parse error = %v", err)
	}
	tampered := checkpoint
	tampered.HeadDigest = fixtureDigest("tampered")
	if _, err := RenderCheckpointIndex(locator, tampered); err == nil || !strings.Contains(err.Error(), "digest is invalid") {
		t.Fatalf("tampered render error = %v", err)
	}
}

func TestStagedWorkAppendPlansThenFinalizesExactCheckpoint(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	snapshot := Snapshot{Locator: locator, Checkpoint: checkpoint}
	input := plannerStagedInput(20, 7)
	plan, err := PlanStagedWorkAppend(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil {
		t.Fatalf("append plan = %+v", plan)
	}
	if plan.Record.Sequence != 1 || plan.Record.PreviousDigest != checkpoint.HeadDigest || plan.Record.ObservedCursorDigest != checkpoint.Cursor.Digest {
		t.Fatalf("planned record is not checkpoint-bound: %+v", plan.Record)
	}
	if plan.Checkpoint.Checkpoint.Digest != "" || plan.Checkpoint.Checkpoint.ActivePlan != nil || plan.Checkpoint.PendingRecord.Digest != plan.Record.Digest {
		t.Fatalf("checkpoint template = %+v", plan.Checkpoint)
	}

	recordComment := CommentIdentity{DatabaseID: 201, NodeID: "IC_201"}
	commit, err := FinalizeStagedWorkAppend(plan, checkpoint, recordComment)
	if err != nil {
		t.Fatal(err)
	}
	if commit.Checkpoint.Generation != 2 || commit.Checkpoint.HeadDigest != commit.Record.Digest || commit.Checkpoint.Coverage.StagingSHA != input.MergeRevision {
		t.Fatalf("append commit = %+v", commit)
	}
	store := StoreSnapshot{
		Locator:    locator,
		Checkpoint: fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, commit.CheckpointBody),
		Records:    []StoredComment{fixtureComment(recordComment.DatabaseID, recordComment.NodeID, commit.RecordBody)},
		Complete:   true,
	}
	parsed, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.StagedWork) != 1 || parsed.StagedWork[0].Digest != commit.Record.Digest {
		t.Fatalf("parsed staged work = %+v", parsed.StagedWork)
	}

	retry, err := PlanStagedWorkAppend(parsed, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Applicable || retry.Existing == nil || retry.Existing.Reference != commit.RecordReference {
		t.Fatalf("retry plan = %+v", retry)
	}
}

func TestFindStagedWorkRetryRecoversOneTrustedOrphan(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	plan, err := PlanStagedWorkAppend(Snapshot{Locator: locator, Checkpoint: checkpoint}, plannerStagedInput(20, 7))
	if err != nil {
		t.Fatal(err)
	}
	orphan := fixtureComment(201, "IC_201", renderStagedWork(*plan.Record))
	parsed, err := ParseStagedWorkComment(orphan)
	if err != nil || parsed.Digest != plan.Record.Digest {
		t.Fatalf("parsed orphan = %+v, error = %v", parsed, err)
	}
	match, err := FindStagedWorkRetry([]StoredComment{
		fixtureComment(200, "IC_200", "ordinary issue comment"), orphan,
	}, plan.Record.RetryID)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Comment != orphan.Comment || match.Record.Digest != plan.Record.Digest {
		t.Fatalf("retry match = %+v", match)
	}
	attempt := *plan.Record
	attempt.Writer.RunID++
	attempt = finalizeStagedWork(attempt)
	if attempt.RetryID != plan.Record.RetryID || attempt.Digest == plan.Record.Digest {
		t.Fatalf("attempt-local staged retry identity changed: %+v", attempt)
	}
	adopted, err := AdoptStagedWorkRetry(plan, checkpoint, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Record.Digest != attempt.Digest {
		t.Fatalf("adopted staged retry = %+v", adopted.Record)
	}

	duplicate := orphan
	duplicate.Comment = CommentIdentity{DatabaseID: 202, NodeID: "IC_202"}
	if _, err := FindStagedWorkRetry([]StoredComment{orphan, duplicate}, plan.Record.RetryID); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate retry error = %v", err)
	}
	tooMany := make([]StoredComment, MaxRetryComments+1)
	if _, err := FindStagedWorkRetry(tooMany, plan.Record.RetryID); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("retry cap error = %v", err)
	}
}

func TestStagedWorkAppendRejectsStaleAndTamperedPlans(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	plan, err := PlanStagedWorkAppend(Snapshot{Locator: locator, Checkpoint: checkpoint}, plannerStagedInput(20, 7))
	if err != nil {
		t.Fatal(err)
	}
	staleInput := GenesisCheckpointInput{
		LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository, Issue: checkpoint.Issue,
		StagingSHA: strings.Repeat("9", 40), Writer: fixtureWriter(), ObservedAt: "2026-07-14T01:01:00Z",
	}
	stale, err := NewGenesisCheckpoint(staleInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FinalizeStagedWorkAppend(plan, stale, CommentIdentity{DatabaseID: 201, NodeID: "IC_201"}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale finalize error = %v", err)
	}

	tampered := plan
	template := *plan.Checkpoint
	template.Checkpoint.Generation++
	tampered.Checkpoint = &template
	if _, err := FinalizeStagedWorkAppend(tampered, checkpoint, CommentIdentity{DatabaseID: 201, NodeID: "IC_201"}); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("tampered finalize error = %v", err)
	}
}

func TestCoverageAdvanceChangesOnlyCoverageAndCheckpointGeneration(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	plan, err := PlanCoverageAdvance(Snapshot{Locator: locator, Checkpoint: checkpoint}, CoverageAdvanceInput{
		StagingSHA: strings.Repeat("b", 40), Writer: fixtureWriter(), ObservedAt: "2026-07-14T02:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicable || plan.Checkpoint == nil || plan.CheckpointBody == "" {
		t.Fatalf("coverage plan = %+v", plan)
	}
	if plan.Checkpoint.Generation != checkpoint.Generation+1 || plan.Checkpoint.HeadDigest != checkpoint.HeadDigest || plan.Checkpoint.Cursor.Digest != checkpoint.Cursor.Digest {
		t.Fatalf("coverage advance changed record or cursor state: %+v", plan.Checkpoint)
	}
	if plan.Checkpoint.Coverage.StagingSHA != strings.Repeat("b", 40) || plan.Checkpoint.Coverage.RecordDigest != checkpoint.HeadDigest {
		t.Fatalf("coverage advance = %+v", plan.Checkpoint.Coverage)
	}
	parsed, err := ParseCheckpointIndex(locator, fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, plan.CheckpointBody))
	if err != nil || parsed.Digest != plan.Checkpoint.Digest {
		t.Fatalf("parsed coverage checkpoint = %+v, error = %v", parsed, err)
	}

	unchanged, err := PlanCoverageAdvance(Snapshot{Locator: locator, Checkpoint: checkpoint}, CoverageAdvanceInput{
		StagingSHA: checkpoint.Coverage.StagingSHA, Writer: fixtureWriter(), ObservedAt: "2026-07-14T02:00:00Z",
	})
	if err != nil || unchanged.Applicable || unchanged.Checkpoint != nil {
		t.Fatalf("unchanged coverage plan = %+v, error = %v", unchanged, err)
	}
}

func TestStagedWorkAppendCapDoesNotBlockCommittedRetry(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	input := plannerStagedInput(20, 7)
	firstPlan, err := PlanStagedWorkAppend(Snapshot{Locator: locator, Checkpoint: checkpoint}, input)
	if err != nil {
		t.Fatal(err)
	}
	record := *firstPlan.Record
	reference := recordReference(CommentIdentity{DatabaseID: 201, NodeID: "IC_201"}, record.RecordHeader)
	checkpoint.ActiveRecords = make([]RecordReference, MaxActiveRecords)
	checkpoint.ActiveRecords[0] = reference
	for index := 1; index < MaxActiveRecords; index++ {
		checkpoint.ActiveRecords[index] = RecordReference{
			Comment: CommentIdentity{DatabaseID: int64(201 + index), NodeID: "IC_" + strings.Repeat("x", index+1)},
			Kind:    RecordStagedWork, Sequence: uint64(index + 1), Digest: fixtureDigest("record-" + strings.Repeat("x", index)), RetryID: fixtureDigest("retry-" + strings.Repeat("x", index)),
		}
	}
	checkpoint.HeadSequence = MaxActiveRecords
	checkpoint.HeadDigest = checkpoint.ActiveRecords[len(checkpoint.ActiveRecords)-1].Digest
	checkpoint = finalizeCheckpoint(checkpoint)
	snapshot := Snapshot{Locator: locator, Checkpoint: checkpoint, StagedWork: []StagedWorkRecord{record}}
	retry, err := PlanStagedWorkAppend(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Applicable || retry.Existing == nil {
		t.Fatalf("cap retry plan = %+v", retry)
	}

	newInput := plannerStagedInput(21, 8)
	if _, err := PlanStagedWorkAppend(snapshot, newInput); err == nil || !strings.Contains(err.Error(), "cap reached") {
		t.Fatalf("new append at cap error = %v", err)
	}
}

func TestPromotionSealPlansFinalizesAndResolvesDurableIssues(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	stagedPlan, err := PlanStagedWorkAppend(Snapshot{Locator: locator, Checkpoint: checkpoint}, plannerStagedInput(20, 7))
	if err != nil {
		t.Fatal(err)
	}
	stagedComment := CommentIdentity{DatabaseID: 201, NodeID: "IC_201"}
	staged, err := FinalizeStagedWorkAppend(stagedPlan, checkpoint, stagedComment)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{Locator: locator, Checkpoint: staged.Checkpoint, StagedWork: []StagedWorkRecord{staged.Record}}
	revision := PromotionRevision{
		RepositoryNodeID: locator.Repository.NodeID,
		PullRequest:      ResourceIdentity{Number: 50, NodeID: "PR_50"},
		BaseSHA:          strings.Repeat("4", 40),
		HeadSHA:          staged.Record.MergeRevision,
	}
	plan, err := PlanPromotionSeal(snapshot, PromotionSealInput{Revision: revision, Writer: fixtureWriter(), SealedAt: "2026-07-14T02:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || len(plan.Work) != 1 || len(plan.ExpectedIssues) != 1 || plan.ExpectedIssues[0].Number != 7 {
		t.Fatalf("seal plan = %+v", plan)
	}
	if len(plan.Record.Included) != 1 || len(plan.Record.Exclusions) != 0 || plan.Record.CoverageDigest != staged.Checkpoint.Coverage.Digest {
		t.Fatalf("promotion record = %+v", plan.Record)
	}
	planComment := CommentIdentity{DatabaseID: 202, NodeID: "IC_202"}
	commit, err := FinalizePromotionSeal(plan, staged.Checkpoint, planComment)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ParseStoreSnapshot(StoreSnapshot{
		Locator:    locator,
		Checkpoint: fixtureComment(locator.Checkpoint.DatabaseID, locator.Checkpoint.NodeID, commit.CheckpointBody),
		Records: []StoredComment{
			fixtureComment(stagedComment.DatabaseID, stagedComment.NodeID, staged.RecordBody),
			fixtureComment(planComment.DatabaseID, planComment.NodeID, commit.RecordBody),
		},
		Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveSealedPromotion(store, revision)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Plan.Digest != commit.Record.Digest || len(resolved.Work) != 1 || resolved.Work[0].Excluded || len(resolved.ExpectedIssues) != 1 || resolved.ExpectedIssues[0].OwnershipDigest != staged.Record.Issues[0].OwnershipDigest {
		t.Fatalf("resolved promotion = %+v", resolved)
	}
	retry, err := PlanPromotionSeal(store, PromotionSealInput{Revision: revision, Writer: WriterProvenance{Workflow: "another-attempt", RunID: 99}, SealedAt: "2026-07-14T03:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Applicable || retry.Existing == nil || retry.Existing.Reference != commit.RecordReference {
		t.Fatalf("seal retry = %+v", retry)
	}
}

func TestPromotionSealFailsClosedForCoverageGapAndSupportsManualOnly(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	revision := PromotionRevision{
		RepositoryNodeID: locator.Repository.NodeID,
		PullRequest:      ResourceIdentity{Number: 50, NodeID: "PR_50"},
		BaseSHA:          strings.Repeat("4", 40),
		HeadSHA:          strings.Repeat("b", 40),
	}
	input := PromotionSealInput{Revision: revision, Writer: fixtureWriter(), SealedAt: "2026-07-14T02:00:00Z"}
	if _, err := PlanPromotionSeal(Snapshot{Locator: locator, Checkpoint: checkpoint}, input); err == nil || !strings.Contains(err.Error(), "exact promotion head") {
		t.Fatalf("coverage-gap error = %v", err)
	}
	revision.HeadSHA = checkpoint.Coverage.StagingSHA
	input.Revision = revision
	plan, err := PlanPromotionSeal(Snapshot{Locator: locator, Checkpoint: checkpoint}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicable || len(plan.Work) != 0 || len(plan.ExpectedIssues) != 0 || len(plan.Record.Included) != 0 {
		t.Fatalf("manual-only seal = %+v", plan)
	}
}

func TestFindPromotionPlanRetryIsBoundedAndRejectsAmbiguity(t *testing.T) {
	checkpoint, locator := plannerGenesis(t)
	revision := PromotionRevision{RepositoryNodeID: locator.Repository.NodeID, PullRequest: ResourceIdentity{Number: 50, NodeID: "PR_50"}, BaseSHA: strings.Repeat("4", 40), HeadSHA: checkpoint.Coverage.StagingSHA}
	plan, err := PlanPromotionSeal(Snapshot{Locator: locator, Checkpoint: checkpoint}, PromotionSealInput{Revision: revision, Writer: fixtureWriter(), SealedAt: "2026-07-14T02:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	orphan := fixtureComment(201, "IC_201", renderPromotionPlan(*plan.Record))
	match, err := FindPromotionPlanRetry([]StoredComment{orphan}, plan.Record.RetryID)
	if err != nil || match == nil || match.Record.Digest != plan.Record.Digest {
		t.Fatalf("retry match = %+v, error = %v", match, err)
	}
	attempt := *plan.Record
	attempt.Writer.RunID++
	attempt.SealedAt = "2026-07-14T03:00:00Z"
	attempt = finalizePromotionPlan(attempt)
	if attempt.RetryID != plan.Record.RetryID || attempt.Digest == plan.Record.Digest {
		t.Fatalf("attempt-local promotion retry identity changed: %+v", attempt)
	}
	adopted, err := AdoptPromotionPlanRetry(plan, checkpoint, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Record.Digest != attempt.Digest {
		t.Fatalf("adopted promotion retry = %+v", adopted.Record)
	}
	duplicate := orphan
	duplicate.Comment = CommentIdentity{DatabaseID: 202, NodeID: "IC_202"}
	if _, err := FindPromotionPlanRetry([]StoredComment{orphan, duplicate}, plan.Record.RetryID); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate retry error = %v", err)
	}
	if _, err := FindPromotionPlanRetry(make([]StoredComment, MaxRetryComments+1), plan.Record.RetryID); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("retry cap error = %v", err)
	}
}

func TestBootstrapPlanIsStableAndGenesisAmbiguityBlocksApply(t *testing.T) {
	genesis, _ := plannerGenesis(t)
	facts := []BootstrapSourceFact{
		{ID: "pr-20", Kind: "mergedStagingPullRequest", Source: "githubPullRequest", ObservedDigest: fixtureDigest("pr-20"), Details: map[string]string{"number": "20"}},
		{ID: "issue-7", Kind: "managedIssue", Source: "githubIssue", ObservedDigest: fixtureDigest("issue-7"), Details: map[string]string{"number": "7"}},
	}
	input := BootstrapPlanInput{
		Genesis: GenesisCheckpointInput{
			LedgerID: genesis.LedgerID, Repository: genesis.Repository, Issue: genesis.Issue,
			StagingSHA: strings.Repeat("a", 40), Writer: fixtureWriter(), ObservedAt: "2026-07-14T00:00:00Z",
		},
		SourceFacts:      facts,
		Relationships:    []BootstrapRelationship{{Kind: "delivers", FromFactID: "pr-20", ToFactID: "issue-7", EvidenceFactIDs: []string{"pr-20"}}},
		Ambiguities:      []BootstrapAmbiguity{{Code: "promotion-boundary", Message: "last promotion cannot be proven", FactIDs: []string{"pr-20"}, RequiresGenesisBoundary: true}},
		OwnershipRecords: []BootstrapOwnershipRecord{{Issue: ResourceIdentity{Number: 7, NodeID: "I_7"}, Digest: fixtureDigest("ownership-7"), Body: "<!-- exact ownership -->", SourceFactIDs: []string{"issue-7"}}},
		StagedWork:       []BootstrapStagedWork{{Order: 1, SourceFactIDs: []string{"issue-7", "pr-20"}, Input: plannerStagedInput(20, 7)}},
	}
	blocked, err := PlanBootstrap(input)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Applicable || blocked.Ambiguities[0].ResolvedBy != "" || len(blocked.StagedWork) != 1 || blocked.OwnershipRecords[0].Body == "" {
		t.Fatalf("blocked bootstrap plan = %+v", blocked)
	}

	input.GenesisBoundary = &BootstrapGenesisBoundary{
		Explicit: true, StagingSHA: strings.Repeat("b", 40), BaseSHA: strings.Repeat("c", 40),
		SourceFactIDs: []string{"pr-20"}, Rationale: "Reviewed last acknowledged promotion.",
	}
	applicable, err := PlanBootstrap(input)
	if err != nil {
		t.Fatal(err)
	}
	if !applicable.Applicable || applicable.Ambiguities[0].ResolvedBy != "explicitGenesisBoundary" || applicable.GenesisCheckpoint.Coverage.StagingSHA != strings.Repeat("b", 40) {
		t.Fatalf("applicable bootstrap plan = %+v", applicable)
	}

	reordered := input
	reordered.SourceFacts = []BootstrapSourceFact{facts[1], facts[0]}
	reordered.Relationships[0].EvidenceFactIDs = []string{"pr-20"}
	again, err := PlanBootstrap(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if applicable.PlanID != again.PlanID {
		t.Fatalf("stable plan IDs differ: %s != %s", applicable.PlanID, again.PlanID)
	}
	encoded, err := json.Marshal(applicable)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"sourceFacts", "relationships", "ambiguities", "ownershipRecords", "stagedWork", "planId", "applicable"} {
		if !strings.Contains(string(encoded), `"`+wanted+`"`) {
			t.Fatalf("bootstrap JSON missing %q: %s", wanted, encoded)
		}
	}
}

func TestPromotionConsumptionPlansIssueDeliveryAndCursorCommit(t *testing.T) {
	store, revision := fixtureStore(t, adapterFixtureScenario{Name: "sealed", Lifecycle: "sealed"})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	input := PromotionConsumptionInput{
		Revision: revision, MergeRevision: strings.Repeat("a", 40), Method: PromotionUnknown,
		Writer: WriterProvenance{Workflow: "Work Item Transition", RunID: 99}, RecordedAt: "2026-07-14T03:00:00Z",
	}
	plan, err := PlanPromotionConsumption(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applicable || plan.Record == nil || plan.Checkpoint == nil || len(plan.ExpectedIssues) != 1 || plan.ExpectedIssues[0].Number != 7 {
		t.Fatalf("consumption plan = %+v", plan)
	}
	if plan.Record.PullRequest != revision.PullRequest || plan.Record.PromotionBaseSHA != revision.BaseSHA || plan.Record.StagingSHA != revision.HeadSHA || plan.Record.BaseSHA != input.MergeRevision {
		t.Fatalf("base integration identity = %+v", plan.Record)
	}
	if plan.Checkpoint.Checkpoint.Cursor.Position != snapshot.Checkpoint.Cursor.Position+1 || plan.Checkpoint.Checkpoint.Cursor.ConsumedPlanDigest != plan.PlanDigest {
		t.Fatalf("committed cursor = %+v", plan.Checkpoint.Checkpoint.Cursor)
	}
	commit, err := FinalizePromotionConsumption(plan, snapshot.Checkpoint, CommentIdentity{DatabaseID: 104, NodeID: "IC_104"})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Checkpoint.ActivePlan != nil || len(commit.Checkpoint.ActiveRecords) != 0 || commit.Checkpoint.BaseIntegration == nil || commit.Checkpoint.PromotionIntegration == nil || *commit.Checkpoint.PromotionIntegration != *commit.Checkpoint.BaseIntegration || commit.Checkpoint.CursorBoundary == nil {
		t.Fatalf("consumed checkpoint = %+v", commit.Checkpoint)
	}
}

func TestPromotionConsumptionCommittedDuplicateIsTotalNoOp(t *testing.T) {
	store, revision := fixtureStore(t, adapterFixtureScenario{
		Name: "consumed", Lifecycle: "consumed", Method: PromotionMerge, BaseResultSHA: strings.Repeat("a", 40),
	})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanPromotionConsumption(snapshot, PromotionConsumptionInput{
		Revision: revision, MergeRevision: strings.Repeat("a", 40), Method: PromotionUnknown,
		Writer: WriterProvenance{Workflow: "Work Item Transition", RunID: 100}, RecordedAt: "2026-07-14T04:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applicable || plan.Existing == nil || len(plan.ExpectedIssues) != 0 || plan.Record != nil || plan.Checkpoint != nil {
		t.Fatalf("duplicate consumption plan = %+v", plan)
	}
}

func TestPromotionConsumptionManualOnlyAndStaleSeal(t *testing.T) {
	store, revision := fixtureStore(t, adapterFixtureScenario{Name: "manual", Lifecycle: "sealed", ManualOnly: true})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	input := PromotionConsumptionInput{
		Revision: revision, MergeRevision: strings.Repeat("b", 40), Method: PromotionSquash,
		Writer: WriterProvenance{Workflow: "Work Item Transition", RunID: 99}, RecordedAt: "2026-07-14T03:00:00Z",
	}
	plan, err := PlanPromotionConsumption(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ExpectedIssues) != 0 || plan.Checkpoint.Checkpoint.CursorBoundary != nil || plan.Checkpoint.Checkpoint.Cursor.ThroughSequence != 0 {
		t.Fatalf("manual-only consumption plan = %+v", plan)
	}
	input.Revision.HeadSHA = strings.Repeat("c", 40)
	if _, err := PlanPromotionConsumption(snapshot, input); err == nil || !strings.Contains(err.Error(), "active seal") {
		t.Fatalf("stale seal error = %v", err)
	}
}

func TestPromotionConsumptionAdoptsExactOrphanRetry(t *testing.T) {
	store, revision := fixtureStore(t, adapterFixtureScenario{Name: "sealed", Lifecycle: "sealed"})
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	input := PromotionConsumptionInput{
		Revision: revision, MergeRevision: strings.Repeat("d", 40), Method: PromotionRebase,
		Writer: WriterProvenance{Workflow: "Work Item Transition", RunID: 99}, RecordedAt: "2026-07-14T03:00:00Z",
	}
	plan, err := PlanPromotionConsumption(snapshot, input)
	if err != nil {
		t.Fatal(err)
	}
	orphan := *plan.Record
	orphan.Writer.RunID = 100
	orphan.RecordedAt = "2026-07-14T03:01:00Z"
	orphan = finalizeBaseIntegration(orphan)
	if orphan.RetryID != plan.Record.RetryID || orphan.Digest == plan.Record.Digest {
		t.Fatalf("orphan retry identity did not preserve semantic identity")
	}
	match, err := FindBaseIntegrationRetry([]StoredComment{fixtureComment(104, "IC_104", renderBaseIntegration(orphan))}, orphan.RetryID)
	if err != nil || match == nil {
		t.Fatalf("retry match = %+v, err = %v", match, err)
	}
	adopted, err := AdoptBaseIntegrationRetry(plan, snapshot.Checkpoint, match.Record)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Record.Digest != orphan.Digest || adopted.Checkpoint.Checkpoint.Coverage.Writer.RunID != 100 {
		t.Fatalf("adopted plan = %+v", adopted)
	}
}

func plannerGenesis(t *testing.T) (DeliveryCheckpoint, DeliveryStoreLocator) {
	t.Helper()
	repository := RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}
	issue := ResourceIdentity{Number: 900, NodeID: "I_900"}
	checkpoint, err := NewGenesisCheckpoint(GenesisCheckpointInput{
		LedgerID: "ledger-1", Repository: repository, Issue: issue,
		StagingSHA: strings.Repeat("a", 40), Writer: fixtureWriter(), ObservedAt: "2026-07-14T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint, DeliveryStoreLocator{
		Repository: repository, Issue: issue,
		Checkpoint: CommentIdentity{DatabaseID: 100, NodeID: "IC_100"},
	}
}

func plannerStagedInput(pullRequest, issue int) StagedWorkAppendInput {
	return StagedWorkAppendInput{
		PullRequest:   ResourceIdentity{Number: pullRequest, NodeID: "PR_" + strings.Repeat("x", pullRequest)},
		StagingBranch: "agent", BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40), MergeRevision: strings.Repeat("3", 40),
		MergedAt: "2026-07-14T01:00:00Z", Writer: fixtureWriter(),
		Issues: []ManagedIssueReference{{Number: issue, NodeID: "I_" + strings.Repeat("x", issue), OwnershipDigest: fixtureDigest("ownership")}},
	}
}
