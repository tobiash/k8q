package engine

import (
	"encoding/json"
	"io"

	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ObjectRef mirrors the Kubernetes ObjectReference shape used in Events,
// OwnerReferences, and other cross-resource references.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

// JSONListEnvelope is the Kubernetes "List" resource envelope used when
// outputting multiple objects as JSON.
type JSONListEnvelope struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Items      []interface{} `json:"items"`
}

// WriteJSONList writes nodes as a Kubernetes List envelope JSON to out.
func WriteJSONList(out io.Writer, nodes []*yaml.RNode) error {
	items := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		m, err := n.Map()
		if err != nil {
			continue
		}
		items = append(items, m)
	}
	list := JSONListEnvelope{APIVersion: "v1", Kind: "List", Items: items}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}

// RunPipelineJSON executes the filter chain, then writes the result as a
// JSON List envelope instead of YAML.
func RunPipelineJSON(in io.Reader, out io.Writer, filters ...kio.Filter) error {
	nodes, err := ReadNodes(in)
	if err != nil {
		return err
	}
	for _, f := range filters {
		nodes, err = f.Filter(nodes)
		if err != nil {
			return err
		}
	}
	return WriteJSONList(out, nodes)
}
