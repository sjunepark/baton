package delivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type adapterFixtureManifest struct {
	Scenarios []adapterFixtureScenario `json:"scenarios"`
}

type adapterFixtureScenario struct {
	Name          string          `json:"name"`
	Lifecycle     string          `json:"lifecycle"`
	Method        PromotionMethod `json:"method"`
	BaseResultSHA string          `json:"baseResultSha"`
	ManualOnly    bool            `json:"manualOnly"`
	Exclusion     bool            `json:"exclusion"`
	CoverageGap   bool            `json:"coverageGap"`
	Mutation      string          `json:"mutation"`
	WantError     string          `json:"wantError"`
}

func TestDeliveryStorageAdapterFixtures(t *testing.T) {
	manifest := loadAdapterFixtureManifest(t)
	var consumedCursor *PromotionCursor
	methods := map[PromotionMethod]string{}
	for _, scenario := range manifest.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			store, revision := fixtureStore(t, scenario)
			snapshot, err := ParseStoreSnapshot(store)
			if err == nil && scenario.Lifecycle == "sealed" {
				err = ValidateSealedPlan(snapshot, revision)
			}
			if scenario.WantError != "" {
				if err == nil || !strings.Contains(err.Error(), scenario.WantError) {
					t.Fatalf("error = %v, want substring %q", err, scenario.WantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario.Lifecycle != "consumed" {
				return
			}
			if len(snapshot.BaseIntegrations) != 1 {
				t.Fatalf("base integrations = %d, want 1", len(snapshot.BaseIntegrations))
			}
			integration := snapshot.BaseIntegrations[0]
			if integration.Method != scenario.Method || integration.BaseSHA != scenario.BaseResultSHA {
				t.Fatalf("integration = %+v, want method %q and base %q", integration, scenario.Method, scenario.BaseResultSHA)
			}
			methods[scenario.Method] = integration.BaseSHA
			cursor := snapshot.Checkpoint.Cursor
			if consumedCursor == nil {
				consumedCursor = &cursor
			} else if cursor != *consumedCursor {
				t.Fatalf("promotion cursor changed with merge method:\ngot  %+v\nwant %+v", cursor, *consumedCursor)
			}
		})
	}
	if len(methods) != 3 || methods[PromotionMerge] == methods[PromotionSquash] || methods[PromotionMerge] == methods[PromotionRebase] || methods[PromotionSquash] == methods[PromotionRebase] {
		t.Fatalf("promotion integration fixtures = %#v, want distinct merge/squash/rebase results", methods)
	}
}

func TestRawPromotionMergeStoreFixture(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "delivery", "raw-promotion-merge.json"))
	if err != nil {
		t.Fatal(err)
	}
	var store StoreSnapshot
	if err := json.Unmarshal(content, &store); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseStoreSnapshot(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.BaseIntegrations) != 1 || snapshot.BaseIntegrations[0].Method != PromotionMerge {
		t.Fatalf("raw promotion integration = %+v", snapshot.BaseIntegrations)
	}
}

