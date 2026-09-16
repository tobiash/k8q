package engine

import (
	"encoding/json"
	"fmt"
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
	for i, n := range nodes {
		if yaml.IsMissingOrNull(n) || n.YNode().Kind != yaml.MappingNode {
			return fmt.Errorf("resource %d must be a mapping", i+1)
		}
		m, err := n.Map()
		if err != nil {
			return fmt.Errorf("converting resource %d to JSON: %w", i+1, err)
		}
		items = append(items, m)
	}
	list := JSONListEnvelope{APIVersion: "v1", Kind: "List", Items: items}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}
