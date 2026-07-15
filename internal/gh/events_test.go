package gh

import (
	"os"
	"testing"
	"time"
)

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

func TestParseEditedIssueCommentPreviousBody(t *testing.T) {
	event, err := ParseIssueEvent([]byte(`{
  "action": "edited",
  "issue": {"number": 12, "node_id": "I_12", "body": "body", "labels": [{"name": "baton:managed"}]},
  "comment": {"id": 88, "body": "marker removed", "user": {"login": "github-actions[bot]", "type": "Bot"}},
  "changes": {"body": {"from": "<!-- baton-managed-issue:v1 original -->"}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.CommentID != 88 || event.CommentPreviousBody != "<!-- baton-managed-issue:v1 original -->" {
		t.Fatalf("event = %#v", event)
	}
}

func TestParsePullRequestEvent(t *testing.T) {
	pr, err := ParsePullRequestEvent([]byte(`{
  "action": "closed",
  "pull_request": {
    "number": 8,
    "node_id": "PR_8",
    "title": "Update policy",
    "body": "Refs #12",
    "state": "closed",
    "merged": true,
    "merged_at": "2026-07-15T01:02:03Z",
    "merge_commit_sha": "merge-sha",
    "base": {"ref": "agent", "sha": "base-sha", "repo": {"full_name": "example-org/example-repo"}},
    "head": {"ref": "agent-work/policy", "sha": "head-sha", "repo": {"full_name": "example-org/example-repo"}}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Action != "closed" || !pr.Merged || pr.State != "closed" || pr.Number != 8 || pr.NodeID != "PR_8" || pr.BaseRef != "agent" || pr.HeadRef != "agent-work/policy" || pr.BaseSHA != "base-sha" || pr.HeadSHA != "head-sha" || pr.Body != "Refs #12" || pr.MergeRevision != "merge-sha" || pr.MergedAt.Format(time.RFC3339) != "2026-07-15T01:02:03Z" {
		t.Fatalf("pr = %#v", pr)
	}
}

func TestParseMergedWorkPullRequestFixture(t *testing.T) {
	content, err := os.ReadFile("../../testdata/events/merged-work-pull-request.json")
	if err != nil {
		t.Fatal(err)
	}
	event, err := ParsePullRequestEvent(content)
	if err != nil {
		t.Fatal(err)
	}
	if event.Number != 42 || !event.Merged || event.HeadSHA != "head" || event.BaseRepositoryFullName != "example/repo" {
		t.Fatalf("event = %+v", event)
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
	tests := []struct {
		login, actorType, want string
	}{
		{login: "sejunpark", actorType: "User", want: "human"},
		{login: "robotics-human", actorType: "User", want: "human"},
		{login: "botticelli", actorType: "User", want: "human"},
		{login: "codex-user", actorType: "User", want: "human"},
		{login: "greptile-maintainer", actorType: "User", want: "human"},
		{login: "coderabbit-fan", actorType: "User", want: "human"},
		{login: "coderabbitai[bot]", actorType: "Bot", want: "coderabbit"},
		{login: "greptile-app", actorType: "Bot", want: "greptile"},
		{login: "codex-bot", actorType: "Bot", want: "codex"},
		{login: "actions[bot]", actorType: "Bot", want: "bot"},
		{login: "", actorType: "", want: "unknown"},
		{login: "someone", actorType: "Organization", want: "unknown"},
	}
	for _, test := range tests {
		if got := classifyAuthor(test.login, test.actorType); got != test.want {
			t.Fatalf("classifyAuthor(%q, %q) = %q, want %q", test.login, test.actorType, got, test.want)
		}
	}
}
