package labels

type SyncPlan struct {
	SchemaVersion int           `json:"schemaVersion"`
	Kind          string        `json:"kind"`
	Repo          string        `json:"repo"`
	Changes       []LabelChange `json:"changes"`
}

type LabelChange struct {
	Name        string `json:"name"`
	Action      string `json:"action"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

func PlanSync(repo string, desired []Label, existing []Label) SyncPlan {
	existingByName := map[string]Label{}
	for _, label := range existing {
		existingByName[label.Name] = label
	}
	changes := make([]LabelChange, 0, len(desired))
	for _, label := range desired {
		existingLabel, exists := existingByName[label.Name]
		action := "ok"
		if !exists {
			action = "create"
		} else if NormalizeColor(existingLabel.Color) != NormalizeColor(label.Color) || existingLabel.Description != label.Description {
			action = "update"
		}
		changes = append(changes, LabelChange{
			Name:        label.Name,
			Action:      action,
			Color:       label.Color,
			Description: label.Description,
		})
	}
	return SyncPlan{SchemaVersion: 1, Kind: "labelSyncPlan", Repo: repo, Changes: changes}
}
