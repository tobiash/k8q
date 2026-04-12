package engine

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/labels"
)

const labeledInput = `apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  labels:
    app: web
    env: production
    tier: frontend
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  labels:
    app: api
    env: staging
    tier: backend
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  labels:
    app: web
    env: production
    tier: frontend
---
apiVersion: v1
kind: Secret
metadata:
  name: db-secret
  labels:
    app: api
    env: production
`

func TestGetFilterWithSelector(t *testing.T) {
	raw := []byte(labeledInput)

	tests := []struct {
		name     string
		kind     string
		name_    string
		selector string
		wantLen  int
		wantErr  bool
	}{
		{
			name:     "selector only",
			selector: "app=web",
			wantLen:  2, // web-config + web-app deployment
		},
		{
			name:     "kind and selector combined",
			kind:     "ConfigMap",
			selector: "app=web",
			wantLen:  1, // web-config only
		},
		{
			name:     "kind and name and selector combined",
			kind:     "ConfigMap",
			name_:    "api-config",
			selector: "app=api",
			wantLen:  1,
		},
		{
			name:     "selector with not equals",
			selector: "env!=staging",
			wantLen:  3, // web-config, web-app, db-secret
		},
		{
			name:     "selector with in operator",
			selector: "tier in (frontend,backend)",
			wantLen:  3, // web-config, api-config, web-app
		},
		{
			name:     "selector with notin operator",
			selector: "tier notin (frontend)",
			wantLen:  2, // api-config, db-secret (no tier label = notin matches)
		},
		{
			name:     "selector with exists",
			selector: "tier",
			wantLen:  3, // web-config, api-config, web-app (have tier label)
		},
		{
			name:     "selector with not exists",
			selector: "!tier",
			wantLen:  1, // db-secret (no tier label)
		},
		{
			name:     "neither kind nor selector errors",
			selector: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := ParseSelectorFlag(tt.selector)
			if err != nil {
				t.Fatalf("ParseSelectorFlag() error: %v", err)
			}

			f, err := GetFilter(GetOptions{
				Kind:     tt.kind,
				Name:     tt.name_,
				Selector: sel,
			})
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
				var names []string
				for _, n := range got {
					meta, _ := n.GetMeta()
					names = append(names, meta.Kind+"/"+meta.Name)
				}
				t.Errorf("got %d nodes, want %d; got: %v", len(got), tt.wantLen, names)
			}
		})
	}
}

func TestGetFilterSelectorViaPipeline(t *testing.T) {
	raw := []byte(labeledInput)

	tests := []struct {
		name     string
		selector string
		wantDoc  int
	}{
		{name: "get by label app=web", selector: "app=web", wantDoc: 2},
		{name: "get by label env=production", selector: "env=production", wantDoc: 3},
		{name: "no match", selector: "app=nonexistent", wantDoc: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := ParseSelectorFlag(tt.selector)
			if err != nil {
				t.Fatalf("ParseSelectorFlag() error: %v", err)
			}

			f, err := GetFilter(GetOptions{Selector: sel})
			if err != nil {
				t.Fatalf("GetFilter() error: %v", err)
			}

			in := bytes.NewReader(raw)
			var out bytes.Buffer

			if err := Pipeline(in, &out, f); err != nil {
				t.Fatalf("Pipeline() error: %v", err)
			}

			output := out.String()
			docCount := 0
			if output != "" {
				nodes, err := ReadNodes(bytes.NewReader([]byte(output)))
				if err != nil {
					t.Fatalf("re-parsing output: %v", err)
				}
				docCount = len(nodes)
			}

			if docCount != tt.wantDoc {
				t.Errorf("got %d documents, want %d", docCount, tt.wantDoc)
			}
		})
	}
}

func TestDropFilterWithSelector(t *testing.T) {
	raw := []byte(labeledInput)

	tests := []struct {
		name     string
		group    string
		selector string
		wantLen  int
		wantErr  bool
	}{
		{
			name:     "drop by selector",
			selector: "env=staging",
			wantLen:  3, // everything except api-config
		},
		{
			name:     "drop by group and selector (OR)",
			group:    "apps",
			selector: "app=web",
			wantLen:  2, // api-config + db-secret survive (OR: apps deployment dropped, web-* dropped by selector)
		},
		{
			name:     "neither group nor selector errors",
			group:    "",
			selector: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := ParseSelectorFlag(tt.selector)
			if err != nil {
				t.Fatalf("ParseSelectorFlag() error: %v", err)
			}

			opts := DropOptions{
				Group:    tt.group,
				Selector: sel,
			}

			if tt.wantErr {
				if err := opts.Validate(); err == nil {
					t.Error("expected validation error, got nil")
				}
				return
			}

			nodes, err := ReadNodes(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("ReadNodes() error: %v", err)
			}

			f := DropFilter(opts)
			got, err := f(nodes)
			if err != nil {
				t.Fatalf("filter() error: %v", err)
			}

			if len(got) != tt.wantLen {
				var names []string
				for _, n := range got {
					meta, _ := n.GetMeta()
					names = append(names, meta.Kind+"/"+meta.Name)
				}
				t.Errorf("got %d nodes, want %d; got: %v", len(got), tt.wantLen, names)
			}
		})
	}
}

func TestSelectorFilterStandalone(t *testing.T) {
	raw := []byte(labeledInput)

	sel, err := labels.Parse("app=web,env=production")
	if err != nil {
		t.Fatalf("labels.Parse() error: %v", err)
	}

	in := bytes.NewReader(raw)
	var out bytes.Buffer

	if err := Pipeline(in, &out, SelectorFilter(sel)); err != nil {
		t.Fatalf("Pipeline() error: %v", err)
	}

	nodes, err := ReadNodes(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("re-parsing output: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(nodes))
	}

	for _, n := range nodes {
		meta, _ := n.GetMeta()
		if meta.Labels["app"] != "web" || meta.Labels["env"] != "production" {
			t.Errorf("node %s: labels = %v, want app=web env=production", meta.Name, meta.Labels)
		}
	}
}

func TestSelectorRoundtrip(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep
  labels:
    app: web
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: drop
  labels:
    app: api
`
	sel, _ := labels.Parse("app=web")
	f, _ := GetFilter(GetOptions{Selector: sel})

	in := bytes.NewReader([]byte(input))
	var out bytes.Buffer

	if err := Pipeline(in, &out, f); err != nil {
		t.Fatalf("Pipeline() error: %v", err)
	}

	want := `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep
  labels:
    app: web
`
	if diff := cmp.Diff(want, out.String()); diff != "" {
		t.Errorf("output mismatch (-want +got):\n%s", diff)
	}
}
