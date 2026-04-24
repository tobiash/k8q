package engine

import (
	"fmt"
	"strconv"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ScaleOptions configures the scale filter.
type ScaleOptions struct {
	Replicas string
	Match    MatchOptions
}

// ScaleFilter returns a Filter that updates spec.replicas for matching manifests.
func ScaleFilter(opts ScaleOptions) Filter {
	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		// Only mutate if spec exists.
		spec, err := node.Pipe(yaml.Lookup("spec"))
		if err != nil || yaml.IsMissingOrNull(spec) {
			return nil
		}

		// Ensure we're setting an integer.
		val, err := strconv.Atoi(opts.Replicas)
		if err != nil {
			return fmt.Errorf("replicas must be an integer, got %q", opts.Replicas)
		}

		return spec.PipeE(yaml.SetField("replicas", yaml.NewScalarRNode(strconv.Itoa(val))))
	})
}
