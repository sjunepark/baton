package task_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sjunepark/baton/internal/task"
)

const repository = "example/repo"

func TestClassifyLifecycle(t *testing.T) {
	t.Parallel()
	modeTrivial, modeBounded := "agent:ready-trivial", "agent:ready-bounded"
	tests := []struct {
		name         string
		state        task.IssueState
		labels       []string
		wantState    task.State
		wantMode     *task.Mode
		wantPriority *task.Priority
		wantReasons  []string
	}{
		{name: "ready with implicit p2", state: task.IssueOpen, labels: []string{task.LabelManaged, modeTrivial}, wantState: task.StateReady, wantMode: mode(task.ModeTrivial), wantPriority: priority(task.PriorityP2), wantReasons: []string{}},
		{name: "activity", state: task.IssueOpen, labels: []string{task.LabelManaged, modeTrivial, task.LabelInProgress}, wantState: task.StateInProgress, wantMode: mode(task.ModeTrivial), wantPriority: priority(task.PriorityP2), wantReasons: []string{}},
		{name: "missing mode", state: task.IssueOpen, labels: []string{task.LabelManaged}, wantState: task.StateBlocked, wantPriority: priority(task.PriorityP2), wantReasons: []string{"missing_mode"}},
		{name: "conflicting modes", state: task.IssueOpen, labels: []string{task.LabelManaged, modeTrivial, modeBounded}, wantState: task.StateBlocked, wantPriority: priority(task.PriorityP2), wantReasons: []string{"conflicting_modes"}},
		{name: "conflicting priorities", state: task.IssueOpen, labels: []string{task.LabelManaged, modeTrivial, "priority:p0", "priority:p3"}, wantState: task.StateBlocked, wantMode: mode(task.ModeTrivial), wantReasons: []string{"conflicting_priorities"}},
		{name: "blocker wins over activity", state: task.IssueOpen, labels: []string{task.LabelManaged, modeTrivial, task.LabelInProgress, task.BlockerNeedsInfo}, wantState: task.StateBlocked, wantMode: mode(task.ModeTrivial), wantPriority: priority(task.PriorityP2), wantReasons: []string{"blocker:needs-info"}},
		{name: "closed wins over conflicts", state: task.IssueClosed, labels: []string{task.LabelManaged, modeTrivial, modeBounded, "priority:p0", "priority:p1"}, wantState: task.StateDone, wantReasons: []string{"conflicting_modes", "conflicting_priorities"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := task.Classify(task.Issue{Number: 7, State: test.state, Labels: test.labels})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != test.wantState || !reflect.DeepEqual(got.Mode, test.wantMode) || !reflect.DeepEqual(got.Priority, test.wantPriority) || !reflect.DeepEqual(got.Reasons, test.wantReasons) {
				t.Fatalf("Classify() = state %q mode %v priority %v reasons %v", got.State, got.Mode, got.Priority, got.Reasons)
			}
		})
	}
}

func TestClassifyRejectsUnenrolledIssue(t *testing.T) {
	t.Parallel()
	_, err := task.Classify(task.Issue{Number: 9, State: task.IssueOpen, Labels: []string{"agent:ready-trivial"}})
	var taskErr *task.Error
	if !errors.As(err, &taskErr) || taskErr.Code != "not_managed" || !strings.Contains(taskErr.Hint, "baton enroll 9") {
		t.Fatalf("Classify() error = %#v", err)
	}
}

