package engine

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// GetOptions configures the get filter.
type GetOptions struct {
	// Resource is a positional filter: "kind", "kind/name", or "api-group".
	Resource  string
	// Kind, Name, Namespace, Group, and Selector define the filter criteria.
	Kind      string
	Name      string
	Namespace string
	Group     string
	Selector  labels.Selector
}

// GetFilter returns a Filter that keeps only manifests matching the given
// criteria. Manifests must match ALL provided non-empty criteria (AND semantics).
func GetFilter(opts GetOptions) (Filter, error) {
	matchOpts := MatchOptions{
		Resource:  opts.Resource,
		Kind:      opts.Kind,
		Name:      opts.Name,
		Namespace: opts.Namespace,
		Group:     opts.Group,
		Selector:  opts.Selector,
		Mode:      AndMode,
	}

	if matchOpts.Resource == "" && matchOpts.Kind == "" && matchOpts.Name == "" && matchOpts.Namespace == "" &&
		matchOpts.Group == "" && (matchOpts.Selector == nil || matchOpts.Selector.Empty()) {
		return nil, fmt.Errorf("at least one filter criterion is required (resource, kind, name, group, namespace, or selector)")
	}

	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		var out []*yaml.RNode
		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, matchOpts) {
				out = append(out, node)
			}
		}
		return out, nil
	}, nil
}
