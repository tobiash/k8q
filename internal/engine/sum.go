package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// SumOptions configures the sum analyzer.
type SumOptions struct {
	Match           MatchOptions
	RequireRequests bool
	RequireLimits   bool
}

// SumFilter returns a Filter that sums CPU and Memory requests from
// matching manifests (looking in Pod templates).
func SumFilter(opts SumOptions) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		totalCPU := resource.NewQuantity(0, resource.DecimalSI)
		totalMem := resource.NewQuantity(0, resource.BinarySI)

		var missingReqs []string

		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, opts.Match) {
				cpu, mem := getPodResources(node)

				// Check for missing resources if required.
				if opts.RequireRequests || opts.RequireLimits {
					if err := checkResources(node, opts.RequireRequests, opts.RequireLimits); err != nil {
						fmt.Fprintf(os.Stderr, "Error: %s/%s: %v\n", meta.Kind, meta.Name, err)
						missingReqs = append(missingReqs, meta.Name) // Track that we had an error
					}
				}

				// Multiply by replicas if present.
				replicas := getReplicas(node)
				for i := 0; i < replicas; i++ {
					totalCPU.Add(cpu)
					totalMem.Add(mem)
				}
			}
		}

		if (opts.RequireRequests || opts.RequireLimits) && len(missingReqs) > 0 {
			return nil, fmt.Errorf("resource requirements check failed")
		}

		fmt.Printf("CPU:    %s\n", totalCPU.String())
		fmt.Printf("Memory: %s\n", totalMem.String())

		return nil, nil // Terminate pipeline
	}
}

func checkResources(node *yaml.RNode, reqReqs, reqLimits bool) error {
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

		return containers.VisitElements(func(container *yaml.RNode) error {
			cname, _ := container.Pipe(yaml.Lookup("name"))
			name := "unknown"
			if cname != nil {
				name = strings.TrimSpace(cname.MustString())
			}

			if reqReqs {
				if r, err := container.Pipe(yaml.Lookup("resources", "requests")); err != nil || yaml.IsMissingOrNull(r) {
					return fmt.Errorf("container %q missing resources.requests", name)
				} else {
					cpu, _ := r.Pipe(yaml.Lookup("cpu"))
					mem, _ := r.Pipe(yaml.Lookup("memory"))
					if yaml.IsMissingOrNull(cpu) || yaml.IsMissingOrNull(mem) {
						return fmt.Errorf("container %q missing cpu or memory requests", name)
					}
				}
			}

			if reqLimits {
				if l, err := container.Pipe(yaml.Lookup("resources", "limits")); err != nil || yaml.IsMissingOrNull(l) {
					return fmt.Errorf("container %q missing resources.limits", name)
				} else {
					cpu, _ := l.Pipe(yaml.Lookup("cpu"))
					mem, _ := l.Pipe(yaml.Lookup("memory"))
					if yaml.IsMissingOrNull(cpu) || yaml.IsMissingOrNull(mem) {
						return fmt.Errorf("container %q missing cpu or memory limits", name)
					}
				}
			}
			return nil
		})
	}
	return nil
}

func getReplicas(node *yaml.RNode) int {
	r, err := node.Pipe(yaml.Lookup("spec", "replicas"))
	if err != nil || yaml.IsMissingOrNull(r) {
		return 1
	}
	val, err := strconv.Atoi(strings.TrimSpace(r.MustString()))
	if err != nil {
		return 1
	}
	return val
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
					if q, err := resource.ParseQuantity(strings.TrimSpace(c.MustString())); err == nil {
						totalCPU.Add(q)
					}
				}
				if m, err := reqs.Pipe(yaml.Lookup("memory")); err == nil && !yaml.IsMissingOrNull(m) {
					if q, err := resource.ParseQuantity(strings.TrimSpace(m.MustString())); err == nil {
						totalMem.Add(q)
					}
				}
			}
			return nil
		})
	}

	return totalCPU, totalMem
}
