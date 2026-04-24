package engine

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8qdiff "github.com/tobiash/k8q/pkg/diff"
	"sigs.k8s.io/kustomize/kyaml/kio"
)

func TestReorderFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "reorders top-level fields",
			input: `kind: ConfigMap
data:
  key: value
apiVersion: v1
metadata:
  labels:
    app: web
  name: test
status:
  phase: Active
`,
			want: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  labels:
    app: web
data:
  key: value
status:
  phase: Active
`,
		},
		{
			name: "reorders deployment",
			input: `spec:
  replicas: 3
metadata:
  labels:
    app: web
  name: my-app
  annotations:
    note: hello
kind: Deployment
apiVersion: apps/v1
status:
  readyReplicas: 3
`,
			want: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  labels:
    app: web
  annotations:
    note: hello
spec:
  replicas: 3
status:
  readyReplicas: 3
`,
		},
		{
			name: "preserves unknown fields at end",
			input: `customField: value
kind: Secret
apiVersion: v1
metadata:
  name: test
type: Opaque
`,
			want: `apiVersion: v1
kind: Secret
metadata:
  name: test
type: Opaque
customField: value
`,
		},
		{
			name: "already ordered stays the same",
			input: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`,
			want: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := bytes.NewReader([]byte(tt.input))
			var out bytes.Buffer

			err := Pipeline(in, &out, kio.FilterFunc(k8qdiff.ReorderFilter()))
			if err != nil {
				t.Fatalf("Pipeline() error: %v", err)
			}

			if diff := cmp.Diff(tt.want, out.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
