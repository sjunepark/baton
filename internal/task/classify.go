package task

import (
	"fmt"
	"sort"
	"strings"
)

type Error struct {
	Code    string
	Message string
	Hint    string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func Classify(issue Issue) (Task, error) {
	labels := canonicalLabelSet(issue.Labels)
	if _, managed := labels[LabelManaged]; !managed {
		return Task{}, &Error{
			Code:    "not_managed",
			Message: fmt.Sprintf("issue #%d is not managed by Baton", issue.Number),
			Hint:    fmt.Sprintf("Run baton enroll %d to enroll it.", issue.Number),
		}
	}

	mode, modeCount := classifiedMode(labels)
	priority, priorityCount := classifiedPriority(labels)
	if priorityCount == 0 {
		value := PriorityP2
		priority = &value
	}

	blockers := make([]string, 0, len(orderedBlockerLabels))
	for _, blocker := range orderedBlockerLabels {
		if _, ok := labels[blocker]; ok {
			blockers = append(blockers, blocker)
		}
	}
	reasons := make([]string, 0, len(blockers)+2)
	for _, blocker := range blockers {
		reasons = append(reasons, "blocker:"+blocker)
	}
	switch modeCount {
	case 0:
		reasons = append(reasons, "missing_mode")
	case 1:
	default:
		reasons = append(reasons, "conflicting_modes")
	}
	if priorityCount > 1 {
		reasons = append(reasons, "conflicting_priorities")
	}
	sort.Strings(reasons)

	projectLabels := []string{}
	for _, label := range issue.Labels {
		if !isFixedLabel(label) {
			projectLabels = append(projectLabels, label)
		}
	}
	sort.Strings(projectLabels)

	inProgress := hasLabel(labels, LabelInProgress)
	state := StateReady
	switch {
	case issue.State == IssueClosed:
		state = StateDone
	case len(reasons) > 0:
		state = StateBlocked
	case inProgress:
		state = StateInProgress
	}

	return Task{
		Number: issue.Number, Title: issue.Title, URL: issue.URL,
		IssueState: issue.State, State: state, Mode: mode, Priority: priority,
		InProgress: inProgress, Blockers: blockers, ProjectLabels: projectLabels,
		Reasons: reasons,
	}, nil
}

func classifiedMode(labels map[string]struct{}) (*Mode, int) {
	var result *Mode
	count := 0
	for _, mode := range []Mode{ModeTrivial, ModeBounded, ModeInvestigate} {
		if hasLabel(labels, modeLabels[mode]) {
			value := mode
			result = &value
			count++
		}
	}
	if count != 1 {
		return nil, count
	}
	return result, count
}

func classifiedPriority(labels map[string]struct{}) (*Priority, int) {
	var result *Priority
	count := 0
	for _, priority := range []Priority{PriorityP0, PriorityP1, PriorityP2, PriorityP3} {
		if hasLabel(labels, priorityLabels[priority]) {
			value := priority
			result = &value
			count++
		}
	}
	if count > 1 {
		return nil, count
	}
	return result, count
}

func hasLabel(labels map[string]struct{}, label string) bool {
	_, ok := labels[strings.ToLower(label)]
	return ok
}

func priorityRank(priority *Priority) int {
	if priority == nil {
		return 4
	}
	switch *priority {
	case PriorityP0:
		return 0
	case PriorityP1:
		return 1
	case PriorityP2:
		return 2
	case PriorityP3:
		return 3
	default:
		return 4
	}
}
