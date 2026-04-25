package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// SumResult is the JSON representation of a resource sum analysis.
type SumResult struct {
	Requests   ResourceTotals `json:"requests"`
	Limits     ResourceTotals `json:"limits"`
	Assertions *SumAssertions `json:"assertions,omitempty"`
}

// ResourceTotals holds CPU and Memory totals using Kubernetes Quantity strings.
type ResourceTotals struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// SumAssertions captures threshold assertion results.
type SumAssertions struct {
	CPURequestsExceeded     bool   `json:"cpuRequestsExceeded"`
	MemoryRequestsExceeded  bool   `json:"memoryRequestsExceeded"`
	CPULimitsExceeded       bool   `json:"cpuLimitsExceeded"`
	MemoryLimitsExceeded    bool   `json:"memoryLimitsExceeded"`
	CPURequestsThreshold    string `json:"cpuRequestsThreshold,omitempty"`
	MemoryRequestsThreshold string `json:"memoryRequestsThreshold,omitempty"`
	CPULimitsThreshold      string `json:"cpuLimitsThreshold,omitempty"`
	MemoryLimitsThreshold   string `json:"memoryLimitsThreshold,omitempty"`
}

// SumOptions configures the sum analyzer.
type SumOptions struct {
	Match           MatchOptions
	RequireRequests bool
	RequireLimits   bool

	// Thresholds for assertions.
	MaxCPURequests string
	MaxMemRequests string
	MaxCPULimits   string
	MaxMemLimits   string
}

// SumFilter returns a Filter that sums CPU and Memory requests/limits from
// matching manifests.
func SumFilter(opts SumOptions) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		reqCPU := resource.NewQuantity(0, resource.DecimalSI)
		reqMem := resource.NewQuantity(0, resource.BinarySI)
		limCPU := resource.NewQuantity(0, resource.DecimalSI)
		limMem := resource.NewQuantity(0, resource.BinarySI)

		var missingCount int

		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, opts.Match) {
				// Check for missing resources if required.
				if opts.RequireRequests || opts.RequireLimits {
					if err := checkResources(node, opts.RequireRequests, opts.RequireLimits); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "Error: %s/%s: %v\n", meta.Kind, meta.Name, err)
						missingCount++
					}
				}

				r, l := getPodResources(node)
				replicas := getReplicas(node)

				for i := 0; i < replicas; i++ {
					reqCPU.Add(r.cpu)
					reqMem.Add(r.mem)
					limCPU.Add(l.cpu)
					limMem.Add(l.mem)
				}
			}
		}

		if (opts.RequireRequests || opts.RequireLimits) && missingCount > 0 {
			return nil, fmt.Errorf("resource requirements check failed for %d resources", missingCount)
		}

		fmt.Println("Requests:")
		fmt.Printf("  CPU:    %s\n", reqCPU.String())
		fmt.Printf("  Memory: %s\n", formatMemory(reqMem))
		fmt.Println("Limits:")
		fmt.Printf("  CPU:    %s\n", limCPU.String())
		fmt.Printf("  Memory: %s\n", formatMemory(limMem))

		// Assertions.
		if err := assertThreshold("CPU Requests", reqCPU, opts.MaxCPURequests); err != nil {
			return nil, err
		}
		if err := assertThreshold("Memory Requests", reqMem, opts.MaxMemRequests); err != nil {
			return nil, err
		}
		if err := assertThreshold("CPU Limits", limCPU, opts.MaxCPULimits); err != nil {
			return nil, err
		}
		if err := assertThreshold("Memory Limits", limMem, opts.MaxMemLimits); err != nil {
			return nil, err
		}

		return nil, nil // Terminate pipeline
	}
}

// SumJSON computes resource totals and returns a JSON-serializable result
// together with any assertion errors.
func SumJSON(nodes []*yaml.RNode, opts SumOptions) (*SumResult, error) {
	reqCPU := resource.NewQuantity(0, resource.DecimalSI)
	reqMem := resource.NewQuantity(0, resource.BinarySI)
	limCPU := resource.NewQuantity(0, resource.DecimalSI)
	limMem := resource.NewQuantity(0, resource.BinarySI)

	var missingCount int

	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			continue
		}
		if Match(meta, opts.Match) {
			if opts.RequireRequests || opts.RequireLimits {
				if err := checkResources(node, opts.RequireRequests, opts.RequireLimits); err != nil {
					missingCount++
				}
			}
			r, l := getPodResources(node)
			replicas := getReplicas(node)
			for i := 0; i < replicas; i++ {
				reqCPU.Add(r.cpu)
				reqMem.Add(r.mem)
				limCPU.Add(l.cpu)
				limMem.Add(l.mem)
			}
		}
	}

	if (opts.RequireRequests || opts.RequireLimits) && missingCount > 0 {
		return nil, fmt.Errorf("resource requirements check failed for %d resources", missingCount)
	}

	assertions, assertErr := buildAssertions(reqCPU, reqMem, limCPU, limMem, opts)

	result := &SumResult{
		Requests: ResourceTotals{CPU: reqCPU.String(), Memory: reqMem.String()},
		Limits:   ResourceTotals{CPU: limCPU.String(), Memory: limMem.String()},
	}
	if assertions.HasAny() {
		result.Assertions = assertions
	}

	return result, assertErr
}

