package engine

import (
	"bytes"
	"testing"
)

func TestSetNamespaceFilter(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: ns1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
  namespace: ns2
`

	tests := []struct {
		name      string
		opts      NamespaceOptions
		wantNames []string // names of resources that SHOULD have the NEW namespace
	}{
		{
			name: "set all namespaces",
			opts: NamespaceOptions{
				Namespace: "new-ns",
				Match:     MatchOptions{},
			},
			wantNames: []string{"cm1", "cm2"},
		},
		{
			name: "set only for specific name",
			opts: NamespaceOptions{
				Namespace: "targeted-ns",
				Match:     MatchOptions{Name: "cm1"},
			},
			wantNames: []string{"cm1"},
		},
		{
			name: "set only for old namespace",
			opts: NamespaceOptions{
				Namespace: "migrated-ns",
				Match:     MatchOptions{Namespace: "ns1"},
			},
			wantNames: []string{"cm1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := SetNamespaceFilter(tt.opts)

			nodes, err := ReadNodes(bytes.NewReader([]byte(input)))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			got, err := f(nodes)
			if err != nil {
				t.Fatalf("filter() error: %v", err)
			}

			for _, n := range got {
				meta, _ := n.GetMeta()
				shouldHave := false
				for _, name := range tt.wantNames {
					if meta.Name == name {
						shouldHave = true
						break
					}
				}

				if shouldHave && meta.Namespace != tt.opts.Namespace {
					t.Errorf("resource %s should have namespace %q, got %q", meta.Name, tt.opts.Namespace, meta.Namespace)
				}
				if !shouldHave && meta.Namespace == tt.opts.Namespace {
					t.Errorf("resource %s should NOT have namespace %q", meta.Name, tt.opts.Namespace)
				}
			}
		})
	}
}
