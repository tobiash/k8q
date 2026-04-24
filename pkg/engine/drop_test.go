package engine

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDropFilter(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: core-resource
  namespace: default
---
apiVersion: toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: flux-ks
  namespace: flux-system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-cert
  namespace: cert-manager
`

	tests := []struct {
		name      string
		opts      DropOptions
		wantKinds []string
	}{
		{
			name:      "drop by resource group",
			opts:      DropOptions{Resource: "toolkit.fluxcd.io"},
			wantKinds: []string{"ConfigMap", "Deployment", "Certificate"},
		},
		{
			name:      "drop by resource kind",
			opts:      DropOptions{Resource: "ConfigMap"},
			wantKinds: []string{"Kustomization", "Deployment", "Certificate"},
		},
		{
			name:      "drop by resource kind/name",
			opts:      DropOptions{Resource: "Deployment/my-app"},
			wantKinds: []string{"ConfigMap", "Kustomization", "Certificate"},
		},
		{
			name:      "drop by group flag",
			opts:      DropOptions{Group: "apps"},
			wantKinds: []string{"ConfigMap", "Kustomization", "Certificate"},
		},
		{
			name:      "drop by multiple criteria (OR)",
			opts:      DropOptions{Kind: "Deployment", Namespace: "flux-system"},
			wantKinds: []string{"ConfigMap", "Certificate"}, // Kustomization (ns) and Deployment (kind) dropped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := DropFilter(tt.opts)

			nodes, err := ReadNodes(bytes.NewReader([]byte(input)))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			got, err := f(nodes)
			if err != nil {
				t.Fatalf("filter() error: %v", err)
			}

			var kinds []string
			for _, n := range got {
				meta, _ := n.GetMeta()
				kinds = append(kinds, meta.Kind)
			}

			if diff := cmp.Diff(tt.wantKinds, kinds); diff != "" {
				t.Errorf("kinds mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
