package serve

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// Store holds an in-memory index of Kubernetes resources for the mock API server.
type Store struct {
	// byGVR maps GVR → namespace → slice of resources.
	byGVR map[GVR]map[string][]*unstructured.Unstructured
	// typeInfo maps GVR → TypeInfo.
	typeInfo map[GVR]TypeInfo
	// gvrToGVK maps GVR → GVK (for discovery responses).
	gvrToGVK map[GVR]GVK
	// gvkToGVR maps GVK → GVR (for indexing).
	gvkToGVR map[GVK]GVR
}

// ParseManifests reads a multi-document YAML stream and returns parsed resources.
// Malformed documents (missing kind or apiVersion) are silently skipped.
func ParseManifests(in io.Reader) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	decoder := k8syaml.NewYAMLToJSONDecoder(in)
	for {
		obj := make(map[string]any)
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		if u.GetKind() == "" || u.GetAPIVersion() == "" {
			continue
		}
		objects = append(objects, u)
	}
	return objects, nil
}

// NewStore creates a resource store from the given manifests.
// It extracts GVRs, enriches metadata, synthesizes namespaces, and indexes everything.
func NewStore(objects []*unstructured.Unstructured) *Store {
	s := &Store{
		byGVR:    make(map[GVR]map[string][]*unstructured.Unstructured),
		typeInfo: make(map[GVR]TypeInfo),
		gvrToGVK: make(map[GVR]GVK),
		gvkToGVR: make(map[GVK]GVR),
	}

	// Collect namespaces referenced by resources.
	referencedNS := make(map[string]bool)
	for _, obj := range objects {
		gvk := gvkOf(obj)
		info := LookupType(gvk)
		gvr := GVR{Group: gvk.Group, Version: gvk.Version, Resource: info.Plural}

		s.typeInfo[gvr] = info
		s.gvrToGVK[gvr] = gvk
		s.gvkToGVR[gvk] = gvr

		if info.Namespaced {
			ns := obj.GetNamespace()
			if ns == "" {
				ns = "default"
				obj.SetNamespace(ns)
			}
			referencedNS[ns] = true
		}
	}

	// Ensure "default" is always present.
	referencedNS["default"] = true

	// Determine which namespaces are explicitly defined in the input.
	definedNS := make(map[string]bool)
	for _, obj := range objects {
		if obj.GetKind() == "Namespace" && obj.GetAPIVersion() == "v1" {
			definedNS[obj.GetName()] = true
		}
	}

	// Synthesize Namespace objects for referenced but undefined namespaces.
	nsGVK := GVK{Group: "", Version: "v1", Kind: "Namespace"}
	nsInfo := LookupType(nsGVK)
	nsGVR := GVR{Group: "", Version: "v1", Resource: nsInfo.Plural}
	s.typeInfo[nsGVR] = nsInfo
	s.gvrToGVK[nsGVR] = nsGVK
	s.gvkToGVR[nsGVK] = nsGVR

	for ns := range referencedNS {
		if definedNS[ns] {
			continue
		}
		syn := synthesizeNamespace(ns)
		enrichMetadata(syn)
		s.add(nsGVR, "", syn)
	}

	// Index all input objects.
	for _, obj := range objects {
		gvk := gvkOf(obj)
		gvr, ok := s.gvkToGVR[gvk]
		if !ok {
			info := LookupType(gvk)
			gvr = GVR{Group: gvk.Group, Version: gvk.Version, Resource: info.Plural}
			s.typeInfo[gvr] = info
			s.gvrToGVK[gvr] = gvk
			s.gvkToGVR[gvk] = gvr
		}

		info := s.typeInfo[gvr]
		ns := ""
		if info.Namespaced {
			ns = obj.GetNamespace()
		}

		enrichMetadata(obj)
		s.add(gvr, ns, obj)
	}

	return s
}

