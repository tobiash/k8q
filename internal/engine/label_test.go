package engine

import (
	"bytes"
	"strings"
	"testing"
)

func TestLabelFilter(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy1
spec:
  template:
    metadata:
      labels:
        old: val
`

	tests := []struct {
		name      string
		opts      LabelOptions
		wantNames []string // names of resources that SHOULD have the label
	}{
		{
			name: "label everything",
			opts: LabelOptions{
				Label: "new=label",
				Match: MatchOptions{}, // Empty matches all
			},
			wantNames: []string{"cm1", "deploy1"},
		},
		{
			name: "label only deployment",
			opts: LabelOptions{
				Label: "app=web",
				Match: MatchOptions{Kind: "Deployment"},
			},
			wantNames: []string{"deploy1"},
		},
		{
			name: "label by resource kind/name",
			opts: LabelOptions{
				Label: "targeted=true",
				Match: MatchOptions{Resource: "ConfigMap/cm1"},
			},
			wantNames: []string{"cm1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := LabelFilter(tt.opts)
			if err != nil {
				t.Fatalf("LabelFilter() error: %v", err)
			}

			nodes, err := ReadNodes(bytes.NewReader([]byte(input)))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			got, err := f(nodes)
			if err != nil {
				t.Fatalf("filter() error: %v", err)
			}

			key := strings.Split(tt.opts.Label, "=")[0]
			val := strings.Split(tt.opts.Label, "=")[1]

			for _, n := range got {
				meta, _ := n.GetMeta()
				shouldHave := false
				for _, name := range tt.wantNames {
					if meta.Name == name {
						shouldHave = true
						break
					}
				}

				labelVal := meta.Labels[key]
				if shouldHave && labelVal != val {
					t.Errorf("resource %s should have label %s=%s, got %q", meta.Name, key, val, labelVal)
				}
				if !shouldHave && labelVal == val {
					t.Errorf("resource %s should NOT have label %s=%s", meta.Name, key, val)
				}
			}
		})
	}
}
