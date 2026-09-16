package engine

import (
	"errors"
	"strings"
	"testing"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

const sumPod = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers:\n  - name: c\n    resources:\n      requests:\n        cpu: 2\n        memory: 1Mi\n      limits:\n        cpu: 3\n        memory: 2Mi\n"

func TestSumGeneratedName(t *testing.T) {
	node := yaml.MustParse(strings.Replace(sumPod, "name: test", "generateName: worker-", 1))
	result, err := SumJSON([]*yaml.RNode{node}, SumOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Requests != (ResourceTotals{CPU: "2", Memory: "1Mi"}) || result.Limits != (ResourceTotals{CPU: "3", Memory: "2Mi"}) {
		t.Errorf("generated Pod totals = %+v", result)
	}
	for _, match := range []MatchOptions{{Kind: "Deployment"}, {Name: "worker-"}, {Resource: "Pod/worker-"}} {
		result, err := SumJSON([]*yaml.RNode{node}, SumOptions{Match: match})
		if err != nil || result == nil || result.Requests.CPU != "0" {
			t.Errorf("SumJSON match %+v = %+v, %v; want zero totals", match, result, err)
		}
	}
	node = yaml.MustParse(strings.Replace(node.MustString(), "limits:", "unused:", 1))
	result, err = SumJSON([]*yaml.RNode{node}, SumOptions{RequireLimits: true, MaxCPURequests: "1"})
	if !errors.Is(err, ErrAssertion) || result == nil {
		t.Fatalf("generated Pod assertions = %+v, %v; want result and ErrAssertion", result, err)
	}
	if result.Assertions.Passed || !result.Assertions.CPURequestsExceeded || len(result.Assertions.MissingResources) != 1 || !strings.Contains(result.Assertions.MissingResources[0], "Pod/generateName=worker-") {
		t.Errorf("generated Pod assertions = %+v", result.Assertions)
	}
}

func TestSumRejectsInvalidAccounting(t *testing.T) {
	cases := map[string]*yaml.RNode{
		"nil node":         nil,
		"scalar node":      yaml.MustParse("scalar"),
		"missing metadata": yaml.MustParse("apiVersion: v1\nkind: Pod\n"),
		"null metadata":    yaml.MustParse("apiVersion: v1\nkind: Pod\nmetadata: null\n"),
		"scalar metadata":  yaml.MustParse("apiVersion: v1\nkind: Pod\nmetadata: broken\n"),
		"numeric name":     yaml.MustParse(strings.Replace(sumPod, "name: test", "name: 12", 1)),
		"invalid labels":   yaml.MustParse(strings.Replace(sumPod, "name: test", "name: test\n  labels: [broken]", 1)),
		"invalid requests": yaml.MustParse(strings.Replace(sumPod, "requests:\n        cpu: 2\n        memory: 1Mi", "requests: broken", 1)),
		"null container":   yaml.MustParse("apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers: [null]\n"),
		"null containers":  yaml.MustParse("apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers: null\n"),
	}
	for _, value := range []string{"12", "true", "[]", "{}", "null", "''"} {
		cases["generateName="+value] = yaml.MustParse(strings.Replace(sumPod, "name: test", "generateName: "+value, 1))
	}
	for _, value := range []string{"12", "true", "[]", "{}", "null"} {
		cases["name="+value+" with generateName"] = yaml.MustParse(strings.Replace(sumPod, "name: test", "name: "+value+"\n  generateName: worker-", 1))
		cases["generateName="+value+" with name"] = yaml.MustParse(strings.Replace(sumPod, "name: test", "name: test\n  generateName: "+value, 1))
	}
	for _, value := range []string{"bad", "-1", "[]", "{}", "null", "true"} {
		for _, field := range []string{"cpu: 2", "memory: 1Mi", "cpu: 3", "memory: 2Mi"} {
			name, _, _ := strings.Cut(field, ":")
			cases[field+"="+value] = yaml.MustParse(strings.Replace(sumPod, field, name+": "+value, 1))
		}
	}
	for _, value := range []string{"bad", "-1", "[]", "{}", "null", "true", "1.5", "'2'", "999999999999999999999999"} {
		cases["replicas="+value] = yaml.MustParse(strings.Replace(sumPod, "spec:\n", "spec:\n  replicas: "+value+"\n", 1))
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			opts := SumOptions{MaxCPURequests: "100"}
			result, err := SumJSON([]*yaml.RNode{node}, opts)
			if err == nil || errors.Is(err, ErrAssertion) || result != nil {
				t.Errorf("SumJSON invalid input = %+v, %v; want nil result and validation error", result, err)
			}
			if _, err := SumFilter(opts).Filter([]*yaml.RNode{node}); err == nil || errors.Is(err, ErrAssertion) {
				t.Errorf("SumFilter invalid input error = %v, want validation error", err)
			}
		})
	}
}

func TestSumReplicaDefaultsAndMatching(t *testing.T) {
	for _, tt := range []struct{ replicas, cpu string }{{"", "2"}, {"0", "0"}, {"2", "4"}} {
		input := sumPod
		if tt.replicas != "" {
			input = strings.Replace(input, "spec:\n", "spec:\n  replicas: "+tt.replicas+"\n", 1)
		}
		result, err := SumJSON([]*yaml.RNode{yaml.MustParse(input)}, SumOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Requests.CPU != tt.cpu {
			t.Errorf("replicas %q CPU=%s, want %s", tt.replicas, result.Requests.CPU, tt.cpu)
		}
	}
	node := yaml.MustParse(strings.Replace(sumPod, "cpu: 2", "cpu: bad", 1))
	result, err := SumJSON([]*yaml.RNode{node}, SumOptions{Match: MatchOptions{Kind: "Deployment"}})
	if err != nil || result == nil {
		t.Fatalf("nonmatching resource = %+v, %v", result, err)
	}
	if result.Requests.CPU != "0" {
		t.Errorf("nonmatching CPU=%s, want 0", result.Requests.CPU)
	}
}
