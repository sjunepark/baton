package task_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/sjunepark/baton/internal/task"
)

func TestClassifyEveryFixedLabelCombination(t *testing.T) {
	t.Parallel()
	modes := []struct {
		label string
		value task.Mode
	}{
		{"agent:ready-trivial", task.ModeTrivial},
		{"agent:ready-bounded", task.ModeBounded},
		{"agent:investigate-only", task.ModeInvestigate},
	}
	priorities := []struct {
		label string
		value task.Priority
	}{
		{"priority:p0", task.PriorityP0},
		{"priority:p1", task.PriorityP1},
		{"priority:p2", task.PriorityP2},
		{"priority:p3", task.PriorityP3},
	}
	blockers := []string{task.BlockerNeedsInfo, task.BlockerNeedsDiscussion}

	for modeMask := 0; modeMask < 1<<len(modes); modeMask++ {
		for priorityMask := 0; priorityMask < 1<<len(priorities); priorityMask++ {
			for blockerMask := 0; blockerMask < 1<<len(blockers); blockerMask++ {
				for _, inProgress := range []bool{false, true} {
					for _, issueState := range []task.IssueState{task.IssueOpen, task.IssueClosed} {
						name := fmt.Sprintf("m%d/p%d/b%d/progress=%t/%s", modeMask, priorityMask, blockerMask, inProgress, issueState)
						t.Run(name, func(t *testing.T) {
							labels := []string{task.LabelManaged, "project:kept"}
							var wantMode *task.Mode
							modeCount := 0
							for index, candidate := range modes {
								if modeMask&(1<<index) != 0 {
									labels = append(labels, candidate.label)
									value := candidate.value
									wantMode = &value
									modeCount++
								}
							}
							if modeCount != 1 {
								wantMode = nil
							}

							var wantPriority *task.Priority
							priorityCount := 0
							for index, candidate := range priorities {
								if priorityMask&(1<<index) != 0 {
									labels = append(labels, candidate.label)
									value := candidate.value
									wantPriority = &value
									priorityCount++
								}
							}
							if priorityCount == 0 {
								value := task.PriorityP2
								wantPriority = &value
							} else if priorityCount > 1 {
								wantPriority = nil
							}

							wantBlockers := []string{}
							wantReasons := []string{}
							for index, blocker := range blockers {
								if blockerMask&(1<<index) != 0 {
									labels = append(labels, blocker)
									wantBlockers = append(wantBlockers, blocker)
									wantReasons = append(wantReasons, "blocker:"+blocker)
								}
							}
							if modeCount == 0 {
								wantReasons = append(wantReasons, "missing_mode")
							} else if modeCount > 1 {
								wantReasons = append(wantReasons, "conflicting_modes")
							}
							if priorityCount > 1 {
								wantReasons = append(wantReasons, "conflicting_priorities")
							}
							sort.Strings(wantReasons)
							if inProgress {
								labels = append(labels, task.LabelInProgress)
							}

							wantState := task.StateReady
							switch {
							case issueState == task.IssueClosed:
								wantState = task.StateDone
							case len(wantReasons) > 0:
								wantState = task.StateBlocked
							case inProgress:
								wantState = task.StateInProgress
							}

							got, err := task.Classify(task.Issue{Number: 1, State: issueState, Labels: labels})
							if err != nil {
								t.Fatal(err)
							}
							if got.State != wantState || !reflect.DeepEqual(got.Mode, wantMode) || !reflect.DeepEqual(got.Priority, wantPriority) || got.InProgress != inProgress || !reflect.DeepEqual(got.Blockers, wantBlockers) || !reflect.DeepEqual(got.Reasons, wantReasons) || !reflect.DeepEqual(got.ProjectLabels, []string{"project:kept"}) {
								t.Fatalf("Classify() = state %q mode %v priority %v progress %t blockers %v reasons %v project %v", got.State, got.Mode, got.Priority, got.InProgress, got.Blockers, got.Reasons, got.ProjectLabels)
							}
						})
					}
				}
			}
		}
	}
}

