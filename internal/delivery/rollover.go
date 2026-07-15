package delivery

import (
	"errors"
	"fmt"
	"strings"
)

type RolloverInput struct {
	Issue      ResourceIdentity `json:"issue"`
	Writer     WriterProvenance `json:"writer"`
	ObservedAt string           `json:"observedAt"`
}

type RolloverPlan struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Kind          string                 `json:"kind"`
	Applicable    bool                   `json:"applicable"`
	Precondition  CheckpointPrecondition `json:"precondition"`
	Predecessor   CheckpointLink         `json:"predecessor"`
	Checkpoint    DeliveryCheckpoint     `json:"checkpoint"`
}

func PlanRollover(snapshot Snapshot, input RolloverInput) (RolloverPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return RolloverPlan{}, fmt.Errorf("rollover checkpoint: %w", err)
	}
	if checkpoint.Successor != nil {
		return RolloverPlan{}, errors.New("delivery checkpoint already has a successor")
	}
	if len(checkpoint.ActiveRecords) != 0 || checkpoint.ActivePlan != nil || checkpoint.PendingRechecks != nil {
		return RolloverPlan{}, errors.New("delivery rollover requires a drained active window and no pending work")
	}
	if err := validateResource(input.Issue, "successor ledger issue"); err != nil || input.Issue == checkpoint.Issue {
		return RolloverPlan{}, errors.New("successor ledger issue is invalid")
	}
	baseSHA := checkpoint.GenesisBaseSHA
	if checkpoint.BaseIntegration != nil {
		found := false
		for _, integration := range snapshot.BaseIntegrations {
			if integration.Digest == checkpoint.BaseIntegration.Digest {
				baseSHA = integration.BaseSHA
				found = true
				break
			}
		}
		if !found {
			return RolloverPlan{}, errors.New("delivery rollover base integration is missing from the snapshot")
		}
	}
	next, err := NewGenesisCheckpoint(GenesisCheckpointInput{
		LedgerID: checkpoint.LedgerID, Repository: checkpoint.Repository, Issue: input.Issue,
		StagingSHA: checkpoint.Coverage.StagingSHA, BaseSHA: baseSHA, Writer: input.Writer, ObservedAt: strings.TrimSpace(input.ObservedAt),
	})
	if err != nil {
		return RolloverPlan{}, err
	}
	next.Predecessor = &CheckpointLink{Locator: snapshot.Locator, Digest: checkpoint.Digest}
	next = finalizeCheckpoint(next)
	return RolloverPlan{
		SchemaVersion: SchemaVersion, Kind: "deliveryRolloverPlan", Applicable: true,
		Precondition: checkpointPrecondition(checkpoint), Predecessor: *next.Predecessor, Checkpoint: next,
	}, nil
}

func FinalizeRollover(snapshot Snapshot, plan RolloverPlan, successor DeliveryStoreLocator) (DeliveryCheckpoint, string, error) {
	if err := validateCheckpoint(snapshot.Checkpoint, snapshot.Locator); err != nil {
		return DeliveryCheckpoint{}, "", err
	}
	if checkpointPrecondition(snapshot.Checkpoint) != plan.Precondition {
		return DeliveryCheckpoint{}, "", errors.New("rollover checkpoint changed after planning")
	}
	expected, err := PlanRollover(snapshot, RolloverInput{
		Issue: plan.Checkpoint.Issue, Writer: plan.Checkpoint.Coverage.Writer, ObservedAt: plan.Checkpoint.Coverage.ObservedAt,
	})
	if err != nil {
		return DeliveryCheckpoint{}, "", err
	}
	if canonicalDigest(plan) != canonicalDigest(expected) {
		return DeliveryCheckpoint{}, "", errors.New("rollover plan does not match its checkpoint")
	}
	if successor.Repository != snapshot.Locator.Repository || successor.Issue != plan.Checkpoint.Issue {
		return DeliveryCheckpoint{}, "", errors.New("rollover successor locator does not match the plan")
	}
	link := CheckpointLink{Locator: successor, Digest: plan.Checkpoint.Digest}
	if err := validateCheckpointLink(link, snapshot.Checkpoint.Repository, snapshot.Checkpoint.Issue); err != nil {
		return DeliveryCheckpoint{}, "", err
	}
	next := snapshot.Checkpoint
	next.Generation++
	next.Successor = &link
	next = finalizeCheckpoint(next)
	body, err := RenderCheckpointIndex(snapshot.Locator, next)
	return next, body, err
}
