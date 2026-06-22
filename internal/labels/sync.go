package labels

type SyncPlan struct {
	SchemaVersion int           `json:"schemaVersion"`
	Kind          string        `json:"kind"`
	Repo          string        `json:"repo"`
	Count         int           `json:"count"`
	Counts        SyncCounts    `json:"counts"`
	Changes       []LabelChange `json:"changes"`
	Help          []string      `json:"help,omitempty"`
}

type SyncCounts struct {
	OK     int `json:"ok"`
	Create int `json:"create"`
	Update int `json:"update"`
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
	return SyncPlan{SchemaVersion: 1, Kind: "labelSyncPlan", Repo: repo, Count: len(changes), Counts: countChanges(changes), Changes: changes, Help: syncHelp(changes)}
}

func countChanges(changes []LabelChange) SyncCounts {
	counts := SyncCounts{}
	for _, change := range changes {
		switch change.Action {
		case "ok":
			counts.OK++
		case "create":
			counts.Create++
		case "update":
			counts.Update++
		}
	}
	return counts
}

func syncHelp(changes []LabelChange) []string {
	counts := countChanges(changes)
	if counts.Create == 0 && counts.Update == 0 {
		return []string{"No label changes needed."}
	}
	return []string{"Run `baton sync-labels --apply --json` to apply planned label changes."}
}
