package engine

import (
	"bytes"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestPipelinePassthrough(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "single document", input: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"},
		{name: "multi document", input: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: b\n"},
		{name: "empty stream", input: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := bytes.NewReader([]byte(tt.input))
			var out bytes.Buffer

			// No-op filter: passthrough.
			noop := Filter(func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
				return nodes, nil
			})
			err := Pipeline(in, &out, noop)

			if err != nil {
				t.Fatalf("Pipeline() error: %v", err)
			}

			if diff := cmp.Diff(tt.input, out.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadNodes(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantLen  int
		wantKind []string
	}{
		{
			name:     "multi document",
			file:     "testdata/multi.yaml",
			wantLen:  3,
			wantKind: []string{"ConfigMap", "Secret", "Deployment"},
		},
		{
			name:     "single document",
			file:     "testdata/single.yaml",
			wantLen:  1,
			wantKind: []string{"ConfigMap"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("reading testdata: %v", err)
			}

			nodes, err := ReadNodes(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			if len(nodes) != tt.wantLen {
				t.Fatalf("got %d nodes, want %d", len(nodes), tt.wantLen)
			}

			for i, want := range tt.wantKind {
				meta, err := nodes[i].GetMeta()
				if err != nil {
					t.Fatalf("GetMeta() on node %d: %v", i, err)
				}
				if meta.Kind != want {
					t.Errorf("node %d: got kind %q, want %q", i, meta.Kind, want)
				}
			}
		})
	}
}

func TestReadNodesEmpty(t *testing.T) {
	nodes, err := ReadNodes(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("ReadNodes() error: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("got %d nodes from empty input, want 0", len(nodes))
	}
}

func TestWriteNodesRoundtrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/multi.yaml")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	nodes, err := ReadNodes(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadNodes() error: %v", err)
	}

	var out bytes.Buffer
	if err := WriteNodes(&out, nodes); err != nil {
		t.Fatalf("WriteNodes() error: %v", err)
	}

	if diff := cmp.Diff(string(raw), out.String()); diff != "" {
		t.Errorf("roundtrip mismatch (-want +got):\n%s", diff)
	}
}
