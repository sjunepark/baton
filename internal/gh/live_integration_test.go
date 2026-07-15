package gh

import (
	"os"
	"strconv"
	"testing"
)

func liveClient(t *testing.T) (*Client, string) {
	t.Helper()
	if os.Getenv("BATON_LIVE_GITHUB") != "1" {
		t.Skip("set BATON_LIVE_GITHUB=1 to run live GitHub integration tests")
	}
	repo := os.Getenv("BATON_LIVE_GITHUB_REPO")
	if repo == "" {
		t.Skip("set BATON_LIVE_GITHUB_REPO=owner/name")
	}
	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return client, repo
}

func TestLiveIssueLabelReadWrite(t *testing.T) {
	client, repo := liveClient(t)
	if os.Getenv("BATON_LIVE_GITHUB_WRITE") != "1" {
		t.Skip("set BATON_LIVE_GITHUB_WRITE=1 to allow live issue label writes")
	}
	issueNumber := liveInt(t, "BATON_LIVE_GITHUB_ISSUE")
	labelName := os.Getenv("BATON_LIVE_GITHUB_LABEL")
	if labelName == "" {
		labelName = "baton-live-test"
	}
	if err := client.CreateLabel(repo, Label{Name: labelName, Color: "5319E7", Description: "Temporary Baton live integration label."}); err != nil {
		t.Logf("create label returned: %v", err)
	}
	if err := client.AddIssueLabels(repo, issueNumber, []string{labelName}); err != nil {
		t.Fatal(err)
	}
	issue, err := client.GetIssueContext(t.Context(), repo, issueNumber)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLabel(issue.Labels, labelName) {
		t.Fatalf("issue labels = %#v, want %q", issue.Labels, labelName)
	}
	if err := client.RemoveIssueLabel(repo, issueNumber, labelName); err != nil {
		t.Fatal(err)
	}
}

func TestLiveCheckRollupFetch(t *testing.T) {
	client, repo := liveClient(t)
	prNumber := liveInt(t, "BATON_LIVE_GITHUB_PR")
	pr, err := client.GetPullRequest(repo, prNumber)
	if err != nil {
		t.Fatal(err)
	}
	rollup, err := client.GetCheckRollup(repo, pr.Number, pr.HeadSHA)
	if err != nil {
		t.Fatal(err)
	}
	if rollup.Kind != "checkRollup" || rollup.PRNumber != prNumber {
		t.Fatalf("rollup = %#v", rollup)
	}
}

func TestLiveReviewThreadsFetch(t *testing.T) {
	client, repo := liveClient(t)
	prNumber := liveInt(t, "BATON_LIVE_GITHUB_PR")
	threads, err := client.GetReviewThreads(repo, prNumber)
	if err != nil {
		t.Fatal(err)
	}
	if threads.Kind != "reviewThreads" || threads.PRNumber != prNumber {
		t.Fatalf("threads = %#v", threads)
	}
}

func TestLiveBranchHealthFetch(t *testing.T) {
	client, repo := liveClient(t)
	ref := os.Getenv("BATON_LIVE_GITHUB_BRANCH")
	if ref == "" {
		ref = "agent"
	}
	health, err := client.GetBranchHealth(repo, ref)
	if err != nil {
		t.Fatal(err)
	}
	if health == nil || health.Ref != ref {
		t.Fatalf("health = %#v", health)
	}
}

func TestLiveQueueSnapshotFetch(t *testing.T) {
	client, repo := liveClient(t)
	issues, err := client.ListOpenIssues(repo)
	if err != nil {
		t.Fatal(err)
	}
	prs, err := client.ListOpenPullRequests(repo, os.Getenv("BATON_LIVE_GITHUB_BRANCH"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fetched %d issues and %d pull requests", len(issues), len(prs))
}

func liveInt(t *testing.T, name string) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skipf("set %s", name)
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("%s must be an integer: %v", name, err)
	}
	return number
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
