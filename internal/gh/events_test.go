package gh

import "testing"

func TestParseIssueEvent(t *testing.T) {
	event, err := ParseIssueEvent([]byte(`{
  "issue": {
    "number": 12,
    "body": "### Summary\n\nDo the thing.",
    "labels": [{"name": "bug"}, {"name": "needs-info"}]
  },
  "repository": {"full_name": "example-org/example-repo"}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Number != 12 || event.Body == "" || event.Repository != "example-org/example-repo" {
		t.Fatalf("event = %#v", event)
	}
	if len(event.Labels) != 2 || event.Labels[0] != "bug" || event.Labels[1] != "needs-info" {
		t.Fatalf("labels = %#v", event.Labels)
	}
}

func TestParsePullRequestEvent(t *testing.T) {
	pr, err := ParsePullRequestEvent([]byte(`{
  "pull_request": {
    "number": 8,
    "title": "Update policy",
    "body": "Refs #12",
    "base": {"ref": "agent", "repo": {"full_name": "example-org/example-repo"}},
    "head": {"ref": "agent-work/policy", "repo": {"full_name": "example-org/example-repo"}}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 8 || pr.BaseRef != "agent" || pr.HeadRef != "agent-work/policy" || pr.Body != "Refs #12" {
		t.Fatalf("pr = %#v", pr)
	}
}

func TestParsePullRequestEventBaseBranchFlows(t *testing.T) {
	tests := []struct {
		name    string
		baseRef string
		headRef string
	}{
		{name: "promotion", baseRef: "main", headRef: "agent"},
		{name: "direct human", baseRef: "main", headRef: "feature-x"},
		{name: "direct work branch", baseRef: "main", headRef: "agent-work/123-policy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := ParsePullRequestEvent([]byte(`{
  "pull_request": {
    "number": 9,
    "title": "Update policy",
    "body": "Refs #12",
    "base": {"ref": "` + tt.baseRef + `", "repo": {"full_name": "example-org/example-repo"}},
    "head": {"ref": "` + tt.headRef + `", "repo": {"full_name": "example-org/example-repo"}}
  },
  "repository": {"full_name": "example-org/example-repo"}
}`))
			if err != nil {
				t.Fatal(err)
			}
			if pr.BaseRef != tt.baseRef || pr.HeadRef != tt.headRef || pr.BaseRepositoryFullName != "example-org/example-repo" {
				t.Fatalf("pr = %#v, want base=%q head=%q repo=example-org/example-repo", pr, tt.baseRef, tt.headRef)
			}
		})
	}
}

func TestClassifyAuthor(t *testing.T) {
	tests := map[string]string{
		"sejunpark":         "human",
		"coderabbitai[bot]": "coderabbit",
		"greptile-app":      "greptile",
		"codex-bot":         "codex",
		"actions[bot]":      "bot",
		"":                  "unknown",
	}
	for login, want := range tests {
		if got := classifyAuthor(login); got != want {
			t.Fatalf("classifyAuthor(%q) = %q, want %q", login, got, want)
		}
	}
}