func (s *Store) add(gvr GVR, ns string, obj *unstructured.Unstructured) {
	if s.byGVR[gvr] == nil {
		s.byGVR[gvr] = make(map[string][]*unstructured.Unstructured)
	}
	s.byGVR[gvr][ns] = append(s.byGVR[gvr][ns], obj)
}

// List returns all resources matching the given GVR, namespace, and label selector.
// For cluster-scoped resources, namespace is ignored.
// For namespaced resources, namespace="" means "all namespaces".
func (s *Store) List(gvr GVR, ns string, sel labels.Selector) []unstructured.Unstructured {
	nsMap, ok := s.byGVR[gvr]
	if !ok {
		return nil
	}

	var items []unstructured.Unstructured

	namespaces := nsMap
	if ns != "" {
		namespaces = map[string][]*unstructured.Unstructured{ns: nsMap[ns]}
	}

	for _, objs := range namespaces {
		for _, obj := range objs {
			if sel != nil && !sel.Empty() && !sel.Matches(labels.Set(obj.GetLabels())) {
				continue
			}
			items = append(items, *obj.DeepCopy())
		}
	}

	return items
}

// Get returns a single resource by GVR, namespace, and name.
// Returns nil if not found.
func (s *Store) Get(gvr GVR, ns, name string) *unstructured.Unstructured {
	nsMap, ok := s.byGVR[gvr]
	if !ok {
		return nil
	}

	if ns != "" {
		for _, obj := range nsMap[ns] {
			if obj.GetName() == name {
				return obj.DeepCopy()
			}
		}
		return nil
	}

	// Cluster-scoped or cross-namespace get.
	for _, objs := range nsMap {
		for _, obj := range objs {
			if obj.GetName() == name {
				return obj.DeepCopy()
			}
		}
	}
	return nil
}

// GVRSet returns the set of all GVRs in the store.
func (s *Store) GVRSet() map[GVR]TypeInfo {
	result := make(map[GVR]TypeInfo, len(s.typeInfo))
	for gvr, info := range s.typeInfo {
		result[gvr] = info
	}
	return result
}

// KindForGVR returns the Kind for a given GVR.
func (s *Store) KindForGVR(gvr GVR) string {
	if gvk, ok := s.gvrToGVK[gvr]; ok {
		return gvk.Kind
	}
	return ""
}

// GroupVersions returns the unique set of (group, version) pairs in the store.
func (s *Store) GroupVersions() map[groupVersion]bool {
	gvs := make(map[groupVersion]bool)
	for gvr := range s.byGVR {
		gvs[groupVersion{Group: gvr.Group, Version: gvr.Version}] = true
	}
	return gvs
}

type groupVersion struct {
	Group   string
	Version string
}

func gvkOf(obj *unstructured.Unstructured) GVK {
	apiVersion := obj.GetAPIVersion()
	group, version := "", ""
	if i := strings.Index(apiVersion, "/"); i >= 0 {
		group = apiVersion[:i]
		version = apiVersion[i+1:]
	} else {
		version = apiVersion
	}
	return GVK{Group: group, Version: version, Kind: obj.GetKind()}
}

func enrichMetadata(obj *unstructured.Unstructured) {
	meta, ok := obj.Object["metadata"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		obj.Object["metadata"] = meta
	}
	if _, exists := meta["uid"]; !exists {
		meta["uid"] = generateUID()
	}
	if _, exists := meta["resourceVersion"]; !exists {
		meta["resourceVersion"] = "1"
	}
	if _, exists := meta["creationTimestamp"]; !exists {
		meta["creationTimestamp"] = "2024-01-01T00:00:00Z"
	}
}

func generateUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail for 16 bytes, but handle gracefully.
		// Use timestamp + counter as fallback (not cryptographically secure,
		// but sufficient for mock server UIDs).
		return fmt.Sprintf("%x", time.Now().UnixNano())[:16]
	}
	return hex.EncodeToString(b)
}

func synthesizeNamespace(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]any{
				"name": name,
			},
		},
	}
}
