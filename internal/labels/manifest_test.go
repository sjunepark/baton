package labels

import "testing"

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
}
