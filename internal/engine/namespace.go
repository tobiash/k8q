package engine

import (
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// NamespaceOptions configures the namespace filter.
type NamespaceOptions struct {
	Namespace string
	Match     MatchOptions
}

// SetNamespaceFilter returns a Filter that overwrites metadata.namespace
// on manifests that match MatchOptions. Non-matching manifests are passed
// through untouched.
func SetNamespaceFilter(opts NamespaceOptions) Filter {
	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		// Ensure metadata map exists.
		metaNode, err := node.Pipe(yaml.LookupCreate(yaml.MappingNode, "metadata"))
		if err != nil {
			return err
		}
		return metaNode.PipeE(yaml.SetField("namespace", yaml.NewScalarRNode(opts.Namespace)))
	})
}