func TestResolvedPromotionRejectsConflictingIssueIdentity(t *testing.T) {
	first := RecordReference{Kind: RecordStagedWork, Sequence: 1, Digest: fixtureDigest("work-1")}
	second := RecordReference{Kind: RecordStagedWork, Sequence: 2, Digest: fixtureDigest("work-2")}
	plan := RecordReference{Kind: RecordPromotionPlan, Sequence: 3, Digest: fixtureDigest("plan")}
	snapshot := Snapshot{
		Checkpoint: DeliveryCheckpoint{ActiveRecords: []RecordReference{first, second, plan}, ActivePlan: &plan},
		StagedWork: []StagedWorkRecord{
			{RecordHeader: RecordHeader{Digest: first.Digest}, PullRequest: ResourceIdentity{Number: 20}, Issues: []ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: fixtureDigest("ownership-7")}}},
			{RecordHeader: RecordHeader{Digest: second.Digest}, PullRequest: ResourceIdentity{Number: 21}, Issues: []ManagedIssueReference{{Number: 7, NodeID: "I_other", OwnershipDigest: fixtureDigest("ownership-other")}}},
		},
	}
	if _, err := resolvedPromotion(snapshot, PromotionPlanRecord{}); err == nil || !strings.Contains(err.Error(), "conflicting managed identities") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseStoreSnapshotFailsClosedOnTransportAndMarkerProblems(t *testing.T) {
	valid, _ := fixtureStore(t, adapterFixtureScenario{Name: "valid", Lifecycle: "sealed"})
	tests := []struct {
		name      string
		mutate    func(*StoreSnapshot)
		wantError string
	}{
		{name: "incomplete acquisition", mutate: func(store *StoreSnapshot) { store.Complete = false }, wantError: "acquisition is incomplete"},
		{name: "untrusted checkpoint", mutate: func(store *StoreSnapshot) { store.Checkpoint.AuthorLogin = "contributor" }, wantError: "not trusted"},
		{name: "unpinned checkpoint", mutate: func(store *StoreSnapshot) { store.Checkpoint.Comment.NodeID = "IC_other" }, wantError: "pinned locator"},
		{name: "multiple markers", mutate: func(store *StoreSnapshot) { store.Records[0].Body += "\n" + store.Records[0].Body }, wantError: "exactly one"},
		{name: "unknown field", mutate: func(store *StoreSnapshot) {
			store.Records[0].Body = strings.Replace(store.Records[0].Body, `"issues":`, `"unknown":true,"issues":`, 1)
		}, wantError: "unknown field"},
		{name: "record node identity mismatch", mutate: func(store *StoreSnapshot) {
			checkpoint := parseCheckpointForTest(t, store.Checkpoint.Body)
			checkpoint.ActiveRecords[0].Comment.NodeID = "IC_stale"
			checkpoint = finalizeCheckpoint(checkpoint)
			store.Checkpoint.Body = renderCheckpoint(checkpoint)
		}, wantError: "does not match its checkpoint reference"},
		{name: "noncanonical json", mutate: func(store *StoreSnapshot) {
			store.Records[0].Body = strings.Replace(store.Records[0].Body, "{", "{ ", 1)
		}, wantError: "canonical form"},
		{name: "active cap", mutate: func(store *StoreSnapshot) {
			checkpoint := parseCheckpointForTest(t, store.Checkpoint.Body)
			for len(checkpoint.ActiveRecords) <= MaxActiveRecords {
				checkpoint.ActiveRecords = append(checkpoint.ActiveRecords, checkpoint.ActiveRecords[0])
			}
			checkpoint = finalizeCheckpoint(checkpoint)
			store.Checkpoint.Body = renderCheckpoint(checkpoint)
		}, wantError: "cap exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := cloneStore(t, valid)
			test.mutate(&store)
			_, err := ParseStoreSnapshot(store)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestSynchronizationIntegrationContractKeepsCursorStable(t *testing.T) {
	repository := RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}
	cursor := finalizeCursor(PromotionCursor{ThroughDigest: genesisDigest("ledger-1", repository.NodeID)})
	record := finalizeBaseIntegration(BaseIntegrationRecord{
		RecordHeader: RecordHeader{LedgerID: "ledger-1", Repository: repository, Sequence: 1, PreviousDigest: genesisDigest("ledger-1", repository.NodeID), Writer: fixtureWriter()},
		Source:       IntegrationSynchronization, Method: PromotionSync, PullRequest: ResourceIdentity{Number: 42, NodeID: "PR_42"},
		PriorStagingSHA: strings.Repeat("c", 40), BaseSHA: strings.Repeat("a", 40), StagingSHA: strings.Repeat("b", 40),
		PriorCursorDigest: cursor.Digest, CommittedCursorDigest: cursor.Digest, RecordedAt: "2026-07-14T03:00:00Z",
	})
	comment := fixtureComment(401, "IC_401", renderBaseIntegration(record))
	parsed, err := parseRecord(comment)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.value.(BaseIntegrationRecord)
	if got.Source != IntegrationSynchronization || got.PriorCursorDigest != got.CommittedCursorDigest {
		t.Fatalf("synchronization integration = %+v", got)
	}
}

func TestCoverageCannotPrecedeRetainedRecordWindow(t *testing.T) {
	store, _ := fixtureStore(t, adapterFixtureScenario{
		Name: "consumed", Lifecycle: "consumed", Method: PromotionMerge,
		BaseResultSHA: strings.Repeat("a", 40),
	})
	checkpoint := parseCheckpointForTest(t, store.Checkpoint.Body)
	checkpoint.Coverage.RecordSequence = checkpoint.CursorBoundary.Sequence
	checkpoint.Coverage.RecordDigest = checkpoint.CursorBoundary.Digest
	checkpoint.Coverage = finalizeCoverage(checkpoint.Coverage)
	checkpoint = finalizeCheckpoint(checkpoint)
	store.Checkpoint.Body = renderCheckpoint(checkpoint)
	_, err := ParseStoreSnapshot(store)
	if err == nil || !strings.Contains(err.Error(), "outside the verifiable delivery record window") {
		t.Fatalf("error = %v, want below-window coverage failure", err)
	}
}

func TestEvidenceURLRequiresExactRepositoryResource(t *testing.T) {
	repository := RepositoryIdentity{Host: "github.example.com", FullName: "example/repo", NodeID: "R_1"}
	tests := []struct {
		name     string
		raw      string
		path     string
		fragment string
	}{
		{name: "wrong host", raw: "https://evil.example/example/repo/pull/50#pullrequestreview-8000", path: "/example/repo/pull/50", fragment: "pullrequestreview-8000"},
		{name: "path prefix", raw: "https://github.example.com/example/repo/pull/500#pullrequestreview-8000", path: "/example/repo/pull/50", fragment: "pullrequestreview-8000"},
		{name: "fragment prefix", raw: "https://github.example.com/example/repo/pull/50#pullrequestreview-18000", path: "/example/repo/pull/50", fragment: "pullrequestreview-8000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := evidenceURL(test.raw, repository, test.path, test.fragment); err == nil {
				t.Fatalf("evidence URL %q unexpectedly passed", test.raw)
			}
		})
	}
}

