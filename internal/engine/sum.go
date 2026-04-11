package engine

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// SumOptions configures the sum analyzer.
type SumOptions struct {
	Match MatchOptions
}

// SumFilter returns a Filter that sums CPU and Memory requests from
// matching manifests (looking in Pod templates).
func SumFilter(opts SumOptions) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		totalCPU := resource.NewQuantity(0, resource.DecimalSI)
		totalMem := resource.NewQuantity(0, resource.BinarySI)

		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, opts.Match) {
				cpu, mem := getPodResources(node)
				totalCPU.Add(cpu)
				totalMem.Add(mem)
			}
		}

		fmt.Printf("CPU:    %s\n", totalCPU.String())
		fmt.Printf("Memory: %s\n", totalMem.String())

		return nil, nil // Terminate pipeline
	}
}

func getPodResources(node *yaml.RNode) (cpu, mem resource.Quantity) {
	var totalCPU, totalMem resource.Quantity

	containerPaths := [][]string{
		{"spec", "containers"},
		{"spec", "template", "spec", "containers"},
		{"spec", "jobTemplate", "spec", "template", "spec", "containers"},
	}

	for _, path := range containerPaths {
		containers, err := node.Pipe(yaml.Lookup(path...))
		if err != nil || yaml.IsMissingOrNull(containers) {
			continue
		}

		_ = containers.VisitElements(func(container *yaml.RNode) error {
			// Check requests.
			if reqs, err := container.Pipe(yaml.Lookup("resources", "requests")); err == nil && !yaml.IsMissingOrNull(reqs) {
				if c, err := reqs.Pipe(yaml.Lookup("cpu")); err == nil && !yaml.IsMissingOrNull(c) {
					if q, err := resource.ParseQuantity(c.MustString()); err == nil {
						totalCPU.Add(q)
					}
				}
				if m, err := reqs.Pipe(yaml.Lookup("memory")); err == nil && !yaml.IsMissingOrNull(m) {
					if q, err := resource.ParseQuantity(m.MustString()); err == nil {
						totalMem.Add(q)
					}
				}
			}
			return nil
		})
	}

	return totalCPU, totalMem
}
