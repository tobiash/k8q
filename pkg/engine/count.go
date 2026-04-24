package engine

import (
	"fmt"
	"sort"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// CountResult is the JSON representation of a count analysis.
type CountResult struct {
	Count       int            `json:"count"`
	CountByKind map[string]int `json:"countByKind,omitempty"`
}

// CountOptions configures the count analyzer.
type CountOptions struct {
	GroupByKind bool
	Match       MatchOptions
}

// CountFilter returns a Filter that counts matching manifests.
// Instead of returning YAML, it prints the count to stdout and returns
// an empty slice to terminate the YAML pipeline.
func CountFilter(opts CountOptions) Filter {
	return func(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
		count := 0
		kindCounts := make(map[string]int)

		for _, node := range nodes {
			meta, err := node.GetMeta()
			if err != nil {
				continue
			}

			if Match(meta, opts.Match) {
				count++
				if opts.GroupByKind {
					kindCounts[meta.Kind]++
				}
			}
		}

		if opts.GroupByKind {
			keys := make([]string, 0, len(kindCounts))
			for k := range kindCounts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%s: %d\n", k, kindCounts[k])
			}
		} else {
			fmt.Println(count)
		}

		return nil, nil // Terminate pipeline
	}
}

// CountJSON counts matching manifests and returns a JSON-serializable result.
func CountJSON(nodes []*yaml.RNode, opts CountOptions) (*CountResult, error) {
	count := 0
	kindCounts := make(map[string]int)

	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			continue
		}
		if Match(meta, opts.Match) {
			count++
			if opts.GroupByKind {
				kindCounts[meta.Kind]++
			}
		}
	}
	return &CountResult{Count: count, CountByKind: kindCounts}, nil
}
