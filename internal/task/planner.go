package task

import (
	"fmt"
	"sort"
	"strings"
)

type MutationKind string

const (
	MutationEnroll   MutationKind = "enroll"
	MutationUpdate   MutationKind = "update"
	MutationUnenroll MutationKind = "unenroll"
	MutationStart    MutationKind = "start"
	MutationStop     MutationKind = "stop"
	MutationClose    MutationKind = "close"
)

type Mutation struct {
	Kind MutationKind

	ModeSet bool
	Mode    *Mode

	PrioritySet bool
	Priority    *Priority

	AddBlockers    []string
	RemoveBlockers []string
}

type ChangeAction string

const (
	ChangeCreateLabel ChangeAction = "create_label"
	ChangeAddLabel    ChangeAction = "add_label"
	ChangeRemoveLabel ChangeAction = "remove_label"
	ChangeCloseIssue  ChangeAction = "close_issue"
)

type Change struct {
	Action ChangeAction `json:"action"`
	Label  string       `json:"label,omitempty"`
}

type Plan struct {
	Changes   []Change
	Projected *Task
}

func PlanMutation(issue Issue, mutation Mutation) (Plan, error) {
	if err := validateMutation(mutation); err != nil {
		return Plan{}, err
	}
	labels := canonicalLabelSet(issue.Labels)
	managed := hasLabel(labels, LabelManaged)
	if requiresEnrollment(mutation.Kind) && !managed {
		_, err := Classify(issue)
		return Plan{}, err
	}
	if mutation.Kind == MutationStart && issue.State == IssueClosed {
		return Plan{}, &Error{
			Code:    "invalid_transition",
			Message: fmt.Sprintf("closed task #%d cannot be started", issue.Number),
			Hint:    "Choose an open task.",
		}
	}

	changes := []Change{}
	switch mutation.Kind {
	case MutationEnroll:
		appendAdd(&changes, labels, LabelManaged)
		appendFacetChanges(&changes, labels, orderedModeLabels, mutation.ModeSet, modeLabel(mutation.Mode))
		appendFacetChanges(&changes, labels, orderedPriorityLabels, mutation.PrioritySet, priorityLabel(mutation.Priority))
	case MutationUpdate:
		appendFacetChanges(&changes, labels, orderedModeLabels, mutation.ModeSet, modeLabel(mutation.Mode))
		appendFacetChanges(&changes, labels, orderedPriorityLabels, mutation.PrioritySet, priorityLabel(mutation.Priority))
		for _, blocker := range uniqueSorted(mutation.RemoveBlockers) {
			appendRemove(&changes, labels, blocker)
		}
		for _, blocker := range uniqueSorted(mutation.AddBlockers) {
			appendAdd(&changes, labels, blocker)
		}
	case MutationUnenroll:
		appendRemove(&changes, labels, LabelInProgress)
		appendRemove(&changes, labels, LabelManaged)
	case MutationStart:
		appendAdd(&changes, labels, LabelInProgress)
	case MutationStop:
		appendRemove(&changes, labels, LabelInProgress)
	case MutationClose:
		appendRemove(&changes, labels, LabelInProgress)
		if issue.State != IssueClosed {
			changes = append(changes, Change{Action: ChangeCloseIssue})
		}
	default:
		return Plan{}, fmt.Errorf("unknown Task mutation %q", mutation.Kind)
	}

	projectedIssue := applyChanges(issue, changes)
	var projected *Task
	if hasLabel(canonicalLabelSet(projectedIssue.Labels), LabelManaged) {
		value, err := Classify(projectedIssue)
		if err != nil {
			return Plan{}, err
		}
		projected = &value
	}
	return Plan{Changes: changes, Projected: projected}, nil
}

