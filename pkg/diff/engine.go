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
// Inputs are not modified. Invalid nodes and duplicate identities within either
// input are rejected rather than omitted or overwritten.
//
//nolint:revive // exported API name; renaming would break consumers
func DiffNodes(before, after []*yaml.RNode) (*DiffResult, error) {
	beforeMap, err := buildResourceMap(before)
	if err != nil {
		return nil, fmt.Errorf("indexing before: %w", err)
	}
	afterMap, err := buildResourceMap(after)
	if err != nil {
		return nil, fmt.Errorf("indexing after: %w", err)
	}
	return diffResourceMaps(beforeMap, afterMap)
}

func diffResourceMaps(beforeMap, afterMap map[ObjectRef]*yaml.RNode) (*DiffResult, error) {
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

		beforeStr, err := renderNode(beforeNode)
		if err != nil {
			return nil, fmt.Errorf("rendering before %v: %w", key, err)
		}
		afterStr, err := renderNode(afterNode)
		if err != nil {
			return nil, fmt.Errorf("rendering after %v: %w", key, err)
		}

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
		return lessRef(result.Added[i], result.Added[j])
	})
	sort.Slice(result.Removed, func(i, j int) bool {
		return lessRef(result.Removed[i], result.Removed[j])
	})
	sort.Slice(result.Modified, func(i, j int) bool {
		return lessRef(result.Modified[i].Key, result.Modified[j].Key)
	})

	return result, nil
}

func buildResourceMap(nodes []*yaml.RNode) (map[ObjectRef]*yaml.RNode, error) {
	m := make(map[ObjectRef]*yaml.RNode, len(nodes))
	for i, node := range nodes {
		if yaml.IsMissingOrNull(node) || node.YNode().Kind != yaml.MappingNode {
			return nil, fmt.Errorf("resource %d must be a mapping", i+1)
		}
		// Normalize only copies: callers may reuse the original ASTs.
		node = node.Copy()
		object, err := node.Map()
		if err != nil {
			return nil, fmt.Errorf("converting resource %d to map: %w", i+1, err)
		}
		metadata, _ := object["metadata"].(map[string]any)
		var key ObjectRef
		for _, field := range []struct {
			name   string
			value  any
			target *string
		}{
			{"apiVersion", object["apiVersion"], &key.APIVersion},
			{"kind", object["kind"], &key.Kind},
			{"metadata.name", metadata["name"], &key.Name},
		} {
			value, ok := field.value.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("resource %d requires a non-empty string %s", i+1, field.name)
			}
			*field.target = value
		}
		if namespace, exists := metadata["namespace"]; exists {
			var ok bool
			key.Namespace, ok = namespace.(string)
			if !ok {
				return nil, fmt.Errorf("resource %d requires a string metadata.namespace", i+1)
			}
		}
		if _, exists := m[key]; exists {
			return nil, fmt.Errorf("duplicate resource identity: %s %s", key.APIVersion, key)
		}
		if _, err := ReorderFilter()([]*yaml.RNode{node}); err != nil {
			return nil, fmt.Errorf("normalizing %v: %w", key, err)
		}
		if _, err := renderNode(node); err != nil {
			return nil, fmt.Errorf("rendering %v: %w", key, err)
		}
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

func renderNode(node *yaml.RNode) (string, error) {
	var buf bytes.Buffer
	writer := &kio.ByteWriter{Writer: &buf}
	if err := writer.Write([]*yaml.RNode{node}); err != nil {
		return "", err
	}
	s := buf.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, nil
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
	beforeMap, err := buildResourceMap(before)
	if err != nil {
		return nil, fmt.Errorf("indexing before: %w", err)
	}
	afterMap, err := buildResourceMap(after)
	if err != nil {
		return nil, fmt.Errorf("indexing after: %w", err)
	}
	result, err := diffResourceMaps(beforeMap, afterMap)
	if err != nil {
		return nil, err
	}

	out := &DiffResultJSON{Added: []map[string]any{}, Deleted: []ObjectRef{}, Modified: []DiffChangeJSON{}}

	for _, key := range result.Added {
		node := afterMap[key]
		m, err := node.Map()
		if err != nil {
			return nil, fmt.Errorf("converting added %v: %w", key, err)
		}
		out.Added = append(out.Added, m)
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
		oldMap, err := beforeNode.Map()
		if err != nil {
			return nil, fmt.Errorf("converting before %v: %w", change.Key, err)
		}
		newMap, err := afterNode.Map()
		if err != nil {
			return nil, fmt.Errorf("converting after %v: %w", change.Key, err)
		}

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
		return lessRef(sorted[i], sorted[j])
	})
	return sorted
}

func lessRef(a, b ObjectRef) bool {
	for i, av := range [...]string{a.Kind, a.Name, a.Namespace, a.APIVersion} {
		bv := [...]string{b.Kind, b.Name, b.Namespace, b.APIVersion}[i]
		if av != bv {
			return av < bv
		}
	}
	return false
}
