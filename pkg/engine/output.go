package engine

import (
	"encoding/json"
	"io"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// JSONListEnvelope is the Kubernetes "List" resource envelope used when
// outputting multiple objects as JSON.
type JSONListEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Items      []any  `json:"items"`
}

// WriteJSONList writes nodes as a Kubernetes List envelope JSON to out.
func WriteJSONList(out io.Writer, nodes []*yaml.RNode) error {
	items := make([]any, 0, len(nodes))
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
