package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ErrAssertion marks a valid analysis whose requested assertions failed.
var ErrAssertion = errors.New("assertion failed")

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

// SumAssertions captures threshold and required-resource assertion results.
type SumAssertions struct {
	Passed                  bool     `json:"passed"`
	MissingResources        []string `json:"missingResources,omitempty"`
	CPURequestsExceeded     bool     `json:"cpuRequestsExceeded"`
	MemoryRequestsExceeded  bool     `json:"memoryRequestsExceeded"`
	CPULimitsExceeded       bool     `json:"cpuLimitsExceeded"`
	MemoryLimitsExceeded    bool     `json:"memoryLimitsExceeded"`
	CPURequestsThreshold    string   `json:"cpuRequestsThreshold,omitempty"`
	MemoryRequestsThreshold string   `json:"memoryRequestsThreshold,omitempty"`
	CPULimitsThreshold      string   `json:"cpuLimitsThreshold,omitempty"`
	MemoryLimitsThreshold   string   `json:"memoryLimitsThreshold,omitempty"`
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
		result, err := SumJSON(nodes, opts)
		if result == nil {
			return nil, err
		}
		reqMem := resource.MustParse(result.Requests.Memory)
		limMem := resource.MustParse(result.Limits.Memory)
		fmt.Println("Requests:")
		fmt.Printf("  CPU:    %s\n", result.Requests.CPU)
		fmt.Printf("  Memory: %s\n", formatMemory(&reqMem))
		fmt.Println("Limits:")
		fmt.Printf("  CPU:    %s\n", result.Limits.CPU)
		fmt.Printf("  Memory: %s\n", formatMemory(&limMem))
		return nil, err // Terminate pipeline.
	}
}

// SumJSON computes resource totals and returns a JSON-serializable result.
// Failed assertions return both the result and an error wrapping ErrAssertion.
// Invalid thresholds or accounting inputs return a nil result and a validation error.
func SumJSON(nodes []*yaml.RNode, opts SumOptions) (*SumResult, error) {
	if err := opts.validateThresholds(); err != nil {
		return nil, err
	}
	reqCPU := resource.NewQuantity(0, resource.DecimalSI)
	reqMem := resource.NewQuantity(0, resource.BinarySI)
	limCPU := resource.NewQuantity(0, resource.DecimalSI)
	limMem := resource.NewQuantity(0, resource.BinarySI)

	var missing []string

	for i, node := range nodes {
		meta, err := sumResourceMeta(node)
		if err != nil {
			return nil, fmt.Errorf("resource %d metadata: %w", i+1, err)
		}
		if Match(meta, opts.Match) {
			if meta.Name == "" {
				generated, _ := node.Pipe(yaml.Lookup("metadata", "generateName")) // Validated by sumResourceMeta.
				meta.Name = "generateName=" + generated.YNode().Value
			}
			r, l, err := getPodResources(node)
			if err != nil {
				return nil, fmt.Errorf("%s/%s resources: %w", meta.Kind, meta.Name, err)
			}
			replicas, err := getReplicas(node)
			if err != nil {
				return nil, fmt.Errorf("%s/%s replicas: %w", meta.Kind, meta.Name, err)
			}
			if err := checkResources(node, opts.RequireRequests, opts.RequireLimits); err != nil {
				missing = append(missing, fmt.Sprintf("%s/%s: %v", meta.Kind, meta.Name, err))
			}
			for i := 0; i < replicas; i++ {
				reqCPU.Add(r.cpu)
				reqMem.Add(r.mem)
				limCPU.Add(l.cpu)
				limMem.Add(l.mem)
			}
		}
	}

	assertions, assertErr := buildAssertions(reqCPU, reqMem, limCPU, limMem, opts)
	assertions.MissingResources = missing
	if len(missing) > 0 {
		assertErr = errors.Join(assertErr, fmt.Errorf("resource requirements check failed: %s", strings.Join(missing, "; ")))
	}
	assertions.Passed = assertErr == nil

	result := &SumResult{
		Requests: ResourceTotals{CPU: reqCPU.String(), Memory: reqMem.String()},
		Limits:   ResourceTotals{CPU: limCPU.String(), Memory: limMem.String()},
	}
	if opts.RequireRequests || opts.RequireLimits || assertions.hasThresholds() {
		result.Assertions = assertions
	}

	if assertErr != nil {
		return result, fmt.Errorf("%w: %w", ErrAssertion, assertErr)
	}
	return result, assertErr
}