// HasAny reports whether any threshold assertion was exceeded.
func (a *SumAssertions) HasAny() bool {
	if a == nil {
		return false
	}
	return a.CPURequestsExceeded || a.MemoryRequestsExceeded || a.CPULimitsExceeded || a.MemoryLimitsExceeded
}

func evaluateThreshold(actual *resource.Quantity, thresholdStr string) (exceeded bool, threshold string, overErr error) {
	if thresholdStr == "" {
		return false, "", nil
	}
	q, err := resource.ParseQuantity(thresholdStr)
	if err != nil {
		return false, thresholdStr, nil
	}
	if actual.Cmp(q) > 0 {
		return true, thresholdStr, fmt.Errorf("got %s, max %s", actual.String(), q.String())
	}
	return false, thresholdStr, nil
}

func buildAssertions(reqCPU, reqMem, limCPU, limMem *resource.Quantity, opts SumOptions) (*SumAssertions, error) {
	a := &SumAssertions{}
	var firstErr error

	exceeded, thr, err := evaluateThreshold(reqCPU, opts.MaxCPURequests)
	a.CPURequestsExceeded = exceeded
	a.CPURequestsThreshold = thr
	if err != nil {
		firstErr = fmt.Errorf("CPU requests threshold exceeded: %w", err)
	}

	exceeded, thr, err = evaluateThreshold(reqMem, opts.MaxMemRequests)
	a.MemoryRequestsExceeded = exceeded
	a.MemoryRequestsThreshold = thr
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("memory requests threshold exceeded: %w", err)
	}

	exceeded, thr, err = evaluateThreshold(limCPU, opts.MaxCPULimits)
	a.CPULimitsExceeded = exceeded
	a.CPULimitsThreshold = thr
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("CPU limits threshold exceeded: %w", err)
	}

	exceeded, thr, err = evaluateThreshold(limMem, opts.MaxMemLimits)
	a.MemoryLimitsExceeded = exceeded
	a.MemoryLimitsThreshold = thr
	if err != nil && firstErr == nil {
		firstErr = fmt.Errorf("memory limits threshold exceeded: %w", err)
	}

	return a, firstErr
}

func assertThreshold(label string, actual *resource.Quantity, thresholdStr string) error {
	if thresholdStr == "" {
		return nil
	}
	threshold, err := resource.ParseQuantity(thresholdStr)
	if err != nil {
		return fmt.Errorf("invalid threshold for %s: %w", label, err)
	}

	if actual.Cmp(threshold) > 0 {
		actualStr := actual.String()
		if strings.Contains(label, "Memory") {
			actualStr = formatMemory(actual)
		}
		return fmt.Errorf("%s threshold exceeded: got %s, max %s", label, actualStr, threshold.String())
	}
	return nil
}

// formatMemory ensures memory is printed in a sane unit (fallback to Gi/Mi if large).
func formatMemory(q *resource.Quantity) string {
	val := q.Value() // bytes
	if val == 0 {
		return "0"
	}

	// Prefer Gi if >= 1Gi
	if val >= 1024*1024*1024 {
		return fmt.Sprintf("%.2f Gi", float64(val)/(1024*1024*1024))
	}
	// Prefer Mi if >= 1Mi
	if val >= 1024*1024 {
		return fmt.Sprintf("%.2f Mi", float64(val)/(1024*1024))
	}
	return q.String()
}

type resourcePair struct {
	cpu resource.Quantity
	mem resource.Quantity
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

//nolint:gocyclo
func getPodResources(node *yaml.RNode) (req, lim resourcePair) {
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
			// Requests
			if r, err := container.Pipe(yaml.Lookup("resources", "requests")); err == nil && !yaml.IsMissingOrNull(r) {
				if c, err := r.Pipe(yaml.Lookup("cpu")); err == nil && !yaml.IsMissingOrNull(c) {
					if q, err := resource.ParseQuantity(strings.TrimSpace(c.MustString())); err == nil {
						req.cpu.Add(q)
					}
				}
				if m, err := r.Pipe(yaml.Lookup("memory")); err == nil && !yaml.IsMissingOrNull(m) {
					if q, err := resource.ParseQuantity(strings.TrimSpace(m.MustString())); err == nil {
						req.mem.Add(q)
					}
				}
			}
			// Limits
			if l, err := container.Pipe(yaml.Lookup("resources", "limits")); err == nil && !yaml.IsMissingOrNull(l) {
				if c, err := l.Pipe(yaml.Lookup("cpu")); err == nil && !yaml.IsMissingOrNull(c) {
					if q, err := resource.ParseQuantity(strings.TrimSpace(c.MustString())); err == nil {
						lim.cpu.Add(q)
					}
				}
				if m, err := l.Pipe(yaml.Lookup("memory")); err == nil && !yaml.IsMissingOrNull(m) {
					if q, err := resource.ParseQuantity(strings.TrimSpace(m.MustString())); err == nil {
						lim.mem.Add(q)
					}
				}
			}
			return nil
		})
	}

	return req, lim
}