func TestNextOrdersEveryPriorityAndIgnoresMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		issues []task.Issue
		want   int
	}{
		{name: "p0 before lower issue p1", issues: []task.Issue{readyIssue(50, task.ModeTrivial, "priority:p0"), readyIssue(1, task.ModeTrivial, "priority:p1")}, want: 50},
		{name: "p1 before lower issue implicit p2", issues: []task.Issue{readyIssue(50, task.ModeBounded, "priority:p1"), readyIssue(1, task.ModeTrivial, "")}, want: 50},
		{name: "implicit and explicit p2 tie by issue", issues: []task.Issue{readyIssue(50, task.ModeInvestigate, ""), readyIssue(1, task.ModeTrivial, "priority:p2")}, want: 1},
		{name: "explicit p2 before lower issue p3", issues: []task.Issue{readyIssue(50, task.ModeTrivial, "priority:p2"), readyIssue(1, task.ModeTrivial, "priority:p3")}, want: 50},
		{name: "same priority ties by issue", issues: []task.Issue{readyIssue(50, task.ModeBounded, "priority:p0"), readyIssue(1, task.ModeInvestigate, "priority:p0")}, want: 1},
	}
	for _, mode := range []task.Mode{task.ModeTrivial, task.ModeBounded, task.ModeInvestigate} {
		tests = append(tests, struct {
			name   string
			issues []task.Issue
			want   int
		}{name: "winning mode " + string(mode), issues: []task.Issue{readyIssue(50, mode, "priority:p0"), readyIssue(1, task.ModeTrivial, "priority:p1")}, want: 50})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := task.NewMemoryStore()
			for _, issue := range test.issues {
				store.PutIssue(repository, issue)
			}
			got, err := task.NewService(store).Next(context.Background(), repository)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.Number != test.want {
				t.Fatalf("Next() = %#v, want #%d", got, test.want)
			}
		})
	}
}

func TestEveryMutationExecutionState(t *testing.T) {
	t.Parallel()
	trivial, bounded := task.ModeTrivial, task.ModeBounded
	tests := []struct {
		name        string
		initial     task.Issue
		noOpInitial task.Issue
		mutation    task.Mutation
	}{
		{name: "enroll", initial: issueWithLabels(), noOpInitial: issueWithLabels(task.LabelManaged, "agent:ready-trivial"), mutation: task.Mutation{Kind: task.MutationEnroll, ModeSet: true, Mode: &trivial}},
		{name: "update", initial: issueWithLabels(task.LabelManaged, "agent:ready-trivial"), noOpInitial: issueWithLabels(task.LabelManaged, "agent:ready-bounded"), mutation: task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded}},
		{name: "unenroll", initial: issueWithLabels(task.LabelManaged, "agent:ready-trivial", task.LabelInProgress), noOpInitial: issueWithLabels(), mutation: task.Mutation{Kind: task.MutationUnenroll}},
		{name: "start", initial: issueWithLabels(task.LabelManaged, "agent:ready-trivial"), noOpInitial: issueWithLabels(task.LabelManaged, "agent:ready-trivial", task.LabelInProgress), mutation: task.Mutation{Kind: task.MutationStart}},
		{name: "stop", initial: issueWithLabels(task.LabelManaged, "agent:ready-trivial", task.LabelInProgress), noOpInitial: issueWithLabels(task.LabelManaged, "agent:ready-trivial"), mutation: task.Mutation{Kind: task.MutationStop}},
		{name: "close", initial: issueWithLabels(task.LabelManaged, "agent:ready-trivial", task.LabelInProgress), noOpInitial: closedIssueWithLabels(task.LabelManaged, "agent:ready-trivial"), mutation: task.Mutation{Kind: task.MutationClose}},
	}

	for _, test := range tests {
		t.Run(test.name+"/dry-run", func(t *testing.T) {
			store := newInstrumentedStore(test.initial)
			before, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			result, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, true)
			if err != nil || !result.Changed || !result.DryRun || store.issueWrites != 0 {
				t.Fatalf("dry-run = %#v, writes %d, error %v", result, store.issueWrites, err)
			}
			after, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("dry-run changed issue: before %#v after %#v", before, after)
			}
		})
		t.Run(test.name+"/apply", func(t *testing.T) {
			store := newInstrumentedStore(test.initial)
			result, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, false)
			if err != nil || !result.Changed || result.DryRun {
				t.Fatalf("apply = %#v, error %v", result, err)
			}
		})
		t.Run(test.name+"/no-op", func(t *testing.T) {
			store := newInstrumentedStore(test.noOpInitial)
			result, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, false)
			if err != nil || result.Changed || len(result.Changes) != 0 || store.issueWrites != 0 {
				t.Fatalf("no-op = %#v, writes %d, error %v", result, store.issueWrites, err)
			}
		})
		t.Run(test.name+"/partial-failure", func(t *testing.T) {
			store := newInstrumentedStore(test.initial)
			store.failAfterIssueWrite = 1
			plan, planErr := task.PlanMutation(test.initial, test.mutation)
			if planErr != nil || len(plan.Changes) == 0 {
				t.Fatalf("PlanMutation() = %#v, %v", plan, planErr)
			}
			before, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			_, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, false)
			var mutationErr *task.MutationError
			if !errors.As(err, &mutationErr) {
				t.Fatalf("error = %#v, want MutationError", err)
			}
			after, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			if reflect.DeepEqual(after, before) {
				t.Fatalf("injected post-write failure had no confirmed effect: %#v", mutationErr)
			}
			if !hasChange(mutationErr.Changes, plan.Changes[0]) {
				t.Fatalf("confirmed changes %v omit post-write effect %v", mutationErr.Changes, plan.Changes[0])
			}
			assertMutationErrorMatchesIssue(t, mutationErr, after)
		})
		t.Run(test.name+"/final-reread-failure", func(t *testing.T) {
			store := newInstrumentedStore(test.initial)
			store.failGet = 2
			before, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			_, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, false)
			var mutationErr *task.MutationError
			if !errors.As(err, &mutationErr) || store.getCalls < 3 {
				t.Fatalf("error = %#v, gets %d, want failed final reread plus confirmation", err, store.getCalls)
			}
			after, _ := store.MemoryStore.GetIssue(context.Background(), repository, 1)
			if reflect.DeepEqual(after, before) {
				t.Fatalf("mutation did not apply before final reread failure")
			}
			assertMutationErrorMatchesIssue(t, mutationErr, after)
		})
	}
}