func sumResourceMeta(node *yaml.RNode) (yaml.ResourceMeta, error) { //nolint:gocyclo // Keep metadata shape, type and identity validation together.
	var meta yaml.ResourceMeta
	if yaml.IsMissingOrNull(node) || node.YNode().Kind != yaml.MappingNode {
		return meta, fmt.Errorf("resource must be a mapping")
	}
	metadata, err := node.Pipe(yaml.Lookup("metadata"))
	if err != nil {
		return meta, err
	}
	if yaml.IsMissingOrNull(metadata) || metadata.YNode().Kind != yaml.MappingNode {
		return meta, fmt.Errorf("metadata must be a mapping")
	}
	// Decode through JSON to reject wrong field types rather than coercing
	// them into strings or silently dropping malformed labels during matching.
	data, err := node.MarshalJSON()
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	name, err := metadata.Pipe(yaml.Lookup("name"))
	if err != nil {
		return meta, err
	}
	if name != nil && name.YNode().Tag == "!!null" {
		return meta, fmt.Errorf("metadata.name must be a string")
	}
	generated, err := metadata.Pipe(yaml.Lookup("generateName"))
	if err != nil {
		return meta, err
	}
	if generated != nil && (generated.YNode().Kind != yaml.ScalarNode || generated.YNode().Tag != "!!str") {
		return meta, fmt.Errorf("metadata.generateName must be a string")
	}
	if meta.APIVersion == "" || meta.Kind == "" || (meta.Name == "" && (generated == nil || generated.YNode().Value == "")) {
		return meta, fmt.Errorf("apiVersion, kind and metadata.name or metadata.generateName are required")
	}
	return meta, nil
}

func (opts SumOptions) validateThresholds() error {
	for _, threshold := range []struct{ name, value string }{
		{"max-cpu-requests", opts.MaxCPURequests}, {"max-mem-requests", opts.MaxMemRequests},
		{"max-cpu-limits", opts.MaxCPULimits}, {"max-mem-limits", opts.MaxMemLimits},
	} {
		if threshold.value == "" {
			continue
		}
		q, err := resource.ParseQuantity(threshold.value)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", threshold.name, err)
		}
		if q.Sign() < 0 {
			return fmt.Errorf("%s must be non-negative", threshold.name)
		}
	}
	return nil
}

func (a *SumAssertions) hasThresholds() bool {
	return a.CPURequestsThreshold != "" || a.MemoryRequestsThreshold != "" || a.CPULimitsThreshold != "" || a.MemoryLimitsThreshold != ""
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
		return false, thresholdStr, fmt.Errorf("invalid threshold: %w", err)
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

func getReplicas(node *yaml.RNode) (int, error) {
	r, err := node.Pipe(yaml.Lookup("spec", "replicas"))
	if err != nil {
		return 0, err
	}
	if r == nil {
		return 1, nil
	}
	if r.YNode().Kind != yaml.ScalarNode || r.YNode().Tag != "!!int" {
		return 0, fmt.Errorf("spec.replicas must be an integer")
	}
	val, err := strconv.ParseInt(r.YNode().Value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid spec.replicas: %w", err)
	}
	if val < 0 {
		return 0, fmt.Errorf("spec.replicas must be non-negative")
	}
	return int(val), nil
}

func getPodResources(node *yaml.RNode) (req, lim resourcePair, err error) {
	containerPaths := [][]string{
		{"spec", "containers"},
		{"spec", "template", "spec", "containers"},
		{"spec", "jobTemplate", "spec", "template", "spec", "containers"},
	}

	for _, path := range containerPaths {
		containers, err := node.Pipe(yaml.Lookup(path...))
		if err != nil {
			return req, lim, err
		}
		if containers == nil {
			continue
		}
		if containers.YNode().Kind != yaml.SequenceNode {
			return req, lim, fmt.Errorf("%s must be a sequence", strings.Join(path, "."))
		}

		err = containers.VisitElements(func(container *yaml.RNode) error {
			if err := addContainerResources(container, "requests", &req); err != nil {
				return err
			}
			return addContainerResources(container, "limits", &lim)
		})
		if err != nil {
			return req, lim, fmt.Errorf("%s: %w", strings.Join(path, "."), err)
		}
	}

	return req, lim, nil
}

func addContainerResources(container *yaml.RNode, category string, totals *resourcePair) error {
	if yaml.IsMissingOrNull(container) || container.YNode().Kind != yaml.MappingNode {
		return fmt.Errorf("container must be a mapping")
	}
	resources, err := container.Pipe(yaml.Lookup("resources", category))
	if err != nil {
		return err
	}
	if resources == nil {
		return nil
	}
	if resources.YNode().Kind != yaml.MappingNode {
		return fmt.Errorf("resources.%s must be a mapping", category)
	}
	for _, field := range []struct {
		name  string
		total *resource.Quantity
	}{{"cpu", &totals.cpu}, {"memory", &totals.mem}} {
		n, err := resources.Pipe(yaml.Lookup(field.name))
		if err != nil {
			return err
		}
		if n == nil {
			continue
		}
		value := n.YNode()
		if value.Kind != yaml.ScalarNode || (value.Tag != "!!str" && value.Tag != "!!int" && value.Tag != "!!float") {
			return fmt.Errorf("resources.%s.%s must be a quantity string or number", category, field.name)
		}
		q, err := resource.ParseQuantity(value.Value)
		if err != nil {
			return fmt.Errorf("invalid resources.%s.%s: %w", category, field.name, err)
		}
		if q.Sign() < 0 {
			return fmt.Errorf("resources.%s.%s must be non-negative", category, field.name)
		}
		field.total.Add(q)
	}
	return nil
}
