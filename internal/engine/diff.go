package engine

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	intdiff "github.com/tobiash/k8q/internal/diff"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// DiffResultJSON is the JSON representation of a manifest diff.
type DiffResultJSON struct {
	Added    []map[string]any  `json:"added"`
	Deleted  []ObjectRef       `json:"deleted"`
	Modified []DiffChangeJSON  `json:"modified"`
}

// DiffChangeJSON represents a modified resource with before/after snapshots.
type DiffChangeJSON struct {
	ObjectRef   ObjectRef      `json:"objectRef"`
	Old         map[string]any `json:"old"`
	New         map[string]any `json:"new"`
	UnifiedDiff string         `json:"unifiedDiff"`
}

type resourceKey struct {
	apiVersion string
	kind       string
	namespace  string
	name       string
}

func (k resourceKey) String() string {
	if k.namespace != "" {
		return fmt.Sprintf("%s/%s (%s)", k.kind, k.name, k.namespace)
	}
	return fmt.Sprintf("%s/%s", k.kind, k.name)
}

func resourceKeyFromMeta(meta yaml.ResourceMeta) resourceKey {
	return resourceKey{
		apiVersion: meta.APIVersion,
		kind:       meta.Kind,
		namespace:  meta.Namespace,
		name:       meta.Name,
	}
}

type ResourceChange struct {
	Key    resourceKey
	Before string
	After  string
	Diff   intdiff.Unified
}

type DiffResult struct {
	Added    []resourceKey
	Removed  []resourceKey
	Modified []ResourceChange
}

func (r *DiffResult) HasChanges() bool {
	return len(r.Added) > 0 || len(r.Removed) > 0 || len(r.Modified) > 0
}

func DiffNodes(before, after []*yaml.RNode) (*DiffResult, error) {
	reorder := ReorderFilter()
	if _, err := reorder(before); err != nil {
		return nil, fmt.Errorf("normalizing before: %w", err)
	}
	if _, err := reorder(after); err != nil {
		return nil, fmt.Errorf("normalizing after: %w", err)
	}

	beforeMap, err := buildResourceMap(before)
	if err != nil {
		return nil, fmt.Errorf("indexing before: %w", err)
	}
	afterMap, err := buildResourceMap(after)
	if err != nil {
		return nil, fmt.Errorf("indexing after: %w", err)
	}

	result := &DiffResult{}

	for key := range afterMap {
		if _, exists := beforeMap[key]; !exists {
			result.Added = append(result.Added, key)
		}
	}
	for key := range beforeMap {
		if _, exists := afterMap[key]; !exists {
			result.Removed = append(result.Removed, key)
		}
	}

	for key, afterNode := range afterMap {
		beforeNode, exists := beforeMap[key]
		if !exists {
			continue
		}

		beforeStr := renderNode(beforeNode)
		afterStr := renderNode(afterNode)

		if beforeStr == afterStr {
			continue
		}

		aLines := splitLines(beforeStr)
		bLines := splitLines(afterStr)
		ops := intdiff.ComputeOps(aLines, bLines)
		u := intdiff.ToUnified(key.String(), key.String(), aLines, bLines, ops)

		result.Modified = append(result.Modified, ResourceChange{
			Key:    key,
			Before: beforeStr,
			After:  afterStr,
			Diff:   u,
		})
	}

	sort.Slice(result.Added, func(i, j int) bool {
		return result.Added[i].String() < result.Added[j].String()
	})
	sort.Slice(result.Removed, func(i, j int) bool {
		return result.Removed[i].String() < result.Removed[j].String()
	})
	sort.Slice(result.Modified, func(i, j int) bool {
		return result.Modified[i].Key.String() < result.Modified[j].Key.String()
	})

	return result, nil
}

// DiffNodesJSON computes a diff and returns a JSON-serializable result.
func DiffNodesJSON(before, after []*yaml.RNode) (*DiffResultJSON, error) {
	result, err := DiffNodes(before, after)
	if err != nil {
		return nil, err
	}

	beforeMap, _ := buildResourceMap(before)
	afterMap, _ := buildResourceMap(after)

	out := &DiffResultJSON{}

	for _, key := range result.Added {
		node := afterMap[key]
		if node == nil {
			continue
		}
		m, _ := node.Map()
		if m != nil {
			out.Added = append(out.Added, m)
		}
	}

	for _, key := range result.Removed {
		out.Deleted = append(out.Deleted, ObjectRef{
			APIVersion: key.apiVersion,
			Kind:       key.kind,
			Name:       key.name,
			Namespace:  key.namespace,
		})
	}

	for _, change := range result.Modified {
		beforeNode := beforeMap[change.Key]
		afterNode := afterMap[change.Key]
		oldMap, _ := beforeNode.Map()
		newMap, _ := afterNode.Map()

		var diffBuf bytes.Buffer
		intdiff.Format(&diffBuf, change.Diff)

		out.Modified = append(out.Modified, DiffChangeJSON{
			ObjectRef: ObjectRef{
				APIVersion: change.Key.apiVersion,
				Kind:       change.Key.kind,
				Name:       change.Key.name,
				Namespace:  change.Key.namespace,
			},
			Old:         oldMap,
			New:         newMap,
			UnifiedDiff: diffBuf.String(),
		})
	}

	return out, nil
}

func buildResourceMap(nodes []*yaml.RNode) (map[resourceKey]*yaml.RNode, error) {
	m := make(map[resourceKey]*yaml.RNode, len(nodes))
	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			continue
		}
		key := resourceKeyFromMeta(meta)
		m[key] = node
	}
	return m, nil
}

func renderNode(node *yaml.RNode) string {
	var buf bytes.Buffer
	writer := &kio.ByteWriter{Writer: &buf}
	if err := writer.Write([]*yaml.RNode{node}); err != nil {
		return ""
	}
	s := buf.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func FormatUnifiedDiff(w io.Writer, result *DiffResult) {
	for _, key := range result.Removed {
		fmt.Fprintf(w, "REMOVED %s\n\n", key)
	}

	for _, key := range result.Added {
		fmt.Fprintf(w, "ADDED %s\n\n", key)
	}

	for _, change := range result.Modified {
		intdiff.Format(w, change.Diff)
		fmt.Fprintln(w)
	}
}

func FormatSummary(w io.Writer, result *DiffResult) {
	for _, key := range sortKeys(result.Removed) {
		fmt.Fprintf(w, "REMOVED  %s\n", key)
	}
	for _, key := range sortKeys(result.Added) {
		fmt.Fprintf(w, "ADDED    %s\n", key)
	}
	for _, change := range result.Modified {
		fmt.Fprintf(w, "MODIFIED %s\n", change.Key)
	}
}

func sortKeys(keys []resourceKey) []resourceKey {
	sorted := make([]resourceKey, len(keys))
	copy(sorted, keys)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].String() < sorted[j].String()
	})
	return sorted
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