func TestRecordRetryIdentityIgnoresWriterAttemptMetadata(t *testing.T) {
	repository := RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}
	base := PromotionPlanRecord{
		RecordHeader: RecordHeader{LedgerID: "ledger-1", Repository: repository, Sequence: 1, PreviousDigest: genesisDigest("ledger-1", repository.NodeID), Writer: WriterProvenance{Workflow: "baton-delivery", RunID: 1}},
		PullRequest:  ResourceIdentity{Number: 50, NodeID: "PR_50"}, BaseSHA: strings.Repeat("1", 40), HeadSHA: strings.Repeat("2", 40),
		CursorDigest: fixtureDigest("cursor"), SealedAt: "2026-07-14T01:00:00Z",
	}
	first := finalizePromotionPlan(base)
	base.Writer.RunID = 2
	base.Sequence = 9
	base.PreviousDigest = fixtureDigest("later-chain-head")
	base.SealedAt = "2026-07-14T02:00:00Z"
	second := finalizePromotionPlan(base)
	if first.RetryID != second.RetryID {
		t.Fatalf("retry identity changed across writer attempts: %q != %q", first.RetryID, second.RetryID)
	}
	if first.Digest == second.Digest {
		t.Fatal("record digest did not retain writer provenance and seal time")
	}
}