type instrumentedStore struct {
	*task.MemoryStore
	getCalls            int
	issueWrites         int
	failGet             int
	failAfterIssueWrite int
}

func newInstrumentedStore(issue task.Issue) *instrumentedStore {
	store := &instrumentedStore{MemoryStore: task.NewMemoryStore()}
	store.PutIssue(repository, issue)
	return store
}

func (s *instrumentedStore) GetIssue(ctx context.Context, repository string, number int) (task.Issue, error) {
	s.getCalls++
	if s.failGet == s.getCalls {
		return task.Issue{}, fmt.Errorf("injected get failure")
	}
	return s.MemoryStore.GetIssue(ctx, repository, number)
}

func (s *instrumentedStore) AddLabel(ctx context.Context, repository string, number int, label string) error {
	err := s.MemoryStore.AddLabel(ctx, repository, number, label)
	return s.afterIssueWrite(err)
}

func (s *instrumentedStore) RemoveLabel(ctx context.Context, repository string, number int, label string) error {
	err := s.MemoryStore.RemoveLabel(ctx, repository, number, label)
	return s.afterIssueWrite(err)
}

func (s *instrumentedStore) CloseIssue(ctx context.Context, repository string, number int) error {
	err := s.MemoryStore.CloseIssue(ctx, repository, number)
	return s.afterIssueWrite(err)
}

func (s *instrumentedStore) afterIssueWrite(err error) error {
	if err != nil {
		return err
	}
	s.issueWrites++
	if s.failAfterIssueWrite == s.issueWrites {
		return fmt.Errorf("injected post-write failure")
	}
	return nil
}

func assertMutationErrorMatchesIssue(t *testing.T, mutationErr *task.MutationError, issue task.Issue) {
	t.Helper()
	classified, err := task.Classify(issue)
	if taskErr := new(task.Error); errors.As(err, &taskErr) && taskErr.Code == "not_managed" {
		if mutationErr.Task != nil {
			t.Fatalf("confirmed task = %#v for unenrolled issue", mutationErr.Task)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if mutationErr.Task == nil || !reflect.DeepEqual(*mutationErr.Task, classified) {
		t.Fatalf("confirmed task = %#v, classify(issue) = %#v", mutationErr.Task, classified)
	}
}

func readyIssue(number int, mode task.Mode, priorityLabel string) task.Issue {
	modeLabel, _ := task.LabelForMode(mode)
	labels := []string{task.LabelManaged, modeLabel}
	if priorityLabel != "" {
		labels = append(labels, priorityLabel)
	}
	return task.Issue{Number: number, State: task.IssueOpen, Labels: labels}
}

func issueWithLabels(labels ...string) task.Issue {
	return task.Issue{Number: 1, State: task.IssueOpen, Labels: labels}
}

func closedIssueWithLabels(labels ...string) task.Issue {
	return task.Issue{Number: 1, State: task.IssueClosed, Labels: labels}
}

func hasChange(changes []task.Change, want task.Change) bool {
	for _, change := range changes {
		if change == want {
			return true
		}
	}
	return false
}
