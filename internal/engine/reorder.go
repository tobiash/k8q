package engine

import (
	"sort"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Kubernetes field ordering. Lower number = earlier in output.
// Fields not listed get score 999 (appended at the end in original order).

var topFieldOrder = map[string]int{
	"apiVersion": 0,
	"kind":       1,
	"metadata":   2,
	"spec":       3,
	"data":       4,
	"type":       5,
	"rules":      6,
	"subjects":   7,
	"status":     100,
}

var metadataFieldOrder = map[string]int{
	"name":              0,
	"generateName":      1,
	"namespace":         2,
	"labels":            3,
	"annotations":       4,
	"ownerReferences":   5,
	"finalizers":        6,
	"managedFields":     7,
	"creationTimestamp": 8,
	"uid":               9,
	"resourceVersion":   10,
	"generation":        11,
	"selfLink":          12,
}

// ReorderFilter returns a Filter that reorders fields in each manifest to
// follow Kubernetes conventions: apiVersion, kind, metadata, spec, status, ...
// Unknown fields are preserved at the end in their original order.
func ReorderFilter() Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		for _, node := range nodes {
			reorderMapping(node.YNode(), topFieldOrder)
			// Reorder metadata subfields if present.
			meta, err := node.Pipe(yaml.Lookup("metadata"))
			if err != nil {
				return nil, err
			}
			if meta != nil && !yaml.IsMissingOrNull(meta) {
				reorderMapping(meta.YNode(), metadataFieldOrder)
			}
		}
		return nodes, nil
	}
}

// reorderMapping reorders the Content slice of a mapping node so that keys
// with known ordering appear first (sorted by their order score), followed
// by remaining keys in their original relative order.
func reorderMapping(node *yaml.Node, order map[string]int) {
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content) < 4 {
		return
	}

	type kv struct {
		key   string
		keyN  *yaml.Node
		valN  *yaml.Node
		order int
		orig  int
	}

	pairs := make([]kv, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		o, known := order[key]
		if !known {
			o = 999
		}
		pairs = append(pairs, kv{
			key:   key,
			keyN:  node.Content[i],
			valN:  node.Content[i+1],
			order: o,
			orig:  len(pairs),
		})
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].order != pairs[j].order {
			return pairs[i].order < pairs[j].order
		}
		return pairs[i].orig < pairs[j].orig
	})

	newContent := make([]*yaml.Node, 0, len(node.Content))
	for _, p := range pairs {
		newContent = append(newContent, p.keyN, p.valN)
	}
	node.Content = newContent
}
