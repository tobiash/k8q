package engine

import (
	"bytes"
	"errors"
	"testing"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestWriteJSONListRejectsInvalidNodes(t *testing.T) {
	for _, n := range []*yaml.RNode{yaml.MustParse("scalar"), yaml.MustParse("? [a, b]\n: value\n"), nil} {
		var out bytes.Buffer
		if err := WriteJSONList(&out, []*yaml.RNode{n}); err == nil {
			t.Error("WriteJSONList accepted invalid node")
		}
		if out.Len() != 0 {
			t.Errorf("WriteJSONList wrote partial result: %s", &out)
		}
	}
}

func TestSumJSONQuotedQuantities(t *testing.T) {
	n := yaml.MustParse("apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers:\n  - name: c\n    resources:\n      requests:\n        cpu: '2'\n        memory: '1Mi'\n      limits:\n        cpu: '3'\n        memory: '2Mi'\n")
	result, err := SumJSON([]*yaml.RNode{n}, SumOptions{MaxCPURequests: "1"})
	if !errors.Is(err, ErrAssertion) {
		t.Errorf("SumJSON quoted CPU assertion = %v, want ErrAssertion", err)
	}
	if result == nil {
		t.Fatal("missing sum result")
	}
	if result.Requests.CPU != "2" || result.Requests.Memory != "1Mi" || result.Limits.CPU != "3" || result.Limits.Memory != "2Mi" {
		t.Errorf("SumJSON quoted totals = %+v", result)
	}
}

func TestSumJSONRejectsInvalidThresholds(t *testing.T) {
	for _, opts := range []SumOptions{{MaxCPURequests: "bad"}, {MaxMemRequests: "bad"}, {MaxCPULimits: "bad"}, {MaxMemLimits: "bad"}} {
		if _, err := SumJSON(nil, opts); err == nil {
			t.Errorf("SumJSON(%+v) accepted invalid threshold", opts)
		}
	}
}
