package engine

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// DropOptions configures the drop filter.
type DropOptions struct {
	// Resource is a positional filter: "kind", "kind/name", or "api-group".
	Resource  string
	Kind      string
	Name      string
	Namespace string
	Group     string
	Selector  labels.Selector
}

// DropFilter returns a Filter that removes manifests matching the given
// criteria. Manifests matching ANY provided criterion are dropped
// (OR semantics).
func DropFilter(opts DropOptions) Filter {
	matchOpts := MatchOptions{
		Resource:  opts.Resource,
		Kind:      opts.Kind,
		Name:      opts.Name,
		Namespace: opts.Namespace,
		Group:     opts.Group,
		Selector:  opts.Selector,
		Mode:      OrMode,
	}

	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		var out []*yaml.RNode
		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, matchOpts) {
				continue
			}

			out = append(out, node)
		}
		return out, nil
	}
}

// Validate checks that at least one drop criterion is set.
func (o DropOptions) Validate() error {
	if o.Resource == "" && o.Kind == "" && o.Name == "" && o.Namespace == "" && o.Group == "" &&
		(o.Selector == nil || o.Selector.Empty()) {
		return fmt.Errorf("at least one filter criterion is required (resource, kind, name, group, namespace, or selector)")
	}
	return nil
}
