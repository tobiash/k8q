package engine

import (
	"fmt"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// SetImageOptions configures the set-image filter.
type SetImageOptions struct {
	Image string
	Match MatchOptions
}

// SetImageFilter returns a Filter that updates container images within
// matching manifests. It looks for container lists in spec.template.spec
// (for workloads) or top-level spec (for Pods).
//
// Image input format: name=image:tag.
func SetImageFilter(opts SetImageOptions) (Filter, error) {
	idx := strings.Index(opts.Image, "=")
	if idx < 0 {
		return nil, fmt.Errorf("invalid image %q: expected name=image:tag format", opts.Image)
	}
	containerName := opts.Image[:idx]
	newImage := opts.Image[idx+1:]

	return ScopedMutator(opts.Match, func(node *yaml.RNode) error {
		// List of paths where containers might live.
		containerPaths := [][]string{
			{"spec", "containers"},           // Pod
			{"spec", "template", "spec", "containers"}, // Deployment, etc.
			{"spec", "jobTemplate", "spec", "template", "spec", "containers"}, // CronJob
		}

		for _, path := range containerPaths {
			containers, err := node.Pipe(yaml.Lookup(path...))
			if err != nil || yaml.IsMissingOrNull(containers) {
				continue
			}

			err = containers.VisitElements(func(container *yaml.RNode) error {
				name, err := container.Pipe(yaml.Lookup("name"))
				if err != nil || yaml.IsMissingOrNull(name) {
					return nil
				}
				if strings.TrimSpace(name.MustString()) == containerName {
					return container.PipeE(yaml.SetField("image", yaml.NewScalarRNode(newImage)))
				}
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	}), nil
}
