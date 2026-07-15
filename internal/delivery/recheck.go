package delivery

import (
	"errors"
	"fmt"
)

type PromotionRecheckClearPlan struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	Kind           string                    `json:"kind"`
	Applicable     bool                      `json:"applicable"`
	Locator        DeliveryStoreLocator      `json:"locator"`
	Precondition   CheckpointPrecondition    `json:"precondition"`
	Pending        *PendingPromotionRechecks `json:"pendingPromotionRechecks,omitempty"`
	Checkpoint     *DeliveryCheckpoint       `json:"checkpoint,omitempty"`
	CheckpointBody string                    `json:"checkpointBody,omitempty"`
}

func PlanPromotionRecheckClear(snapshot Snapshot) (PromotionRecheckClearPlan, error) {
	checkpoint := snapshot.Checkpoint
	if err := validateCheckpoint(checkpoint, snapshot.Locator); err != nil {
		return PromotionRecheckClearPlan{}, fmt.Errorf("promotion recheck clear checkpoint: %w", err)
	}
	plan := PromotionRecheckClearPlan{
		SchemaVersion: SchemaVersion, Kind: "promotionRecheckClearPlan", Applicable: checkpoint.PendingRechecks != nil,
		Locator: snapshot.Locator, Precondition: checkpointPrecondition(checkpoint),
	}
	if checkpoint.PendingRechecks == nil {
		return plan, nil
	}
	pending := *checkpoint.PendingRechecks
	pending.Targets = append([]PromotionRecheckTarget(nil), pending.Targets...)
	plan.Pending = &pending
	next := checkpoint
	next.Generation++
	next.PendingRechecks = nil
	next = finalizeCheckpoint(next)
	if next.Digest == checkpoint.Digest {
		return PromotionRecheckClearPlan{}, errors.New("promotion recheck clear did not change the checkpoint")
	}
	body, err := RenderCheckpointIndex(snapshot.Locator, next)
	if err != nil {
		return PromotionRecheckClearPlan{}, err
	}
	plan.Checkpoint, plan.CheckpointBody = &next, body
	return plan, nil
}
