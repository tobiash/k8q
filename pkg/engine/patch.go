package engine

import (
	"fmt"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// PatchOptions configures the patch filter.
type PatchOptions struct {
	Patch string
	Match MatchOptions
}

// PatchFilter returns a Filter that merges a YAML patch into matching manifests.
func PatchFilter(opts PatchOptions) (Filter, error) {
	patchNode, err := yaml.Parse(opts.Patch)
	if err != nil {
		return nil, fmt.Errorf("parsing patch YAML: %w", err)
	}

	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		return recursiveMerge(node, patchNode)
	}), nil
}

func recursiveMerge(dst, src *yaml.RNode) error {
	if src.YNode().Kind != yaml.MappingNode || dst.YNode().Kind != yaml.MappingNode {
		// Non-mapping nodes (scalars, sequences) are simply replaced.
		dst.SetYNode(src.YNode())
		return nil
	}

	fields, err := src.Fields()
	if err != nil {
		return err
	}

	for _, field := range fields {
		srcVal := src.Field(field).Value

		dstVal, err := dst.Pipe(yaml.Lookup(field))
		if err != nil {
			return err
		}

		if yaml.IsMissingOrNull(dstVal) {
			// Field doesn't exist in dst, just set it.
			if err := dst.PipeE(yaml.SetField(field, srcVal)); err != nil {
				return err
			}
			continue
		}

		// Field exists, recurse.
		if err := recursiveMerge(dstVal, srcVal); err != nil {
			return err
		}
	}
	return nil
}
