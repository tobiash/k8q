package serve

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// ---------------------------------------------------------------------------
// ParseManifests
// ---------------------------------------------------------------------------

func TestParseManifests(t *testing.T) {
	t.Run("ValidMultiDocument", func(t *testing.T) {
		input := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-one
  namespace: default
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-two
  namespace: kube-system
`
		objs, err := ParseManifests(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseManifests returned error: %v", err)
		}
		if len(objs) != 2 {
			t.Fatalf("expected 2 objects, got %d", len(objs))
		}
		if got := objs[0].GetName(); got != "cm-one" {
			t.Errorf("obj[0] name = %q, want %q", got, "cm-one")
		}
		if got := objs[1].GetName(); got != "cm-two" {
			t.Errorf("obj[1] name = %q, want %q", got, "cm-two")
		}
		if got := objs[1].GetNamespace(); got != "kube-system" {
			t.Errorf("obj[1] namespace = %q, want %q", got, "kube-system")
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		objs, err := ParseManifests(strings.NewReader(""))
		if err != nil {
			t.Fatalf("ParseManifests returned error: %v", err)
		}
		if len(objs) != 0 {
			t.Fatalf("expected 0 objects, got %d", len(objs))
		}
	})

	t.Run("SkipsMissingKindOrVersion", func(t *testing.T) {
		input := `
apiVersion: v1
---
kind: Pod
metadata:
  name: orphan
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: good
`
		objs, err := ParseManifests(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseManifests returned error: %v", err)
		}
		if len(objs) != 1 {
			t.Fatalf("expected 1 object, got %d", len(objs))
		}
		if got := objs[0].GetName(); got != "good" {
			t.Errorf("object name = %q, want %q", got, "good")
		}
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		// Produce a decode error by feeding something that cannot be decoded
		// as valid JSON after the YAML-to-JSON conversion step.
		input := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: ok
---
{invalid: json: content: [}
`
		_, err := ParseManifests(strings.NewReader(input))
		if err == nil {
			t.Fatal("expected error for invalid YAML, got nil")
		}
	})

	t.Run("EmptyDocumentsSkipped", func(t *testing.T) {
		// Multiple --- separators produce empty documents.
		input := `---
---
apiVersion: v1
kind: Secret
metadata:
  name: s
---
---
`
		objs, err := ParseManifests(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseManifests returned error: %v", err)
		}
		if len(objs) != 1 {
			t.Fatalf("expected 1 object, got %d", len(objs))
		}
		if got := objs[0].GetName(); got != "s" {
			t.Errorf("object name = %q, want %q", got, "s")
		}
	})
}

// ---------------------------------------------------------------------------
// NewStore
// ---------------------------------------------------------------------------

