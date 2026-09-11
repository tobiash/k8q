package diff

import (
	"encoding/json"
	"reflect"
	"testing"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestDiffPreservesInputs(t *testing.T) {
	n := yaml.MustParse("metadata:\n  namespace: default\n  name: test\nkind: ConfigMap\napiVersion: v1\n")
	original := n.Copy()
	for _, jsonOutput := range []bool{false, true} {
		if jsonOutput {
			_, err := DiffNodesJSON([]*yaml.RNode{n}, []*yaml.RNode{n})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			_, err := DiffNodes([]*yaml.RNode{n}, []*yaml.RNode{n})
			if err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(n.YNode(), original.YNode()) {
			t.Errorf("DiffNodes(json=%v) changed caller AST", jsonOutput)
		}
	}
}

func TestDiffRejectsInvalidNodes(t *testing.T) {
	valid := yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n")
	badMap := yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\ndata:\n  ? [a, b]\n  : value\n")
	for name, nodes := range map[string][]*yaml.RNode{
		"duplicate":         {valid, valid.Copy()},
		"missing identity":  {yaml.MustParse("kind: ConfigMap\n")},
		"numeric identity":  {yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: 12\n")},
		"invalid namespace": {yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n  namespace: [default]\n")},
		"duplicate fields":  {yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nkind: Secret\nmetadata:\n  name: test\n")},
		"scalar":            {yaml.MustParse("hello")},
		"map conversion":    {badMap},
		"nil":               {nil},
	} {
		t.Run(name, func(t *testing.T) {
			for _, before := range []bool{false, true} {
				var b, a []*yaml.RNode
				if before {
					b = nodes
				} else {
					a = nodes
				}
				if _, err := DiffNodes(b, a); err == nil {
					t.Error("DiffNodes accepted invalid input")
				}
				if _, err := DiffNodesJSON(b, a); err == nil {
					t.Error("DiffNodesJSON accepted invalid input")
				}
			}
		})
	}
}

func TestDiffKnownFieldNormalization(t *testing.T) {
	before := yaml.MustParse("metadata:\n  namespace: default\n  name: test\nkind: ConfigMap\napiVersion: v1\n")
	after := yaml.MustParse("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n  namespace: default\n")
	result, err := DiffNodes([]*yaml.RNode{before}, []*yaml.RNode{after})
	if err != nil {
		t.Fatal(err)
	}
	if result.HasChanges() {
		t.Errorf("known field reordering produced changes: %+v", result)
	}
}

func TestRenderNodeError(t *testing.T) {
	n := yaml.NewRNode(&yaml.Node{Kind: 255})
	if _, err := renderNode(n); err == nil {
		t.Error("renderNode accepted invalid AST kind")
	}
}

func TestDiffSortsFullIdentity(t *testing.T) {
	var nodes []*yaml.RNode
	for _, version := range []string{"z/v1", "a/v2", "a/v1"} {
		nodes = append(nodes, yaml.MustParse("apiVersion: "+version+"\nkind: Thing\nmetadata:\n  name: same\n"))
	}
	for range 30 {
		result, err := DiffNodes(nil, nodes)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, ref := range result.Added {
			got = append(got, ref.APIVersion)
		}
		if !reflect.DeepEqual(got, []string{"a/v1", "a/v2", "z/v1"}) {
			t.Fatalf("sort = %v, want full identity order", got)
		}
	}
}

func TestDiffJSONEmptyArrays(t *testing.T) {
	result, err := DiffNodesJSON(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"added":[],"deleted":[],"modified":[]}` {
		t.Errorf("empty diff = %s, want arrays", data)
	}
}
