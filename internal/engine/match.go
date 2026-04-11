package engine

import (
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// MatchMode defines how multiple criteria are combined.
type MatchMode int

const (
	// AndMode means ALL provided non-empty criteria must match.
	AndMode MatchMode = iota
	// OrMode means ANY provided non-empty criterion must match.
	OrMode
)

// MatchOptions defines the criteria for matching a Kubernetes resource.
type MatchOptions struct {
	// Resource is a positional filter: "kind", "kind/name", or "api-group".
	Resource  string
	Kind      string
	Name      string
	Namespace string
	Group     string
	Selector  labels.Selector
	Mode      MatchMode
}

// Match returns true if the given resource metadata matches the criteria
// according to the specified Mode.
func Match(meta yaml.ResourceMeta, opts MatchOptions) bool {
	// If no criteria are provided, it's a match-all.
	if opts.Resource == "" && opts.Kind == "" && opts.Name == "" && opts.Namespace == "" &&
		opts.Group == "" && (opts.Selector == nil || opts.Selector.Empty()) {
		return true
	}

	matches := []bool{}

	// Positional resource filter.
	if opts.Resource != "" {
		if strings.Contains(opts.Resource, "/") {
			parts := strings.SplitN(opts.Resource, "/", 2)
			k := parts[0]
			n := parts[1]
			matches = append(matches, strings.EqualFold(meta.Kind, k) && meta.Name == n)
		} else {
			group := apiGroup(meta.APIVersion)
			matches = append(matches, strings.EqualFold(meta.Kind, opts.Resource) || strings.Contains(group, opts.Resource))
		}
	}

	// Flags.
	if opts.Kind != "" {
		matches = append(matches, strings.EqualFold(meta.Kind, opts.Kind))
	}
	if opts.Name != "" {
		matches = append(matches, meta.Name == opts.Name)
	}
	if opts.Namespace != "" {
		matches = append(matches, meta.Namespace == opts.Namespace)
	}
	if opts.Group != "" {
		group := apiGroup(meta.APIVersion)
		matches = append(matches, strings.Contains(group, opts.Group))
	}
	if opts.Selector != nil && !opts.Selector.Empty() {
		matches = append(matches, opts.Selector.Matches(labels.Set(meta.Labels)))
	}

	if opts.Mode == AndMode {
		for _, m := range matches {
			if !m {
				return false
			}
		}
		return true
	}

	// OrMode
	for _, m := range matches {
		if m {
			return true
		}
	}
	return false
}

// apiGroup extracts the group portion from an apiVersion string.
// "group/version" → "group", "v1" → "".
func apiGroup(apiVersion string) string {
	idx := strings.LastIndex(apiVersion, "/")
	if idx < 0 {
		return ""
	}
	return apiGroupOnly(apiVersion[:idx])
}

// apiGroupOnly removes the version if it was part of the group string.
// Some older/custom resources might have complex apiVersions.
// For standard k8s, it's usually group/version.
func apiGroupOnly(group string) string {
	return group
}

// ScopedMutator returns a Filter that applies mutateFn only to nodes that
// match opts, while passing all other nodes through untouched.
func ScopedMutator(opts MatchOptions, mutateFn func(*yaml.RNode) error) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				// If we can't get meta, we can't match, so skip mutation.
				continue
			}
			if Match(meta, opts) {
				if err := mutateFn(node); err != nil {
					return nil, err
				}
			}
		}
		return nodes, nil
	}
}
