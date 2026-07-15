package task

import (
	"fmt"
	"strings"
)

const (
	LabelManaged    = "baton:managed"
	LabelInProgress = "baton:in-progress"

	BlockerNeedsInfo       = "needs-info"
	BlockerNeedsDiscussion = "needs:discussion"
)

var modeLabels = map[Mode]string{
	ModeTrivial:     "agent:ready-trivial",
	ModeBounded:     "agent:ready-bounded",
	ModeInvestigate: "agent:investigate-only",
}

var priorityLabels = map[Priority]string{
	PriorityP0: "priority:p0",
	PriorityP1: "priority:p1",
	PriorityP2: "priority:p2",
	PriorityP3: "priority:p3",
}

var fixedLabelDefinitions = map[string]LabelDefinition{
	LabelManaged:             {Name: LabelManaged, Color: "1d76db", Description: "Issue explicitly enrolled as a Baton task"},
	LabelInProgress:          {Name: LabelInProgress, Color: "fbca04", Description: "Advisory signal that work has started"},
	"agent:ready-trivial":    {Name: "agent:ready-trivial", Color: "0e8a16", Description: "Permits a small, obvious implementation"},
	"agent:ready-bounded":    {Name: "agent:ready-bounded", Color: "0e8a16", Description: "Permits a bounded implementation"},
	"agent:investigate-only": {Name: "agent:investigate-only", Color: "d4c5f9", Description: "Permits investigation but not implementation"},
	"priority:p0":            {Name: "priority:p0", Color: "b60205", Description: "Highest Baton task priority"},
	"priority:p1":            {Name: "priority:p1", Color: "d93f0b", Description: "High Baton task priority"},
	"priority:p2":            {Name: "priority:p2", Color: "fbca04", Description: "Normal Baton task priority"},
	"priority:p3":            {Name: "priority:p3", Color: "c5def5", Description: "Low Baton task priority"},
	BlockerNeedsInfo:         {Name: BlockerNeedsInfo, Color: "b60205", Description: "Task needs more information"},
	BlockerNeedsDiscussion:   {Name: BlockerNeedsDiscussion, Color: "b60205", Description: "Task needs discussion before action"},
}

var orderedModeLabels = []string{
	modeLabels[ModeTrivial],
	modeLabels[ModeBounded],
	modeLabels[ModeInvestigate],
}

var orderedPriorityLabels = []string{
	priorityLabels[PriorityP0],
	priorityLabels[PriorityP1],
	priorityLabels[PriorityP2],
	priorityLabels[PriorityP3],
}

var orderedBlockerLabels = []string{BlockerNeedsInfo, BlockerNeedsDiscussion}

func LabelForMode(mode Mode) (string, bool) {
	label, ok := modeLabels[mode]
	return label, ok
}

func LabelForPriority(priority Priority) (string, bool) {
	label, ok := priorityLabels[priority]
	return label, ok
}

func definitionForLabel(label string) (LabelDefinition, error) {
	definition, ok := fixedLabelDefinitions[strings.ToLower(label)]
	if !ok {
		return LabelDefinition{}, fmt.Errorf("label %q is not part of Baton's fixed vocabulary", label)
	}
	return definition, nil
}

func isBlocker(label string) bool {
	return strings.EqualFold(label, BlockerNeedsInfo) || strings.EqualFold(label, BlockerNeedsDiscussion)
}

func isFixedLabel(label string) bool {
	_, ok := fixedLabelDefinitions[strings.ToLower(label)]
	return ok
}

func canonicalLabelSet(labels []string) map[string]struct{} {
	result := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		result[strings.ToLower(strings.TrimSpace(label))] = struct{}{}
	}
	return result
}
