package engine

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// SelectorFilter returns a Filter that keeps only manifests whose labels
// match the given Kubernetes label selector.
func SelectorFilter(sel labels.Selector) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		var out []*yaml.RNode
		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}
			if sel.Matches(labels.Set(meta.Labels)) {
				out = append(out, node)
			}
		}
		return out, nil
	}
}

// ParseSelectorFlag parses a selector string into a labels.Selector.
// Returns nil if the string is empty. Intended for CLI flag parsing.
func ParseSelectorFlag(s string) (labels.Selector, error) {
	if s == "" {
		return labels.Everything(), nil
	}
	sel, err := labels.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid selector %q: %w", s, err)
	}
	return sel, nil
}
