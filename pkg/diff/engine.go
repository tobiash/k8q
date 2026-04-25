package diff

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ObjectRef mirrors the Kubernetes ObjectReference shape used in Events,
// OwnerReferences, and other cross-resource references.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

// String returns a human-readable identifier for the resource.
func (o ObjectRef) String() string {
	if o.Namespace != "" {
		return fmt.Sprintf("%s/%s (%s)", o.Kind, o.Name, o.Namespace)
	}
	return fmt.Sprintf("%s/%s", o.Kind, o.Name)
}

// ResourceChange describes a single modified resource.
type ResourceChange struct {
	Key    ObjectRef
	Before string
	After  string
	Diff   gotextdiff.Unified
}

// DiffResult holds the structured result of a diff between two manifest sets.
//
//nolint:revive // exported API name; renaming would break consumers
type DiffResult struct {
	Added    []ObjectRef
	Removed  []ObjectRef
	Modified []ResourceChange
}

// HasChanges reports whether any resources were added, removed, or modified.
func (r *DiffResult) HasChanges() bool {
	return len(r.Added) > 0 || len(r.Removed) > 0 || len(r.Modified) > 0
}

// DiffNodes computes a semantic diff between two sets of Kubernetes manifests.
// Resources are matched by identity (apiVersion + kind + namespace + name).
//
//nolint:revive // exported API name; renaming would break consumers
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

		u := computeDiff(key.String(), beforeStr, afterStr)

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

func buildResourceMap(nodes []*yaml.RNode) (map[ObjectRef]*yaml.RNode, error) {
	m := make(map[ObjectRef]*yaml.RNode, len(nodes))
	for _, node := range nodes {
		meta, err := node.GetMeta()
		if err != nil {
			return nil, fmt.Errorf("reading resource metadata: %w", err)
		}
		key := ObjectRefFromMeta(meta)
		m[key] = node
	}
	return m, nil
}

// ObjectRefFromMeta builds an ObjectRef from kyaml ResourceMeta.
func ObjectRefFromMeta(meta yaml.ResourceMeta) ObjectRef {
	return ObjectRef{
		APIVersion: meta.APIVersion,
		Kind:       meta.Kind,
		Namespace:  meta.Namespace,
		Name:       meta.Name,
	}
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

// DiffResultJSON is the JSON representation of a manifest diff.
//
//nolint:revive // exported API name; renaming would break consumers
type DiffResultJSON struct {
	Added    []map[string]any `json:"added"`
	Deleted  []ObjectRef      `json:"deleted"`
	Modified []DiffChangeJSON `json:"modified"`
}

// DiffChangeJSON represents a modified resource with before/after snapshots.
//
//nolint:revive // exported API name; renaming would break consumers
type DiffChangeJSON struct {
	ObjectRef   ObjectRef      `json:"objectRef"`
	Old         map[string]any `json:"old"`
	New         map[string]any `json:"new"`
	UnifiedDiff string         `json:"unifiedDiff"`
}

// DiffNodesJSON computes a diff and returns a JSON-serializable result.
//
//nolint:revive // exported API name; renaming would break consumers
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
			APIVersion: key.APIVersion,
			Kind:       key.Kind,
			Name:       key.Name,
			Namespace:  key.Namespace,
		})
	}

	for _, change := range result.Modified {
		beforeNode := beforeMap[change.Key]
		afterNode := afterMap[change.Key]
		oldMap, _ := beforeNode.Map()
		newMap, _ := afterNode.Map()

		var diffBuf bytes.Buffer
		formatUnified(&diffBuf, change.Diff)

		out.Modified = append(out.Modified, DiffChangeJSON{
			ObjectRef:   change.Key,
			Old:         oldMap,
			New:         newMap,
			UnifiedDiff: diffBuf.String(),
		})
	}

	return out, nil
}

// computeDiff computes a Myers diff between two multi-line strings.
func computeDiff(name, before, after string) gotextdiff.Unified {
	edits := myers.ComputeEdits(span.URIFromPath(name), before, after)
	return gotextdiff.ToUnified(name, name, before, edits)
}

// formatUnified writes a unified diff to w.
func formatUnified(w io.Writer, u gotextdiff.Unified) {
	_, _ = fmt.Fprintf(w, "%v", u)
}

// FormatUnifiedDiff writes a plain-text summary of the diff.
func FormatUnifiedDiff(w io.Writer, result *DiffResult) {
	for _, key := range result.Removed {
		_, _ = fmt.Fprintf(w, "REMOVED %s\n\n", key)
	}

	for _, key := range result.Added {
		_, _ = fmt.Fprintf(w, "ADDED %s\n\n", key)
	}

	for _, change := range result.Modified {
		formatUnified(w, change.Diff)
		_, _ = fmt.Fprintln(w)
	}
}

// FormatSummary writes a compact list of changed resources.
func FormatSummary(w io.Writer, result *DiffResult) {
	for _, key := range sortObjectRefs(result.Removed) {
		_, _ = fmt.Fprintf(w, "REMOVED  %s\n", key)
	}
	for _, key := range sortObjectRefs(result.Added) {
		_, _ = fmt.Fprintf(w, "ADDED    %s\n", key)
	}
	for _, change := range result.Modified {
		_, _ = fmt.Fprintf(w, "MODIFIED %s\n", change.Key)
	}
}

func sortObjectRefs(refs []ObjectRef) []ObjectRef {
	sorted := make([]ObjectRef, len(refs))
	copy(sorted, refs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].String() < sorted[j].String()
	})
	return sorted
}