//nolint:gocyclo
func TestNewStore(t *testing.T) {
	t.Run("ResourcesIndexedByGVR", func(t *testing.T) {
		input := `
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: default
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		podsGVR := GVR{Group: "", Version: "v1", Resource: "pods"}
		got := store.List(podsGVR, "", nil)
		if len(got) != 1 {
			t.Fatalf("List(pods) returned %d items, want 1", len(got))
		}
		if got[0].GetName() != "my-pod" {
			t.Errorf("pod name = %q, want %q", got[0].GetName(), "my-pod")
		}
	})

	t.Run("NamespaceSynthesis", func(t *testing.T) {
		// A Pod references namespace "staging" which is not explicitly defined.
		// The store should synthesize a Namespace object for it.
		input := `
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: staging
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		nsGVR := GVR{Group: "", Version: "v1", Resource: "namespaces"}
		ns := store.Get(nsGVR, "", "staging")
		if ns == nil {
			t.Fatal("Get(namespaces, staging) returned nil, expected synthesized namespace")
		}
		if ns.GetKind() != "Namespace" {
			t.Errorf("synthesized kind = %q, want Namespace", ns.GetKind())
		}
	})

	t.Run("DefaultNamespaceAlwaysPresent", func(t *testing.T) {
		// Even with no input objects, the "default" namespace should exist.
		store := NewStore(nil)

		nsGVR := GVR{Group: "", Version: "v1", Resource: "namespaces"}
		ns := store.Get(nsGVR, "", "default")
		if ns == nil {
			t.Fatal("Get(namespaces, default) returned nil, want synthesized default namespace")
		}
		if ns.GetName() != "default" {
			t.Errorf("namespace name = %q, want %q", ns.GetName(), "default")
		}
	})

	t.Run("MetadataEnrichment", func(t *testing.T) {
		input := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-cm
  namespace: default
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		cmGVR := GVR{Group: "", Version: "v1", Resource: "configmaps"}
		cm := store.Get(cmGVR, "default", "my-cm")
		if cm == nil {
			t.Fatal("Get(configmaps, my-cm) returned nil")
		}

		meta := cm.Object["metadata"].(map[string]interface{})

		if uid, ok := meta["uid"].(string); !ok || uid == "" {
			t.Error("metadata.uid is missing or empty, expected auto-generated UID")
		}
		if rv, ok := meta["resourceVersion"].(string); !ok || rv == "" {
			t.Error("metadata.resourceVersion is missing or empty")
		}
		if ct, ok := meta["creationTimestamp"].(string); !ok || ct == "" {
			t.Error("metadata.creationTimestamp is missing or empty")
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		store := NewStore(nil)

		// Only the default namespace should exist.
		nsGVR := GVR{Group: "", Version: "v1", Resource: "namespaces"}
		allNS := store.List(nsGVR, "", nil)
		if len(allNS) != 1 {
			t.Fatalf("expected 1 namespace (default), got %d", len(allNS))
		}
		if allNS[0].GetName() != "default" {
			t.Errorf("namespace name = %q, want %q", allNS[0].GetName(), "default")
		}
	})

	t.Run("ClusterScopedResourceIndexedWithEmptyNamespace", func(t *testing.T) {
		input := `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		crGVR := GVR{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
		cr := store.Get(crGVR, "", "admin")
		if cr == nil {
			t.Fatal("Get(clusterroles, admin) returned nil")
		}
		if cr.GetName() != "admin" {
			t.Errorf("ClusterRole name = %q, want %q", cr.GetName(), "admin")
		}
	})

	t.Run("ExistingNamespaceNotReplacedBySynthesis", func(t *testing.T) {
		input := `
apiVersion: v1
kind: Namespace
metadata:
  name: custom
  labels:
    env: test
---
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: custom
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		nsGVR := GVR{Group: "", Version: "v1", Resource: "namespaces"}
		ns := store.Get(nsGVR, "", "custom")
		if ns == nil {
			t.Fatal("Get(namespaces, custom) returned nil")
		}
		// The explicitly-defined namespace should keep its labels.
		if got := ns.GetLabels(); got["env"] != "test" {
			t.Errorf("namespace labels['env'] = %q, want %q; synthesis may have replaced the original", got["env"], "test")
		}
	})

	t.Run("NamespacedDefaultFallback", func(t *testing.T) {
		// A namespaced resource with no explicit namespace falls back to "default".
		input := `
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
`
		objs, _ := ParseManifests(strings.NewReader(input))
		store := NewStore(objs)

		secretGVR := GVR{Group: "", Version: "v1", Resource: "secrets"}
		sec := store.Get(secretGVR, "default", "my-secret")
		if sec == nil {
			t.Fatal("Get(secrets, default, my-secret) returned nil; namespace should have defaulted")
		}
	})
}

// ---------------------------------------------------------------------------
// Store.List
// ---------------------------------------------------------------------------

func TestStoreList(t *testing.T) {
	// Shared store with a mix of resources across namespaces and labels.
	store := storeFromYAML(t, `
apiVersion: v1
kind: Pod
metadata:
  name: nginx-a
  namespace: alpha
  labels:
    app: nginx
    tier: frontend
---
apiVersion: v1
kind: Pod
metadata:
  name: nginx-b
  namespace: beta
  labels:
    app: nginx
    tier: backend
---
apiVersion: v1
kind: Pod
metadata:
  name: redis-a
  namespace: alpha
  labels:
    app: redis
    tier: backend
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: alpha
`)

	podsGVR := GVR{Group: "", Version: "v1", Resource: "pods"}
	cmGVR := GVR{Group: "", Version: "v1", Resource: "configmaps"}
	unknownGVR := GVR{Group: "fake.example.com", Version: "v1", Resource: "widgets"}

	t.Run("AllNamespaces", func(t *testing.T) {
		got := store.List(podsGVR, "", nil)
		if len(got) != 3 {
			t.Fatalf("List(pods, all namespaces) = %d items, want 3", len(got))
		}
	})

	t.Run("FilterByNamespace", func(t *testing.T) {
		got := store.List(podsGVR, "alpha", nil)
		if len(got) != 2 {
			t.Fatalf("List(pods, alpha) = %d items, want 2", len(got))
		}
		names := sortedNames(got)
		want := []string{"nginx-a", "redis-a"}
		if diff := cmp.Diff(want, names); diff != "" {
			t.Errorf("names mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("FilterByLabelSelector", func(t *testing.T) {
		sel := mustParseSelector(t, "app=nginx")
		got := store.List(podsGVR, "", sel)
		if len(got) != 2 {
			t.Fatalf("List(pods, app=nginx) = %d items, want 2", len(got))
		}
		for _, obj := range got {
			if obj.GetLabels()["app"] != "nginx" {
				t.Errorf("object %q has app=%q, want nginx", obj.GetName(), obj.GetLabels()["app"])
			}
		}
	})

	t.Run("LabelSelectorExcludesAll", func(t *testing.T) {
		sel := mustParseSelector(t, "app=nonexistent")
		got := store.List(podsGVR, "", sel)
		if len(got) != 0 {
			t.Fatalf("List(pods, app=nonexistent) = %d items, want 0", len(got))
		}
	})

	t.Run("NamespaceAndLabelCombined", func(t *testing.T) {
		sel := mustParseSelector(t, "app=nginx")
		got := store.List(podsGVR, "alpha", sel)
		if len(got) != 1 {
			t.Fatalf("List(pods, alpha, app=nginx) = %d items, want 1", len(got))
		}
		if got[0].GetName() != "nginx-a" {
			t.Errorf("name = %q, want %q", got[0].GetName(), "nginx-a")
		}
	})

	t.Run("UnknownGVR", func(t *testing.T) {
		got := store.List(unknownGVR, "", nil)
		if got != nil {
			t.Fatalf("List(unknown GVR) = %v, want nil", got)
		}
	})

	t.Run("EmptyResultForKnownGVRWrongNS", func(t *testing.T) {
		got := store.List(cmGVR, "beta", nil)
		if len(got) != 0 {
			t.Fatalf("List(configmaps, beta) = %d items, want 0", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// Store.Get
// ---------------------------------------------------------------------------

func TestStoreGet(t *testing.T) {
	store := storeFromYAML(t, `
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  namespace: default
  labels:
    app: test
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: admin
`)

	t.Run("ExistingNamespacedResource", func(t *testing.T) {
		podsGVR := GVR{Group: "", Version: "v1", Resource: "pods"}
		got := store.Get(podsGVR, "default", "my-pod")
		if got == nil {
			t.Fatal("Get(pods, default, my-pod) returned nil")
		}
		if got.GetName() != "my-pod" {
			t.Errorf("name = %q, want %q", got.GetName(), "my-pod")
		}
		if got.GetLabels()["app"] != "test" {
			t.Errorf("labels['app'] = %q, want %q", got.GetLabels()["app"], "test")
		}
	})

	t.Run("NonExistentResource", func(t *testing.T) {
		podsGVR := GVR{Group: "", Version: "v1", Resource: "pods"}
		got := store.Get(podsGVR, "default", "no-such-pod")
		if got != nil {
			t.Fatalf("Get(non-existent) = %+v, want nil", got)
		}
	})

	t.Run("ClusterScopedResource", func(t *testing.T) {
		crGVR := GVR{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
		got := store.Get(crGVR, "", "admin")
		if got == nil {
			t.Fatal("Get(clusterroles, admin) returned nil")
		}
		if got.GetName() != "admin" {
			t.Errorf("name = %q, want %q", got.GetName(), "admin")
		}
	})

	t.Run("ReturnsACopy", func(t *testing.T) {
		podsGVR := GVR{Group: "", Version: "v1", Resource: "pods"}
		got := store.Get(podsGVR, "default", "my-pod")
		got.SetLabels(map[string]string{"tampered": "true"})

		// Fetch again; original should be unchanged.
		again := store.Get(podsGVR, "default", "my-pod")
		if again.GetLabels()["tampered"] != "" {
			t.Error("Get did not return a deep copy; mutating the result affected the store")
		}
	})
}

// ---------------------------------------------------------------------------
// Store.KindForGVR
// ---------------------------------------------------------------------------

func TestStoreKindForGVR(t *testing.T) {
	store := storeFromYAML(t, `
apiVersion: v1
kind: Pod
metadata:
  name: p
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: d
  namespace: default
`)

	tests := []struct {
		name string
		gvr  GVR
		want string
	}{
		{
			name: "KnownPodGVR",
			gvr:  GVR{Group: "", Version: "v1", Resource: "pods"},
			want: "Pod",
		},
		{
			name: "KnownDeploymentGVR",
			gvr:  GVR{Group: "apps", Version: "v1", Resource: "deployments"},
			want: "Deployment",
		},
		{
			name: "UnknownGVR",
			gvr:  GVR{Group: "fake.io", Version: "v1", Resource: "widgets"},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := store.KindForGVR(tc.gvr)
			if got != tc.want {
				t.Errorf("KindForGVR(%+v) = %q, want %q", tc.gvr, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Store.GVRSet
// ---------------------------------------------------------------------------

func TestStoreGVRSet(t *testing.T) {
	store := storeFromYAML(t, `
apiVersion: v1
kind: Pod
metadata:
  name: p
  namespace: default
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: c
  namespace: default
`)

	gvrs := store.GVRSet()

	// We expect at least pods, configmaps, and namespaces (always synthesized).
	expected := []GVR{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "configmaps"},
		{Group: "", Version: "v1", Resource: "namespaces"},
	}
	for _, want := range expected {
		if _, ok := gvrs[want]; !ok {
			t.Errorf("GVRSet() missing expected GVR %+v", want)
		}
	}

	// TypeInfo should be populated correctly for each GVR.
	for gvr, info := range gvrs {
		if info.Plural == "" {
			t.Errorf("GVRSet()[%+v].Plural is empty", gvr)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// storeFromYAML parses the YAML and builds a Store, failing the test on error.
func storeFromYAML(t *testing.T, yaml string) *Store {
	t.Helper()
	objs, err := ParseManifests(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("ParseManifests: %v", err)
	}
	return NewStore(objs)
}

// mustParseSelector parses a label selector string or fails the test.
func mustParseSelector(t *testing.T, s string) labels.Selector {
	t.Helper()
	sel, err := labels.Parse(s)
	if err != nil {
		t.Fatalf("labels.Parse(%q): %v", s, err)
	}
	return sel
}

// sortedNames returns the names of objects in sorted order for deterministic comparison.
func sortedNames(objs []unstructured.Unstructured) []string {
	names := make([]string, len(objs))
	for i, o := range objs {
		names[i] = o.GetName()
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}
