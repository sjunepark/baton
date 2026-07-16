package task

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemoryStore is the deterministic adapter used to exercise the complete Task
// interface without transport or GitHub state.
type MemoryStore struct {
	mu     sync.Mutex
	issues map[string]map[int]Issue
	labels map[string]map[string]LabelDefinition

	FailAction string
	FailLabel  string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{issues: map[string]map[int]Issue{}, labels: map[string]map[string]LabelDefinition{}}
}

func (s *MemoryStore) PutIssue(repository string, issue Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.issues[repository] == nil {
		s.issues[repository] = map[int]Issue{}
	}
	s.issues[repository][issue.Number] = cloneIssue(issue)
}

func (s *MemoryStore) PutLabel(repository string, definition LabelDefinition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.labels[repository] == nil {
		s.labels[repository] = map[string]LabelDefinition{}
	}
	s.labels[repository][strings.ToLower(definition.Name)] = definition
}

func (s *MemoryStore) ListIssues(_ context.Context, repository string, state ListState) ([]Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []Issue{}
	for _, issue := range s.issues[repository] {
		if !hasLabel(canonicalLabelSet(issue.Labels), LabelManaged) {
			continue
		}
		if state == ListOpen && issue.State != IssueOpen || state == ListClosed && issue.State != IssueClosed {
			continue
		}
		result = append(result, cloneIssue(issue))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result, nil
}

func (s *MemoryStore) GetIssue(_ context.Context, repository string, number int) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	issue, ok := s.issues[repository][number]
	if !ok {
		return Issue{}, fmt.Errorf("issue #%d not found", number)
	}
	return cloneIssue(issue), nil
}

func (s *MemoryStore) EnsureLabel(_ context.Context, repository string, definition LabelDefinition) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("ensure_label", definition.Name); err != nil {
		return false, err
	}
	if s.labels[repository] == nil {
		s.labels[repository] = map[string]LabelDefinition{}
	}
	key := strings.ToLower(definition.Name)
	if _, ok := s.labels[repository][key]; ok {
		return false, nil
	}
	s.labels[repository][key] = definition
	return true, nil
}

func (s *MemoryStore) AddLabel(_ context.Context, repository string, number int, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("add_label", label); err != nil {
		return err
	}
	issue, ok := s.issues[repository][number]
	if !ok {
		return fmt.Errorf("issue #%d not found", number)
	}
	if _, ok := s.labels[repository][strings.ToLower(label)]; !ok {
		return fmt.Errorf("label %q does not exist", label)
	}
	if !hasLabel(canonicalLabelSet(issue.Labels), label) {
		issue.Labels = append(issue.Labels, label)
	}
	s.issues[repository][number] = issue
	return nil
}

func (s *MemoryStore) RemoveLabel(_ context.Context, repository string, number int, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("remove_label", label); err != nil {
		return err
	}
	issue, ok := s.issues[repository][number]
	if !ok {
		return fmt.Errorf("issue #%d not found", number)
	}
	filtered := make([]string, 0, len(issue.Labels))
	for _, existing := range issue.Labels {
		if !strings.EqualFold(existing, label) {
			filtered = append(filtered, existing)
		}
	}
	issue.Labels = filtered
	s.issues[repository][number] = issue
	return nil
}

func (s *MemoryStore) CloseIssue(_ context.Context, repository string, number int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("close_issue", ""); err != nil {
		return err
	}
	issue, ok := s.issues[repository][number]
	if !ok {
		return fmt.Errorf("issue #%d not found", number)
	}
	issue.State = IssueClosed
	s.issues[repository][number] = issue
	return nil
}

func (s *MemoryStore) failure(action, label string) error {
	if s.FailAction == action && (s.FailLabel == "" || strings.EqualFold(s.FailLabel, label)) {
		return fmt.Errorf("injected %s failure", action)
	}
	return nil
}

func cloneIssue(issue Issue) Issue {
	issue.Labels = append([]string(nil), issue.Labels...)
	return issue
}
