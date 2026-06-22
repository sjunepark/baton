package policy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sejunpark/baton/internal/config"
)

type IssuePolicyInput struct {
	Body          string             `json:"body"`
	CurrentLabels []string           `json:"currentLabels"`
	Policy        config.IssuePolicy `json:"-"`
}

type IssuePolicyDecision struct {
	SchemaVersion           int      `json:"schemaVersion"`
	Kind                    string   `json:"kind"`
	IsFormIssue             bool     `json:"isFormIssue"`
	LabelsToAdd             []string `json:"labelsToAdd"`
	LabelsToRemove          []string `json:"labelsToRemove"`
	MissingRequiredSections []string `json:"missingRequiredSections"`
	PolicyCommentBody       *string  `json:"policyCommentBody"`
}

var issueHeadingPattern = regexp.MustCompile(`^###\s+(.+?)\s*$`)

func ComputeIssuePolicy(input IssuePolicyInput) IssuePolicyDecision {
	sections := ParseIssueSections(input.Body)
	if !hasFormFingerprint(sections, input.Policy) {
		return IssuePolicyDecision{
			SchemaVersion:           1,
			Kind:                    "issuePolicyDecision",
			IsFormIssue:             false,
			LabelsToAdd:             []string{},
			LabelsToRemove:          []string{},
			MissingRequiredSections: []string{},
			PolicyCommentBody:       nil,
		}
	}

	currentLabels := stringSet(input.CurrentLabels)
	desiredLabels := map[string]struct{}{}
	workKind := sectionValue(sections, input.Policy.FormSections["work_kind"])
	agentMode := sectionValue(sections, input.Policy.FormSections["agent_mode"])
	if label := input.Policy.WorkKindLabels[workKind]; label != "" {
		desiredLabels[label] = struct{}{}
	}
	if label := input.Policy.AgentModeLabels[agentMode]; label != "" {
		desiredLabels[label] = struct{}{}
	}

	modeSlug := normalizeSlug(agentMode)
	missing := make([]string, 0)
	for _, sectionID := range input.Policy.RequiredSections[modeSlug] {
		if !hasUsefulContent(sectionValue(sections, input.Policy.FormSections[sectionID])) {
			missing = append(missing, firstNonEmpty(input.Policy.FormSections[sectionID], sectionID))
		}
	}
	if len(missing) > 0 {
		desiredLabels[blockedLabel(input.Policy)] = struct{}{}
	}

	controlledLabels := map[string]struct{}{}
	for _, group := range input.Policy.ControlledLabelGroups {
		for _, label := range group {
			controlledLabels[label] = struct{}{}
		}
	}

	labelsToAdd := make([]string, 0, len(desiredLabels))
	for label := range desiredLabels {
		if _, exists := currentLabels[label]; !exists {
			labelsToAdd = append(labelsToAdd, label)
		}
	}
	sort.Strings(labelsToAdd)

	labelsToRemove := make([]string, 0)
	for label := range controlledLabels {
		if _, exists := currentLabels[label]; !exists {
			continue
		}
		if _, desired := desiredLabels[label]; !desired {
			labelsToRemove = append(labelsToRemove, label)
		}
	}
	sort.Strings(labelsToRemove)

	var comment *string
	if len(missing) > 0 {
		body := createBlockedComment(missing, input.Policy.PolicyCommentMarker)
		comment = &body
	}

	return IssuePolicyDecision{
		SchemaVersion:           1,
		Kind:                    "issuePolicyDecision",
		IsFormIssue:             true,
		LabelsToAdd:             labelsToAdd,
		LabelsToRemove:          labelsToRemove,
		MissingRequiredSections: missing,
		PolicyCommentBody:       comment,
	}
}

func ParseIssueSections(body string) map[string]string {
	sections := map[string]string{}
	var currentHeading string
	var currentLines []string

	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		matches := issueHeadingPattern.FindStringSubmatch(line)
		if len(matches) > 0 {
			if currentHeading != "" {
				sections[currentHeading] = strings.TrimSpace(strings.Join(currentLines, "\n"))
			}
			currentHeading = strings.TrimSpace(matches[1])
			currentLines = nil
			continue
		}
		if currentHeading != "" {
			currentLines = append(currentLines, line)
		}
	}

	if currentHeading != "" {
		sections[currentHeading] = strings.TrimSpace(strings.Join(currentLines, "\n"))
	}
	return sections
}

func hasFormFingerprint(sections map[string]string, issuePolicy config.IssuePolicy) bool {
	for _, sectionID := range []string{"work_kind", "agent_mode", "summary"} {
		heading := issuePolicy.FormSections[sectionID]
		if heading == "" {
			return false
		}
		if _, exists := sections[heading]; !exists {
			return false
		}
	}
	return true
}

func sectionValue(sections map[string]string, heading string) string {
	if heading == "" {
		return ""
	}
	return strings.TrimSpace(sections[heading])
}

func hasUsefulContent(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "no response", "_no response_", "n/a", "none":
		return false
	default:
		return true
	}
}

func createBlockedComment(missing []string, marker string) string {
	lines := make([]string, len(missing))
	for i, section := range missing {
		lines[i] = fmt.Sprintf("- %s", section)
	}
	return fmt.Sprintf(`%s

This issue is marked ready for agent work, but the policy gate is blocking implementation.

Missing required sections:

%s

Update the issue body and the policy action will remove `+"`agent:blocked`"+` when the form is complete.`, marker, strings.Join(lines, "\n"))
}

func ClearIssuePolicyComment(marker string) string {
	return marker + "\n\nThe issue policy gate is currently clear. `agent:blocked` is not required."
}

func blockedLabel(issuePolicy config.IssuePolicy) string {
	if labels := issuePolicy.ControlledLabelGroups["quality_gate"]; len(labels) > 0 {
		return labels[0]
	}
	return "agent:blocked"
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
