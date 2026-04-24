package engine

import (
	"bytes"
	"strings"
	"testing"

	intdiff "github.com/tobiash/k8q/internal/diff"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestDiffNodes_Identical(t *testing.T) {
	yaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key: value
`
	before := mustParse(t, yaml)
	after := mustParse(t, yaml)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasChanges() {
		t.Fatalf("expected no changes, got added=%d removed=%d modified=%d",
			len(result.Added), len(result.Removed), len(result.Modified))
	}
}

func TestDiffNodes_AddedResource(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(result.Added))
	}
	if result.Added[0].name != "cm2" {
		t.Fatalf("expected added resource cm2, got %s", result.Added[0].name)
	}
}

func TestDiffNodes_RemovedResource(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(result.Removed))
	}
	if result.Removed[0].name != "cm2" {
		t.Fatalf("expected removed resource cm2, got %s", result.Removed[0].name)
	}
}

func TestDiffNodes_ModifiedResource(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: old-value
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: new-value
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Modified) != 1 {
		t.Fatalf("expected 1 modified, got %d", len(result.Modified))
	}
	change := result.Modified[0]
	if change.Key.name != "my-config" {
		t.Fatalf("expected modified resource my-config, got %s", change.Key.name)
	}

	var buf bytes.Buffer
	intdiff.Format(&buf, change.Diff)
	output := buf.String()
	if !strings.Contains(output, "-  key: old-value") {
		t.Fatalf("expected '-  key: old-value' in diff output, got:\n%s", output)
	}
	if !strings.Contains(output, "+  key: new-value") {
		t.Fatalf("expected '+  key: new-value' in diff output, got:\n%s", output)
	}
}

func TestDiffNodes_ReorderedFields_NoDiff(t *testing.T) {
	before := mustParse(t, `
data:
  key: value
apiVersion: v1
metadata:
  name: my-config
kind: ConfigMap
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasChanges() {
		t.Fatalf("expected no changes for reordered fields, got modified=%d", len(result.Modified))
	}
}

func TestDiffNodes_DifferentDocumentOrder_NoDiff(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-b
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-b
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-a
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasChanges() {
		t.Fatalf("expected no changes for different document order, got changes")
	}
}

func TestDiffNodes_NamespacePartOfIdentity(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: ns-a
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: ns-b
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || len(result.Added) != 1 {
		t.Fatalf("expected 1 removed + 1 added (different namespace = different resource), got removed=%d added=%d",
			len(result.Removed), len(result.Added))
	}
}

func TestDiffNodes_Mixed(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-unchanged
  namespace: default
data:
  key: value
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-modified
  namespace: default
data:
  key: old
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-removed
  namespace: default
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-unchanged
  namespace: default
data:
  key: value
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-modified
  namespace: default
data:
  key: new
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-added
  namespace: default
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 1 || result.Added[0].name != "cm-added" {
		t.Fatalf("expected 1 added (cm-added), got %v", result.Added)
	}
	if len(result.Removed) != 1 || result.Removed[0].name != "cm-removed" {
		t.Fatalf("expected 1 removed (cm-removed), got %v", result.Removed)
	}
	if len(result.Modified) != 1 || result.Modified[0].Key.name != "cm-modified" {
		t.Fatalf("expected 1 modified (cm-modified), got %v", result.Modified)
	}
}

func TestFormatSummary(t *testing.T) {
	before := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-removed
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-modified
data:
  key: old
`)
	after := mustParse(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-added
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-modified
data:
  key: new
`)

	result, err := DiffNodes(before, after)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	FormatSummary(&buf, result)
	output := buf.String()

	if !strings.Contains(output, "ADDED    ConfigMap/cm-added") {
		t.Fatalf("expected ADDED line in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "REMOVED  ConfigMap/cm-removed") {
		t.Fatalf("expected REMOVED line in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "MODIFIED ConfigMap/cm-modified") {
		t.Fatalf("expected MODIFIED line in summary, got:\n%s", output)
	}
}

func TestResourceKey_String(t *testing.T) {
	tests := []struct {
		key      resourceKey
		expected string
	}{
		{resourceKey{kind: "Deployment", name: "app", namespace: "default"}, "Deployment/app (default)"},
		{resourceKey{kind: "ConfigMap", name: "cfg", namespace: ""}, "ConfigMap/cfg"},
	}
	for _, tt := range tests {
		got := tt.key.String()
		if got != tt.expected {
			t.Errorf("resourceKey.String() = %q, want %q", got, tt.expected)
		}
	}
}

func mustParse(t *testing.T, yamlStr string) []*yaml.RNode {
	t.Helper()
	nodes, err := ReadNodes(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return nodes
}
