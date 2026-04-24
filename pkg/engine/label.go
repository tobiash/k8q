package engine

import (
	"fmt"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var workloadKinds = map[string]bool{
	"Deployment":  true,
	"DaemonSet":   true,
	"StatefulSet": true,
	"Job":         true,
}

// LabelOptions configures the label filter.
type LabelOptions struct {
	Label string
	Match MatchOptions
}

// LabelFilter returns a Filter that injects a label into metadata.labels
// only for manifests that match opts. For workload kinds (Deployment,
// DaemonSet, StatefulSet, Job), the label is also injected into
// spec.template.metadata.labels.
//
// Label input format: key=value (e.g. app.kubernetes.io/managed-by=k8q).
func LabelFilter(opts LabelOptions) (Filter, error) {
	idx := strings.Index(opts.Label, "=")
	if idx < 0 {
		return nil, fmt.Errorf("invalid label %q: expected key=value format", opts.Label)
	}
	key := opts.Label[:idx]
	value := opts.Label[idx+1:]

	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		labelsNode, err := node.Pipe(yaml.LookupCreate(yaml.MappingNode, "metadata", "labels"))
		if err != nil {
			return fmt.Errorf("setting labels on metadata: %w", err)
		}
		if err := labelsNode.PipeE(yaml.SetField(key, yaml.NewScalarRNode(value))); err != nil {
			return fmt.Errorf("setting label %s: %w", key, err)
		}

		meta, err := node.GetMeta()
		if err != nil {
			return nil // Should not happen after Match check.
		}

		if workloadKinds[meta.Kind] {
			tplLabels, err := node.Pipe(yaml.LookupCreate(yaml.MappingNode, "spec", "template", "metadata", "labels"))
			if err != nil {
				return fmt.Errorf("setting labels on pod template: %w", err)
			}
			if err := tplLabels.PipeE(yaml.SetField(key, yaml.NewScalarRNode(value))); err != nil {
				return fmt.Errorf("setting template label %s: %w", key, err)
			}
		}
		return nil
	}), nil
}