func fixtureStore(t *testing.T, scenario adapterFixtureScenario) (StoreSnapshot, PromotionRevision) {
	t.Helper()
	repository := RepositoryIdentity{Host: "github.com", FullName: "example/repo", NodeID: "R_1"}
	issue := ResourceIdentity{Number: 900, NodeID: "I_900"}
	checkpointComment := CommentIdentity{DatabaseID: 100, NodeID: "IC_100"}
	locator := DeliveryStoreLocator{Repository: repository, Issue: issue, Checkpoint: checkpointComment}
	ledgerID := "ledger-1"
	genesisDigest := genesisDigest(ledgerID, repository.NodeID)
	genesisCursor := finalizeCursor(PromotionCursor{ThroughDigest: genesisDigest})
	promotionPR := ResourceIdentity{Number: 50, NodeID: "PR_50"}
	promotionBase := strings.Repeat("1", 40)
	promotionHead := strings.Repeat("2", 40)
	revision := PromotionRevision{RepositoryNodeID: repository.NodeID, PullRequest: promotionPR, BaseSHA: promotionBase, HeadSHA: promotionHead}

	active, comments := []RecordReference{}, []StoredComment{}
	sequence, previous := uint64(0), genesisDigest
	var stagedReference RecordReference
	if !scenario.ManualOnly {
		sequence++
		staged := finalizeStagedWork(StagedWorkRecord{
			RecordHeader: RecordHeader{LedgerID: ledgerID, Repository: repository, Sequence: sequence, PreviousDigest: previous, Writer: fixtureWriter()},
			PullRequest:  ResourceIdentity{Number: 20, NodeID: "PR_20"}, StagingBranch: "agent",
			BaseSHA: strings.Repeat("3", 40), HeadSHA: strings.Repeat("4", 40), MergeRevision: strings.Repeat("5", 40),
			MergedAt: "2026-07-14T01:00:00Z", ObservedCursorDigest: genesisCursor.Digest,
			Issues: []ManagedIssueReference{{Number: 7, NodeID: "I_7", OwnershipDigest: fixtureDigest("issue-7")}},
		})
		identity := CommentIdentity{DatabaseID: 101, NodeID: "IC_101"}
		stagedReference = recordReference(identity, staged.RecordHeader)
		active = append(active, stagedReference)
		comments = append(comments, fixtureComment(identity.DatabaseID, identity.NodeID, renderStagedWork(staged)))
		previous = staged.Digest
	}
	coverageSHA := promotionHead
	if scenario.CoverageGap {
		coverageSHA = strings.Repeat("8", 40)
	}
	coverage := finalizeCoverage(StagingCoverage{
		Repository: repository, StagingSHA: coverageSHA, RecordSequence: sequence, RecordDigest: previous,
		CursorDigest: genesisCursor.Digest, Writer: fixtureWriter(), ObservedAt: "2026-07-14T01:30:00Z",
	})

	exclusions := []RecordReference{}
	if scenario.Exclusion {
		sequence++
		exclusion := finalizeExclusion(ExclusionRecord{
			RecordHeader: RecordHeader{LedgerID: ledgerID, Repository: repository, Sequence: sequence, PreviousDigest: previous, Writer: fixtureWriter()},
			StagedRecord: stagedReference, PullRequest: promotionPR, HeadSHA: promotionHead, CursorDigest: genesisCursor.Digest,
			Reason: "The staged implementation was explicitly reverted.", RequestedBy: "maintainer", ApprovedBy: "reviewer",
			RequestRunID: 7000, RequestURL: "https://github.com/example/repo/actions/runs/7000", RequestedAt: "2026-07-14T01:40:00Z",
			ReviewID: 8000, ReviewNodeID: "PRR_8000", ReviewURL: "https://github.com/example/repo/pull/50#pullrequestreview-8000", ReviewedAt: "2026-07-14T01:50:00Z", ReviewHeadSHA: promotionHead,
		})
		identity := CommentIdentity{DatabaseID: 102, NodeID: "IC_102"}
		reference := recordReference(identity, exclusion.RecordHeader)
		exclusions = append(exclusions, reference)
		active = append(active, reference)
		comments = append(comments, fixtureComment(identity.DatabaseID, identity.NodeID, renderExclusion(exclusion)))
		previous = exclusion.Digest
	}

	sequence++
	included := []RecordReference{}
	if !scenario.ManualOnly && !scenario.Exclusion {
		included = append(included, stagedReference)
	}
	plan := finalizePromotionPlan(PromotionPlanRecord{
		RecordHeader: RecordHeader{LedgerID: ledgerID, Repository: repository, Sequence: sequence, PreviousDigest: previous, Writer: fixtureWriter()},
		PullRequest:  promotionPR, BaseSHA: promotionBase, HeadSHA: promotionHead, CursorDigest: genesisCursor.Digest,
		CoverageDigest: coverage.Digest,
		Included:       included, Exclusions: exclusions, SealedAt: "2026-07-14T02:00:00Z",
	})
	planIdentity := CommentIdentity{DatabaseID: 103, NodeID: "IC_103"}
	planReference := recordReference(planIdentity, plan.RecordHeader)
	active = append(active, planReference)
	comments = append(comments, fixtureComment(planIdentity.DatabaseID, planIdentity.NodeID, renderPromotionPlan(plan)))

	if scenario.Lifecycle == "consumed" {
		throughSequence, throughDigest := uint64(0), genesisDigest
		if !scenario.ManualOnly {
			throughSequence, throughDigest = stagedReference.Sequence, stagedReference.Digest
		}
		cursor := finalizeCursor(PromotionCursor{Position: 1, ThroughSequence: throughSequence, ThroughDigest: throughDigest, ConsumedPlanDigest: plan.Digest})
		sequence++
		integration := finalizeBaseIntegration(BaseIntegrationRecord{
			RecordHeader: RecordHeader{LedgerID: ledgerID, Repository: repository, Sequence: sequence, PreviousDigest: plan.Digest, Writer: fixtureWriter()},
			Source:       IntegrationPromotion, Method: scenario.Method, PullRequest: promotionPR, PromotionBaseSHA: promotionBase, BaseSHA: scenario.BaseResultSHA,
			StagingSHA: promotionHead, PriorCursorDigest: genesisCursor.Digest, CommittedCursorDigest: cursor.Digest,
			PlanDigest: plan.Digest, RecordedAt: "2026-07-14T03:00:00Z",
		})
		integrationIdentity := CommentIdentity{DatabaseID: 104, NodeID: "IC_104"}
		integrationReference := recordReference(integrationIdentity, integration.RecordHeader)
		consumedCoverage := finalizeCoverage(StagingCoverage{
			Repository: repository, StagingSHA: promotionHead, RecordSequence: integration.Sequence, RecordDigest: integration.Digest,
			CursorDigest: cursor.Digest, Writer: fixtureWriter(), ObservedAt: "2026-07-14T03:00:00Z",
		})
		var cursorBoundary *RecordReference
		consumedRecords := []StoredComment{}
		if !scenario.ManualOnly {
			cursorBoundary = &stagedReference
			consumedRecords = append(consumedRecords, comments[0])
		}
		consumedRecords = append(consumedRecords, fixtureComment(integrationIdentity.DatabaseID, integrationIdentity.NodeID, renderBaseIntegration(integration)))
		checkpoint := finalizeCheckpoint(DeliveryCheckpoint{
			LedgerID: ledgerID, Repository: repository, Issue: issue, Generation: 2,
			WindowSequence: integration.Sequence, WindowDigest: integration.Digest,
			HeadSequence: integration.Sequence, HeadDigest: integration.Digest, Cursor: cursor, Coverage: consumedCoverage,
			CursorBoundary: cursorBoundary, ActiveRecords: []RecordReference{}, BaseIntegration: &integrationReference,
		})
		return StoreSnapshot{
			Locator: locator, Checkpoint: fixtureComment(checkpointComment.DatabaseID, checkpointComment.NodeID, renderCheckpoint(checkpoint)),
			Records: consumedRecords, Complete: true,
		}, revision
	}

	checkpoint := finalizeCheckpoint(DeliveryCheckpoint{
		LedgerID: ledgerID, Repository: repository, Issue: issue, Generation: 1,
		WindowDigest: genesisDigest, HeadSequence: plan.Sequence, HeadDigest: plan.Digest, Cursor: genesisCursor, Coverage: coverage,
		ActiveRecords: active, ActivePlan: &planReference,
	})
	store := StoreSnapshot{
		Locator: locator, Checkpoint: fixtureComment(checkpointComment.DatabaseID, checkpointComment.NodeID, renderCheckpoint(checkpoint)),
		Records: comments, Complete: true,
	}
	switch scenario.Mutation {
	case "duplicate":
		duplicate := store.Records[0]
		duplicate.Comment = CommentIdentity{DatabaseID: 999, NodeID: "IC_999"}
		store.Records = append(store.Records, duplicate)
	case "edit":
		store.Records[0].Body = strings.Replace(store.Records[0].Body, strings.Repeat("4", 40), strings.Repeat("6", 40), 1)
	case "delete":
		store.Records = store.Records[1:]
	case "staleHead":
		revision.HeadSHA = strings.Repeat("9", 40)
	case "postSealExclusion":
		postSeal := finalizeExclusion(ExclusionRecord{
			RecordHeader: RecordHeader{LedgerID: ledgerID, Repository: repository, Sequence: plan.Sequence + 1, PreviousDigest: plan.Digest, Writer: fixtureWriter()},
			StagedRecord: stagedReference, PullRequest: promotionPR, HeadSHA: promotionHead, CursorDigest: genesisCursor.Digest,
			Reason: "Exclude after the prior seal.", RequestedBy: "maintainer", ApprovedBy: "reviewer",
			RequestRunID: 7100, RequestURL: "https://github.com/example/repo/actions/runs/7100", RequestedAt: "2026-07-14T02:10:00Z",
			ReviewID: 8100, ReviewNodeID: "PRR_8100", ReviewURL: "https://github.com/example/repo/pull/50#pullrequestreview-8100", ReviewedAt: "2026-07-14T02:20:00Z", ReviewHeadSHA: promotionHead,
		})
		identity := CommentIdentity{DatabaseID: 105, NodeID: "IC_105"}
		reference := recordReference(identity, postSeal.RecordHeader)
		store.Records = append(store.Records, fixtureComment(identity.DatabaseID, identity.NodeID, renderExclusion(postSeal)))
		checkpoint.ActiveRecords = append(checkpoint.ActiveRecords, reference)
		checkpoint.HeadSequence, checkpoint.HeadDigest = postSeal.Sequence, postSeal.Digest
		checkpoint = finalizeCheckpoint(checkpoint)
		store.Checkpoint.Body = renderCheckpoint(checkpoint)
	}
	return store, revision
}

func loadAdapterFixtureManifest(t *testing.T) adapterFixtureManifest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "delivery", "adapter-scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest adapterFixtureManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Scenarios) != 11 {
		t.Fatalf("adapter fixture scenarios = %d, want 11", len(manifest.Scenarios))
	}
	return manifest
}

func fixtureWriter() WriterProvenance {
	return WriterProvenance{Workflow: "baton-delivery", RunID: 7001}
}

func fixtureComment(databaseID int64, nodeID, body string) StoredComment {
	return StoredComment{Comment: CommentIdentity{DatabaseID: databaseID, NodeID: nodeID}, Body: body, AuthorLogin: TrustedAuthorLogin, AuthorType: TrustedAuthorType}
}

func fixtureDigest(value string) string {
	return digestBytes([]byte(value))
}

func parseCheckpointForTest(t *testing.T, body string) DeliveryCheckpoint {
	t.Helper()
	payload, err := extractMarker(body, CheckpointMarkerV1)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint DeliveryCheckpoint
	if err := strictUnmarshal(payload, &checkpoint); err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func cloneStore(t *testing.T, store StoreSnapshot) StoreSnapshot {
	t.Helper()
	content, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	var clone StoreSnapshot
	if err := json.Unmarshal(content, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
