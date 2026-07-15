package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

const bodyPreviewRunes = 2000

type Service struct {
	store IssueStore
}

func NewService(store IssueStore) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context, repository string, state ListState) ([]Task, error) {
	issues, err := s.store.ListIssues(ctx, repository, state)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(issues))
	for _, issue := range issues {
		task, classifyErr := Classify(issue)
		if classifyErr != nil {
			var taskErr *Error
			if errors.As(classifyErr, &taskErr) && taskErr.Code == "not_managed" {
				continue
			}
			return nil, classifyErr
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Number < tasks[j].Number })
	return tasks, nil
}

func (s *Service) Show(ctx context.Context, repository string, number int, full bool) (Task, error) {
	issue, err := s.store.GetIssue(ctx, repository, number)
	if err != nil {
		return Task{}, err
	}
	task, err := Classify(issue)
	if err != nil {
		return Task{}, err
	}
	body, truncated := detailBody(issue.Body, full)
	task.Body = &body
	task.BodyTruncated = &truncated
	return task, nil
}

func (s *Service) Next(ctx context.Context, repository string) (*Task, error) {
	tasks, err := s.List(ctx, repository, ListOpen)
	if err != nil {
		return nil, err
	}
	ready := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if task.State == StateReady {
			ready = append(ready, task)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		left, right := priorityRank(ready[i].Priority), priorityRank(ready[j].Priority)
		if left != right {
			return left < right
		}
		return ready[i].Number < ready[j].Number
	})
	if len(ready) == 0 {
		return nil, nil
	}
	return &ready[0], nil
}

type MutationResult struct {
	Changed bool     `json:"changed"`
	DryRun  bool     `json:"dryRun"`
	Changes []Change `json:"changes"`
	Task    *Task    `json:"task"`
}

type MutationError struct {
	Code    string
	Message string
	Hint    string
	Changes []Change
	Task    *Task
	Cause   error
}

func (e *MutationError) Error() string { return e.Message }
func (e *MutationError) Unwrap() error { return e.Cause }

func (s *Service) Mutate(ctx context.Context, repository string, number int, mutation Mutation, dryRun bool) (MutationResult, error) {
	issue, err := s.store.GetIssue(ctx, repository, number)
	if err != nil {
		return MutationResult{}, err
	}
	plan, err := PlanMutation(issue, mutation)
	if err != nil {
		return MutationResult{}, err
	}
	if dryRun {
		return MutationResult{Changed: len(plan.Changes) > 0, DryRun: true, Changes: plan.Changes, Task: plan.Projected}, nil
	}

	applied := []Change{}
	for _, change := range plan.Changes {
		if err := s.applyChange(ctx, repository, number, change, &applied); err != nil {
			return MutationResult{}, s.mutationFailure(ctx, repository, number, applied, &change, err)
		}
	}
	if len(plan.Changes) == 0 {
		return MutationResult{Changes: plan.Changes, Task: plan.Projected}, nil
	}
	finalTask, err := s.readFinalTask(ctx, repository, number)
	if err != nil {
		return MutationResult{}, s.mutationFailure(ctx, repository, number, applied, nil, fmt.Errorf("reread final task: %w", err))
	}
	return MutationResult{Changed: len(plan.Changes) > 0, Changes: plan.Changes, Task: finalTask}, nil
}

func (s *Service) applyChange(ctx context.Context, repository string, number int, change Change, applied *[]Change) error {
	switch change.Action {
	case ChangeAddLabel:
		definition, err := definitionForLabel(change.Label)
		if err != nil {
			return err
		}
		created, err := s.store.EnsureLabel(ctx, repository, definition)
		if err != nil {
			return fmt.Errorf("ensure label %q: %w", change.Label, err)
		}
		if created {
			*applied = append(*applied, Change{Action: ChangeCreateLabel, Label: change.Label})
		}
		if err := s.store.AddLabel(ctx, repository, number, change.Label); err != nil {
			return fmt.Errorf("add label %q: %w", change.Label, err)
		}
	case ChangeRemoveLabel:
		if err := s.store.RemoveLabel(ctx, repository, number, change.Label); err != nil {
			return fmt.Errorf("remove label %q: %w", change.Label, err)
		}
	case ChangeCloseIssue:
		if err := s.store.CloseIssue(ctx, repository, number); err != nil {
			return fmt.Errorf("close issue: %w", err)
		}
	default:
		return fmt.Errorf("unknown Task change %q", change.Action)
	}
	*applied = append(*applied, change)
	return nil
}

func (s *Service) mutationFailure(ctx context.Context, repository string, number int, changes []Change, attempted *Change, cause error) *MutationError {
	issue, finalTask, readErr := s.readFinalState(ctx, repository, number)
	confirmed := append([]Change(nil), changes...)
	if readErr == nil && attempted != nil && changeConfirmed(issue, *attempted) {
		confirmed = append(confirmed, *attempted)
	}
	if readErr != nil {
		cause = errors.Join(cause, fmt.Errorf("reread state after mutation failure: %w", readErr))
	}
	return &MutationError{
		Code: "mutation_failed", Message: fmt.Sprintf("Task mutation for issue #%d failed", number),
		Hint:    "Inspect the confirmed changes and current task, then retry the command.",
		Changes: confirmed, Task: finalTask, Cause: cause,
	}
}

func (s *Service) readFinalTask(ctx context.Context, repository string, number int) (*Task, error) {
	_, finalTask, err := s.readFinalState(ctx, repository, number)
	return finalTask, err
}

func (s *Service) readFinalState(ctx context.Context, repository string, number int) (Issue, *Task, error) {
	issue, err := s.store.GetIssue(ctx, repository, number)
	if err != nil {
		return Issue{}, nil, err
	}
	task, err := Classify(issue)
	var taskErr *Error
	if errors.As(err, &taskErr) && taskErr.Code == "not_managed" {
		return issue, nil, nil
	}
	if err != nil {
		return Issue{}, nil, err
	}
	return issue, &task, nil
}

func changeConfirmed(issue Issue, change Change) bool {
	labels := canonicalLabelSet(issue.Labels)
	switch change.Action {
	case ChangeAddLabel:
		return hasLabel(labels, change.Label)
	case ChangeRemoveLabel:
		return !hasLabel(labels, change.Label)
	case ChangeCloseIssue:
		return issue.State == IssueClosed
	default:
		return false
	}
}

func detailBody(body string, full bool) (string, bool) {
	if full || utf8.RuneCountInString(body) <= bodyPreviewRunes {
		return body, false
	}
	runes := []rune(body)
	return string(runes[:bodyPreviewRunes]), true
}
