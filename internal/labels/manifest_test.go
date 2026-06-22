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