func TestAdopterLabelsRespectExplicitEnrollment(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	const legacyBody = "### Summary\nLegacy v0.5 body content must remain ordinary issue data."
	tests := []struct {
		name    string
		issue   task.Issue
		managed bool
	}{
		{name: "unmanaged issue", issue: task.Issue{Number: 40, Title: "ordinary issue", State: task.IssueOpen, Labels: []string{"Customer:Acme"}}},
		{name: "legacy labels do not enroll", issue: task.Issue{Number: 50, Title: "v0.5 candidate", Body: legacyBody, State: task.IssueOpen, Labels: []string{"agent:ready-bounded", "priority:p1", "Customer:Acme"}}},
		{name: "managed label enrolls", issue: task.Issue{Number: 60, Title: "v0.6 managed", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial", "priority:p2"}}, managed: true},
	}
	for _, test := range tests {
		store.PutIssue(repository, test.issue)
		t.Run(test.name, func(t *testing.T) {
			_, err := task.Classify(test.issue)
			var taskErr *task.Error
			if test.managed && err != nil {
				t.Fatalf("Classify() error = %v", err)
			}
			if !test.managed && (!errors.As(err, &taskErr) || taskErr.Code != "not_managed") {
				t.Fatalf("Classify() error = %#v, want not_managed", err)
			}
		})
	}
	service := task.NewService(store)

	listed, err := service.List(context.Background(), repository, task.ListAll)
	if err != nil {
		t.Fatal(err)
	}
	if got := taskNumbers(listed); !reflect.DeepEqual(got, []int{60}) {
		t.Fatalf("legacy labels enrolled unexpected issues: %v", got)
	}

	bounded, p1 := task.ModeBounded, task.PriorityP1
	_, err = service.Mutate(context.Background(), repository, 50, task.Mutation{
		Kind: task.MutationEnroll, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p1,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := store.GetIssue(context.Background(), repository, 50)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Body != legacyBody || !reflect.DeepEqual(issue.Labels, []string{"agent:ready-bounded", "priority:p1", "Customer:Acme", task.LabelManaged}) {
		t.Fatalf("approved enrollment changed legacy issue data: %#v", issue)
	}
	listed, err = service.List(context.Background(), repository, task.ListAll)
	if err != nil {
		t.Fatal(err)
	}
	if got := taskNumbers(listed); !reflect.DeepEqual(got, []int{50, 60}) {
		t.Fatalf("explicit enrollment exposed wrong Tasks: %v", got)
	}
}

func TestListShowAndNext(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	for _, issue := range []task.Issue{
		{Number: 8, Title: "p2", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial"}},
		{Number: 7, Title: "p0 bounded", State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-bounded", "priority:p0"}},
		{Number: 3, Title: "p0 investigation", State: task.IssueOpen, Body: strings.Repeat("가", 2001), Labels: []string{task.LabelManaged, "agent:investigate-only", "priority:p0", "documentation"}},
		{Number: 1, Title: "blocked", State: task.IssueOpen, Labels: []string{task.LabelManaged}},
		{Number: 2, Title: "closed", State: task.IssueClosed, Labels: []string{task.LabelManaged, "agent:ready-trivial", "priority:p0"}},
		{Number: 4, Title: "not enrolled", State: task.IssueOpen, Labels: []string{"agent:ready-trivial", "priority:p0"}},
	} {
		store.PutIssue(repository, issue)
	}
	service := task.NewService(store)

	listed, err := service.List(context.Background(), repository, task.ListOpen)
	if err != nil {
		t.Fatal(err)
	}
	if got := taskNumbers(listed); !reflect.DeepEqual(got, []int{1, 3, 7, 8}) {
		t.Fatalf("List() numbers = %v", got)
	}
	next, err := service.Next(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.Number != 3 {
		t.Fatalf("Next() = %#v, want issue 3; mode must not affect ordering", next)
	}
	shown, err := service.Show(context.Background(), repository, 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Body == nil || shown.BodyTruncated == nil || !*shown.BodyTruncated || len([]rune(*shown.Body)) != 2000 {
		t.Fatalf("Show() body = %v truncated = %v", shown.Body, shown.BodyTruncated)
	}
}

func TestNextReturnsDefinitiveNil(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	listed, err := task.NewService(store).List(context.Background(), repository, task.ListOpen)
	if err != nil || listed == nil || len(listed) != 0 {
		t.Fatalf("empty List() = %#v, %v", listed, err)
	}
	store.PutIssue(repository, task.Issue{Number: 1, State: task.IssueOpen, Labels: []string{task.LabelManaged}})
	next, err := task.NewService(store).Next(context.Background(), repository)
	if err != nil || next != nil {
		t.Fatalf("Next() = %#v, %v", next, err)
	}
}

func TestMutationDryRunApplyAndIdempotence(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	const originalBody = "legacy body with no Baton authority"
	store.PutIssue(repository, task.Issue{Number: 12, Title: "task", Body: originalBody, State: task.IssueOpen, Labels: []string{"Customer:Acme"}})
	service := task.NewService(store)
	bounded, p1 := task.ModeBounded, task.PriorityP1
	mutation := task.Mutation{Kind: task.MutationEnroll, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p1}

	dryRun, err := service.Mutate(context.Background(), repository, 12, mutation, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dryRun.Changed || !dryRun.DryRun || dryRun.Task == nil || dryRun.Task.State != task.StateReady || !reflect.DeepEqual(dryRun.Task.ProjectLabels, []string{"Customer:Acme"}) {
		t.Fatalf("dry-run = %#v", dryRun)
	}
	if _, err := service.Show(context.Background(), repository, 12, false); err == nil {
		t.Fatal("dry-run mutated the store")
	}

	applied, err := service.Mutate(context.Background(), repository, 12, mutation, false)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Changed || applied.Task == nil || applied.Task.Mode == nil || *applied.Task.Mode != task.ModeBounded || len(applied.Changes) != 3 {
		t.Fatalf("apply = %#v", applied)
	}
	if !reflect.DeepEqual(applied.Task.ProjectLabels, dryRun.Task.ProjectLabels) {
		t.Fatalf("dry-run project labels %v differ from apply %v", dryRun.Task.ProjectLabels, applied.Task.ProjectLabels)
	}
	if !reflect.DeepEqual(applied.Changes, dryRun.Changes) {
		t.Fatalf("apply changes %v differ from dry-run plan %v", applied.Changes, dryRun.Changes)
	}
	issue, err := store.GetIssue(context.Background(), repository, 12)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Body != originalBody || !reflect.DeepEqual(issue.Labels, []string{"Customer:Acme", "agent:ready-bounded", "priority:p1", task.LabelManaged}) {
		t.Fatalf("enroll changed issue content or project labels: %#v", issue)
	}
	noOp, err := service.Mutate(context.Background(), repository, 12, mutation, false)
	if err != nil {
		t.Fatal(err)
	}
	if noOp.Changed || len(noOp.Changes) != 0 {
		t.Fatalf("idempotent apply = %#v", noOp)
	}
}

func TestLifecycleMutations(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 5, State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial", "priority:p1", "bug"}})
	service := task.NewService(store)

	started := mutate(t, service, 5, task.Mutation{Kind: task.MutationStart})
	if started.Task == nil || started.Task.State != task.StateInProgress {
		t.Fatalf("start = %#v", started)
	}
	if repeated := mutate(t, service, 5, task.Mutation{Kind: task.MutationStart}); repeated.Changed {
		t.Fatalf("idempotent start = %#v", repeated)
	}
	blocked := mutate(t, service, 5, task.Mutation{Kind: task.MutationUpdate, AddBlockers: []string{task.BlockerNeedsDiscussion}})
	if blocked.Task == nil || blocked.Task.State != task.StateBlocked || !blocked.Task.InProgress {
		t.Fatalf("blocked update = %#v", blocked)
	}
	stopped := mutate(t, service, 5, task.Mutation{Kind: task.MutationStop})
	if stopped.Task == nil || stopped.Task.InProgress {
		t.Fatalf("stop = %#v", stopped)
	}
	if repeated := mutate(t, service, 5, task.Mutation{Kind: task.MutationStop}); repeated.Changed {
		t.Fatalf("idempotent stop = %#v", repeated)
	}
	closed := mutate(t, service, 5, task.Mutation{Kind: task.MutationClose})
	if closed.Task == nil || closed.Task.State != task.StateDone || !reflect.DeepEqual(closed.Task.ProjectLabels, []string{"bug"}) {
		t.Fatalf("close = %#v", closed)
	}
	noOp := mutate(t, service, 5, task.Mutation{Kind: task.MutationClose})
	if noOp.Changed {
		t.Fatalf("idempotent close = %#v", noOp)
	}
	unenrolled := mutate(t, service, 5, task.Mutation{Kind: task.MutationUnenroll})
	if unenrolled.Task != nil {
		t.Fatalf("unenroll = %#v", unenrolled)
	}
	issue, err := store.GetIssue(context.Background(), repository, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(issue.Labels, []string{"agent:ready-trivial", "priority:p1", "bug", task.BlockerNeedsDiscussion}) {
		t.Fatalf("unenroll changed classification/project labels: %v", issue.Labels)
	}
	noOp = mutate(t, service, 5, task.Mutation{Kind: task.MutationUnenroll})
	if noOp.Changed || noOp.Task != nil {
		t.Fatalf("idempotent unenroll = %#v", noOp)
	}
}

func TestShowFullBody(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("x", 2001)
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 13, State: task.IssueOpen, Body: body, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	shown, err := task.NewService(store).Show(context.Background(), repository, 13, true)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Body == nil || *shown.Body != body || shown.BodyTruncated == nil || *shown.BodyTruncated {
		t.Fatalf("full Show() = %#v", shown)
	}
}

func TestMutationsRejectUnenrolledAndClosedStart(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 1, State: task.IssueOpen})
	store.PutIssue(repository, task.Issue{Number: 2, State: task.IssueClosed, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	service := task.NewService(store)
	for _, mutation := range []task.Mutation{{Kind: task.MutationUpdate, ModeSet: true}, {Kind: task.MutationStart}, {Kind: task.MutationStop}, {Kind: task.MutationClose}} {
		_, err := service.Mutate(context.Background(), repository, 1, mutation, false)
		var taskErr *task.Error
		if !errors.As(err, &taskErr) || taskErr.Code != "not_managed" {
			t.Fatalf("%s error = %#v", mutation.Kind, err)
		}
	}
	_, err := service.Mutate(context.Background(), repository, 2, task.Mutation{Kind: task.MutationStart}, false)
	var taskErr *task.Error
	if !errors.As(err, &taskErr) || taskErr.Code != "invalid_transition" {
		t.Fatalf("closed start error = %#v", err)
	}
}

func TestMutationReportsConfirmedPartialFailureAndFinalReread(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 6, State: task.IssueOpen})
	store.FailAction = "add_label"
	store.FailLabel = "agent:ready-bounded"
	bounded := task.ModeBounded
	_, err := task.NewService(store).Mutate(context.Background(), repository, 6, task.Mutation{Kind: task.MutationEnroll, ModeSet: true, Mode: &bounded}, false)
	var mutationErr *task.MutationError
	if !errors.As(err, &mutationErr) {
		t.Fatalf("Mutate() error = %#v", err)
	}
	if mutationErr.Task != nil || !reflect.DeepEqual(mutationErr.Changes, []task.Change{{Action: task.ChangeCreateLabel, Label: "agent:ready-bounded"}}) {
		t.Fatalf("partial error = %#v", mutationErr)
	}
}

func TestMutationPreservesOriginalErrorWhenNothingChanged(t *testing.T) {
	t.Parallel()
	store := &typedFailureStore{MemoryStore: task.NewMemoryStore()}
	store.PutIssue(repository, task.Issue{
		Number: 14, State: task.IssueOpen,
		Labels: []string{task.LabelManaged, "agent:ready-trivial"},
	})
	if _, err := store.EnsureLabel(context.Background(), repository, task.LabelDefinition{Name: task.LabelInProgress}); err != nil {
		t.Fatal(err)
	}

	_, err := task.NewService(store).Mutate(
		context.Background(), repository, 14,
		task.Mutation{Kind: task.MutationStart}, false,
	)
	var taskErr *task.Error
	var mutationErr *task.MutationError
	if !errors.As(err, &taskErr) || taskErr.Code != "access_denied" || errors.As(err, &mutationErr) {
		t.Fatalf("Mutate() error = %#v", err)
	}
}

type typedFailureStore struct {
	*task.MemoryStore
}

func (*typedFailureStore) AddLabel(context.Context, string, int, string) error {
	return &task.Error{Code: "access_denied", Message: "repository access denied", Hint: "Check token permissions."}
}

func TestFailedFacetReplacementPreservesPriorClassification(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 10, State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial"}})
	store.FailAction = "add_label"
	store.FailLabel = "agent:ready-bounded"
	bounded := task.ModeBounded
	_, err := task.NewService(store).Mutate(context.Background(), repository, 10, task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded}, false)
	var mutationErr *task.MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Task == nil || mutationErr.Task.Mode == nil || *mutationErr.Task.Mode != task.ModeTrivial || mutationErr.Task.State != task.StateReady {
		t.Fatalf("failed replacement = %#v", err)
	}
}

func TestUpdateClearsFacetAndRejectsConflictingBlockerChange(t *testing.T) {
	t.Parallel()
	store := task.NewMemoryStore()
	store.PutIssue(repository, task.Issue{Number: 11, State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial", "priority:p0"}})
	service := task.NewService(store)
	result := mutate(t, service, 11, task.Mutation{Kind: task.MutationUpdate, ModeSet: true, PrioritySet: true})
	if result.Task == nil || result.Task.Mode != nil || result.Task.Priority == nil || *result.Task.Priority != task.PriorityP2 || result.Task.State != task.StateBlocked {
		t.Fatalf("clear facets = %#v", result)
	}
	_, err := service.Mutate(context.Background(), repository, 11, task.Mutation{
		Kind: task.MutationUpdate, AddBlockers: []string{task.BlockerNeedsInfo}, RemoveBlockers: []string{"NEEDS-INFO"},
	}, true)
	if err == nil {
		t.Fatal("update accepted the same blocker in add and remove")
	}
}

func TestUpdatePlannerNormalizesFacetsAndPreservesProjectLabels(t *testing.T) {
	t.Parallel()
	bounded := task.ModeBounded
	p2 := task.PriorityP2
	issue := task.Issue{Number: 1, State: task.IssueOpen, Labels: []string{
		task.LabelManaged, "agent:ready-trivial", "agent:investigate-only", "priority:p0", "priority:p3", "Customer:Acme",
	}}
	originalLabels := append([]string(nil), issue.Labels...)
	plan, err := task.PlanMutation(issue, task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p2})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Projected == nil || plan.Projected.Mode == nil || *plan.Projected.Mode != task.ModeBounded || plan.Projected.Priority == nil || *plan.Projected.Priority != task.PriorityP2 || !reflect.DeepEqual(plan.Projected.ProjectLabels, []string{"Customer:Acme"}) {
		t.Fatalf("PlanMutation() = %#v", plan)
	}
	if !reflect.DeepEqual(issue.Labels, originalLabels) {
		t.Fatalf("PlanMutation() mutated input labels: %v", issue.Labels)
	}
}

func TestMutationPlansKeepIncompleteWorkNonDispatchable(t *testing.T) {
	t.Parallel()
	bounded := task.ModeBounded
	p1 := task.PriorityP1
	tests := []struct {
		name     string
		issue    task.Issue
		mutation task.Mutation
	}{
		{
			name:     "enrollment publishes last",
			issue:    issueWithLabels("Customer:Acme"),
			mutation: task.Mutation{Kind: task.MutationEnroll, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p1},
		},
		{
			name:     "blocker replacement adds before removal",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-bounded", task.BlockerNeedsInfo),
			mutation: task.Mutation{Kind: task.MutationUpdate, AddBlockers: []string{task.BlockerNeedsDiscussion}, RemoveBlockers: []string{task.BlockerNeedsInfo}},
		},
		{
			name:     "facet replacement keeps conflict until complete",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-trivial", "agent:investigate-only"),
			mutation: task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded},
		},
		{
			name:     "cross-facet update resolves the last conflict last",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-trivial", "agent:investigate-only", "priority:p0"),
			mutation: task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p1},
		},
		{
			name:     "ready cross-facet update stays hidden between writes",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-trivial", "priority:p0"),
			mutation: task.Mutation{Kind: task.MutationUpdate, ModeSet: true, Mode: &bounded, PrioritySet: true, Priority: &p1},
		},
		{
			name:     "unenrollment hides before activity cleanup",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-bounded", task.LabelInProgress),
			mutation: task.Mutation{Kind: task.MutationUnenroll},
		},
		{
			name:     "close reaches terminal state before activity cleanup",
			issue:    issueWithLabels(task.LabelManaged, "agent:ready-bounded", task.LabelInProgress),
			mutation: task.Mutation{Kind: task.MutationClose},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := task.PlanMutation(test.issue, test.mutation)
			if err != nil {
				t.Fatal(err)
			}
			for failedWrite := 1; failedWrite < len(plan.Changes); failedWrite++ {
				store := newInstrumentedStore(test.issue)
				store.failAfterIssueWrite = failedWrite
				_, err := task.NewService(store).Mutate(context.Background(), repository, 1, test.mutation, false)
				var mutationErr *task.MutationError
				if !errors.As(err, &mutationErr) {
					t.Fatalf("write %d error = %#v, want MutationError", failedWrite, err)
				}
				if mutationErr.Task != nil && mutationErr.Task.State == task.StateReady {
					t.Fatalf("write %d of %d published ready task before plan completed: %#v", failedWrite, len(plan.Changes), mutationErr)
				}
			}
		})
	}
}

func TestPlannerRejectsUnsafeConflictingFacetClear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		labels   []string
		mutation task.Mutation
	}{
		{
			name:     "mode",
			labels:   []string{task.LabelManaged, "agent:ready-trivial", "agent:ready-bounded"},
			mutation: task.Mutation{Kind: task.MutationUpdate, ModeSet: true},
		},
		{
			name:     "priority",
			labels:   []string{task.LabelManaged, "agent:ready-trivial", "priority:p0", "priority:p1"},
			mutation: task.Mutation{Kind: task.MutationUpdate, PrioritySet: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := task.PlanMutation(issueWithLabels(test.labels...), test.mutation)
			var taskErr *task.Error
			if !errors.As(err, &taskErr) || taskErr.Code != "invalid_transition" || taskErr.Hint == "" {
				t.Fatalf("PlanMutation() error = %#v", err)
			}
		})
	}
}

func TestPlannerAllowsConflictingFacetClearBehindExistingGuard(t *testing.T) {
	t.Parallel()
	plan, err := task.PlanMutation(issueWithLabels(
		task.LabelManaged,
		"agent:ready-trivial",
		"agent:ready-bounded",
		task.BlockerNeedsInfo,
	), task.Mutation{Kind: task.MutationUpdate, ModeSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Projected == nil || plan.Projected.State != task.StateBlocked || plan.Projected.Mode != nil {
		t.Fatalf("PlanMutation() = %#v", plan)
	}
}

func TestPlannerRejectsFieldsIrrelevantToMutation(t *testing.T) {
	t.Parallel()
	trivial := task.ModeTrivial
	tests := []task.Mutation{
		{Kind: task.MutationEnroll, AddBlockers: []string{task.BlockerNeedsInfo}},
		{Kind: task.MutationStart, ModeSet: true, Mode: &trivial},
		{Kind: task.MutationStop, Priority: priority(task.PriorityP0)},
		{Kind: "unknown"},
	}
	for _, mutation := range tests {
		_, err := task.PlanMutation(task.Issue{Number: 1, State: task.IssueOpen, Labels: []string{task.LabelManaged, "agent:ready-trivial"}}, mutation)
		if err == nil {
			t.Fatalf("PlanMutation(%#v) accepted irrelevant fields", mutation)
		}
	}
}

func mutate(t *testing.T, service *task.Service, number int, mutation task.Mutation) task.MutationResult {
	t.Helper()
	result, err := service.Mutate(context.Background(), repository, number, mutation, false)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mode(value task.Mode) *task.Mode             { return &value }
func priority(value task.Priority) *task.Priority { return &value }

func taskNumbers(tasks []task.Task) []int {
	result := make([]int, len(tasks))
	for i, task := range tasks {
		result[i] = task.Number
	}
	return result
}
