package engine

import (
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// RemoveOptions configures the remove filter.
type RemoveOptions struct {
	Field string
	Match MatchOptions
}

// RemoveFilter returns a Filter that deletes a field from matching manifests.
// The field path is dot-separated (e.g. metadata.managedFields).
func RemoveFilter(opts RemoveOptions) Filter {
	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		path := strings.Split(opts.Field, ".")
		if len(path) == 0 {
			return nil
		}

		parentPath := path[:len(path)-1]
		fieldName := path[len(path)-1]

		var parent *yaml.RNode
		if len(parentPath) == 0 {
			parent = node
		} else {
			var err error
			parent, err = node.Pipe(yaml.Lookup(parentPath...))
			if err != nil {
				return nil // Path not found, nothing to remove
			}
		}

		if yaml.IsMissingOrNull(parent) {
			return nil
		}

		// Only clear if the field actually exists.
		child, err := parent.Pipe(yaml.Lookup(fieldName))
		if err != nil || yaml.IsMissingOrNull(child) {
			return nil
		}

		return parent.PipeE(yaml.Clear(fieldName))
	})
}
