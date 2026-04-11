package engine

import (
	"bytes"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetFilter(t *testing.T) {
	raw, err := os.ReadFile("testdata/multi.yaml")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	tests := []struct {
		name      string
		opts      GetOptions
		wantLen   int
		wantNames []string
		wantErr   bool
	}{
		{
			name: "filter by resource kind only",
			opts: GetOptions{Resource: "ConfigMap"},
			wantLen:  1,
			wantNames: []string{"first-cm"},
		},
		{
			name: "filter by resource kind/name",
			opts: GetOptions{Resource: "Deployment/my-app"},
			wantLen:  1,
			wantNames: []string{"my-app"},
		},
		{
			name: "filter by resource group",
			opts: GetOptions{Resource: "apps"},
			wantLen:  1,
			wantNames: []string{"my-app"},
		},
		{
			name: "filter by kind flag",
			opts: GetOptions{Kind: "ConfigMap"},
			wantLen:  1,
			wantNames: []string{"first-cm"},
		},
		{
			name: "filter by name flag",
			opts: GetOptions{Name: "my-app"},
			wantLen:  1,
			wantNames: []string{"my-app"},
		},
		{
			name: "filter by positional and flag (AND)",
			opts: GetOptions{Resource: "default", Kind: "ConfigMap"},
			// positional "default" matches nothing in multi.yaml (no group/kind "default")
			wantLen: 0,
		},
		{
			name: "no matches returns empty",
			opts: GetOptions{Kind: "StatefulSet"},
			wantLen:  0,
		},
		{
			name: "empty options errors",
			opts: GetOptions{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := GetFilter(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			nodes, err := ReadNodes(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			got, err := f(nodes)
			if err != nil {
				t.Fatalf("filter() error: %v", err)
			}

			if len(got) != tt.wantLen {
				t.Fatalf("got %d nodes, want %d", len(got), tt.wantLen)
			}

			if len(tt.wantNames) > 0 {
				var names []string
				for _, n := range got {
					meta, _ := n.GetMeta()
					names = append(names, meta.Name)
				}
				if diff := cmp.Diff(tt.wantNames, names); diff != "" {
					t.Errorf("names mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