func validateMutation(mutation Mutation) error {
	switch mutation.Kind {
	case MutationEnroll:
		if len(mutation.AddBlockers) > 0 || len(mutation.RemoveBlockers) > 0 {
			return fmt.Errorf("enroll accepts only mode and priority changes")
		}
	case MutationUpdate:
	case MutationUnenroll, MutationStart, MutationStop, MutationClose:
		if mutation.ModeSet || mutation.Mode != nil || mutation.PrioritySet || mutation.Priority != nil || len(mutation.AddBlockers) > 0 || len(mutation.RemoveBlockers) > 0 {
			return fmt.Errorf("%s does not accept classification changes", mutation.Kind)
		}
	default:
		return fmt.Errorf("unknown Task mutation %q", mutation.Kind)
	}
	if !mutation.ModeSet && mutation.Mode != nil {
		return fmt.Errorf("mode value requires ModeSet")
	}
	if !mutation.PrioritySet && mutation.Priority != nil {
		return fmt.Errorf("priority value requires PrioritySet")
	}
	if mutation.ModeSet && mutation.Mode != nil {
		if _, ok := LabelForMode(*mutation.Mode); !ok {
			return fmt.Errorf("invalid mode %q", *mutation.Mode)
		}
	}
	if mutation.PrioritySet && mutation.Priority != nil {
		if _, ok := LabelForPriority(*mutation.Priority); !ok {
			return fmt.Errorf("invalid priority %q", *mutation.Priority)
		}
	}
	if mutation.Kind == MutationUpdate && !mutation.ModeSet && !mutation.PrioritySet && len(mutation.AddBlockers) == 0 && len(mutation.RemoveBlockers) == 0 {
		return fmt.Errorf("update requires at least one classification change")
	}
	added := map[string]struct{}{}
	for _, blocker := range mutation.AddBlockers {
		if !isBlocker(blocker) {
			return fmt.Errorf("invalid blocker %q", blocker)
		}
		added[strings.ToLower(blocker)] = struct{}{}
	}
	for _, blocker := range mutation.RemoveBlockers {
		if !isBlocker(blocker) {
			return fmt.Errorf("invalid blocker %q", blocker)
		}
		if _, conflict := added[strings.ToLower(blocker)]; conflict {
			return fmt.Errorf("blocker %q cannot be added and removed together", blocker)
		}
	}
	return nil
}

func requiresEnrollment(kind MutationKind) bool {
	switch kind {
	case MutationUpdate, MutationStart, MutationStop, MutationClose:
		return true
	default:
		return false
	}
}

func modeLabel(mode *Mode) string {
	if mode == nil {
		return ""
	}
	return modeLabels[*mode]
}

func priorityLabel(priority *Priority) string {
	if priority == nil {
		return ""
	}
	return priorityLabels[*priority]
}

func appendFacetChanges(changes *[]Change, labels map[string]struct{}, facet []string, set bool, desired string) {
	if !set {
		return
	}
	// Establish the replacement before cleanup. A failed add can leave a
	// temporary conflict, but it cannot destroy the prior valid classification.
	if desired != "" {
		appendAdd(changes, labels, desired)
	}
	for _, label := range facet {
		if label != desired {
			appendRemove(changes, labels, label)
		}
	}
}

func appendAdd(changes *[]Change, labels map[string]struct{}, label string) {
	if hasLabel(labels, label) {
		return
	}
	*changes = append(*changes, Change{Action: ChangeAddLabel, Label: label})
	labels[label] = struct{}{}
}

func appendRemove(changes *[]Change, labels map[string]struct{}, label string) {
	if !hasLabel(labels, label) {
		return
	}
	*changes = append(*changes, Change{Action: ChangeRemoveLabel, Label: label})
	delete(labels, label)
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		set[strings.ToLower(value)] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func applyChanges(issue Issue, changes []Change) Issue {
	labels := canonicalLabelSet(issue.Labels)
	for _, change := range changes {
		switch change.Action {
		case ChangeAddLabel:
			labels[change.Label] = struct{}{}
		case ChangeRemoveLabel:
			delete(labels, change.Label)
		case ChangeCloseIssue:
			issue.State = IssueClosed
		}
	}
	issue.Labels = make([]string, 0, len(labels))
	for label := range labels {
		issue.Labels = append(issue.Labels, label)
	}
	sort.Strings(issue.Labels)
	return issue
}
