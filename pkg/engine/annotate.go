package engine

import (
	"fmt"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// AnnotateOptions configures the annotate filter.
type AnnotateOptions struct {
	Annotation string
	Match      MatchOptions
}

// AnnotateFilter returns a Filter that injects an annotation into
// metadata.annotations only for manifests that match opts.
//
// Annotation input format: key=value.
func AnnotateFilter(opts AnnotateOptions) (Filter, error) {
	idx := strings.Index(opts.Annotation, "=")
	if idx < 0 {
		return nil, fmt.Errorf("invalid annotation %q: expected key=value format", opts.Annotation)
	}
	key := opts.Annotation[:idx]
	value := opts.Annotation[idx+1:]

	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		annoNode, err := node.Pipe(yaml.LookupCreate(yaml.MappingNode, "metadata", "annotations"))
		if err != nil {
			return fmt.Errorf("setting annotations on metadata: %w", err)
		}
		if err := annoNode.PipeE(yaml.SetField(key, yaml.NewScalarRNode(value))); err != nil {
			return fmt.Errorf("setting annotation %s: %w", key, err)
		}
		return nil
	}), nil
}
