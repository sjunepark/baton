package gh

import "testing"

func TestParseIssueEvent(t *testing.T) {
	event, err := ParseIssueEvent([]byte(`{
  "issue": {
    "number": 12,
    "body": "### Summary\n\nDo the thing.",
    "labels": [{"name": "bug"}, {"name": "agent:blocked"}]
  },
  "repository": {"full_name": "open-creo/creo"}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Number != 12 || event.Body == "" || event.Repository != "open-creo/creo" {
		t.Fatalf("event = %#v", event)
	}
	if len(event.Labels) != 2 || event.Labels[0] != "bug" || event.Labels[1] != "agent:blocked" {
		t.Fatalf("labels = %#v", event.Labels)
	}
}

func TestParsePullRequestEvent(t *testing.T) {
	pr, err := ParsePullRequestEvent([]byte(`{
  "pull_request": {
    "number": 8,
    "title": "Update policy",
    "body": "Refs #12",
    "base": {"ref": "agent", "repo": {"full_name": "open-creo/creo"}},
    "head": {"ref": "agent-work/policy", "repo": {"full_name": "open-creo/creo"}}
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 8 || pr.BaseRef != "agent" || pr.HeadRef != "agent-work/policy" || pr.Body != "Refs #12" {
		t.Fatalf("pr = %#v", pr)
	}
}
