package engine

import (
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// RenameOptions configures the rename filter.
type RenameOptions struct {
	Prefix string
	Suffix string
	Match  MatchOptions
}

// RenameFilter returns a Filter that modifies metadata.name for matching manifests.
func RenameFilter(opts RenameOptions) Filter {
	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		meta, err := node.GetMeta()
		if err != nil {
			return nil
		}

		newName := opts.Prefix + meta.Name + opts.Suffix
		if newName == meta.Name {
			return nil
		}

		metaNode, err := node.Pipe(yaml.Lookup("metadata"))
		if err != nil || yaml.IsMissingOrNull(metaNode) {
			return nil
		}

		return metaNode.PipeE(yaml.SetField("name", yaml.NewScalarRNode(newName)))
	})
}
