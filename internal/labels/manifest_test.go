package labels

import (
	"os"
	"testing"
)

func TestParseManifest(t *testing.T) {
	manifest, err := ParseManifest([]byte(`labels:
  - name: 'agent:ready-trivial'
    color: '0E8A16'
    description: 'Agent may make a narrow, obvious fix.'
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Labels) != 1 {
		t.Fatalf("labels = %#v", manifest.Labels)
	}
	if manifest.Count != 1 || len(manifest.Help) == 0 {
		t.Fatalf("manifest metadata count=%d help=%#v", manifest.Count, manifest.Help)
	}
	label := manifest.Labels[0]
	if label.Name != "agent:ready-trivial" || label.Color != "0E8A16" || label.Description == "" {
		t.Fatalf("label = %#v", label)
	}
	if NormalizeColor("#0e8a16") != "0E8A16" {
		t.Fatal("color normalization failed")
	}
}

func TestPlanSync(t *testing.T) {
	plan := PlanSync("example-org/example-repo",
		[]Label{
			{Name: "bug", Color: "D73A4A", Description: "Confirmed defect."},
			{Name: "enhancement", Color: "A2EEEF", Description: "Feature."},
		},
		[]Label{
			{Name: "bug", Color: "#d73a4a", Description: "Confirmed defect."},
			{Name: "enhancement", Color: "000000", Description: "Old."},
		},
	)
	if len(plan.Changes) != 2 {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	if plan.Changes[0].Action != "ok" || plan.Changes[1].Action != "update" {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	if plan.Count != 2 || plan.Counts.OK != 1 || plan.Counts.Update != 1 {
		t.Fatalf("counts = %#v count=%d", plan.Counts, plan.Count)
	}
	if len(plan.Help) == 0 {
		t.Fatal("plan help should include next command")
	}
}

func TestInstallManifestIncludesPriorityLabels(t *testing.T) {
	content, err := os.ReadFile("../install/templates/.github/labels.yml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(content)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]Label{}
	for _, label := range manifest.Labels {
		labels[label.Name] = label
	}
	for _, name := range []string{"priority:p0", "priority:p1", "priority:p2", "priority:p3"} {
		label, ok := labels[name]
		if !ok {
			t.Fatalf("missing %s in install manifest", name)
		}
		if label.Color == "" || label.Description == "" {
			t.Fatalf("priority label %s lacks color or description: %#v", name, label)
		}
	}
}
