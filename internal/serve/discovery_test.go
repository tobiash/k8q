package serve

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildAPIVersions(t *testing.T) {
	addr := "127.0.0.1:8443"
	got := buildAPIVersions(addr)

	// Check TypeMeta.
	if got.Kind != "APIVersions" {
		t.Errorf("Kind = %q, want %q", got.Kind, "APIVersions")
	}
	if got.APIVersion != "v1" {
		t.Errorf("APIVersion = %q, want %q", got.APIVersion, "v1")
	}

	// Check versions list contains v1.
	foundV1 := false
	for _, v := range got.Versions {
		if v == "v1" {
			foundV1 = true
			break
		}
	}
	if !foundV1 {
		t.Errorf("Versions = %v, want to contain %q", got.Versions, "v1")
	}

	// Check server address.
	if len(got.ServerAddressByClientCIDRs) != 1 {
		t.Fatalf("ServerAddressByClientCIDRs len = %d, want 1", len(got.ServerAddressByClientCIDRs))
	}
	if got.ServerAddressByClientCIDRs[0].ServerAddress != addr {
		t.Errorf("ServerAddress = %q, want %q", got.ServerAddressByClientCIDRs[0].ServerAddress, addr)
	}
	if got.ServerAddressByClientCIDRs[0].ClientCIDR != "0.0.0.0/0" {
		t.Errorf("ClientCIDR = %q, want %q", got.ServerAddressByClientCIDRs[0].ClientCIDR, "0.0.0.0/0")
	}
}

func TestBuildAPIGroupList(t *testing.T) {
	// Build a store with apps/v1 and batch/v1 resources.
	objects := []*unstructured.Unstructured{
		makeUnstructured("apps/v1", "Deployment", "default", "deploy1"),
		makeUnstructured("batch/v1", "Job", "default", "job1"),
	}
	store := NewStore(objects)

	got := buildAPIGroupList(store)

	// Core v1 group should not appear (it's served under /api).
	for _, g := range got.Groups {
		if g.Name == "" {
			t.Error("core group should not appear in APIGroupList; it is served under /api")
		}
	}

	// Should contain apps and batch groups.
	groupNames := make(map[string]bool)
	for _, g := range got.Groups {
		groupNames[g.Name] = true
	}
	if !groupNames["apps"] {
		t.Error("missing apps group")
	}
	if !groupNames["batch"] {
		t.Error("missing batch group")
	}

	// TypeMeta.
	if got.Kind != "APIGroupList" {
		t.Errorf("Kind = %q, want %q", got.Kind, "APIGroupList")
	}
	if got.APIVersion != "v1" {
		t.Errorf("APIVersion = %q, want %q", got.APIVersion, "v1")
	}

	// Find apps group and check preferred version.
	for _, g := range got.Groups {
		if g.Name == "apps" {
			if g.PreferredVersion.GroupVersion != "apps/v1" {
				t.Errorf("apps PreferredVersion.GroupVersion = %q, want %q", g.PreferredVersion.GroupVersion, "apps/v1")
			}
			if g.PreferredVersion.Version != "v1" {
				t.Errorf("apps PreferredVersion.Version = %q, want %q", g.PreferredVersion.Version, "v1")
			}
			if len(g.Versions) != 1 || g.Versions[0].GroupVersion != "apps/v1" {
				t.Errorf("apps Versions = %+v, want single entry with GroupVersion=apps/v1", g.Versions)
			}
		}
	}
}

func TestBuildAPIGroupList_Sorted(t *testing.T) {
	objects := []*unstructured.Unstructured{
		makeUnstructured("z.io/v1", "Thing", "default", "thing1"),
		makeUnstructured("a.io/v1", "Stuff", "default", "stuff1"),
	}
	store := NewStore(objects)

	got := buildAPIGroupList(store)

	if len(got.Groups) < 2 {
		t.Fatalf("expected at least 2 groups, got %d", len(got.Groups))
	}
	for i := 1; i < len(got.Groups); i++ {
		if got.Groups[i-1].Name > got.Groups[i].Name {
			t.Errorf("groups not sorted: %q > %q", got.Groups[i-1].Name, got.Groups[i].Name)
		}
	}
}

func TestBuildAPIV1Resources(t *testing.T) {
	objects := []*unstructured.Unstructured{
		makeUnstructured("v1", "Pod", "default", "pod1"),
		makeUnstructured("v1", "Service", "default", "svc1"),
		makeUnstructured("v1", "Namespace", "", "ns1"),
	}
	store := NewStore(objects)

	got := buildAPIV1Resources(store)

	// GroupVersion should be just "v1" for core group.
	if got.GroupVersion != "v1" {
		t.Errorf("GroupVersion = %q, want %q", got.GroupVersion, "v1")
	}

	// TypeMeta.
	if got.Kind != "APIResourceList" {
		t.Errorf("Kind = %q, want %q", got.Kind, "APIResourceList")
	}
	if got.APIVersion != "v1" {
		t.Errorf("APIVersion = %q, want %q", got.APIVersion, "v1")
	}

	// Build a lookup by resource name.
	resByName := make(map[string]metav1.APIResource)
	for _, r := range got.APIResources {
		resByName[r.Name] = r
	}

	// Check pods.
	pods, ok := resByName["pods"]
	if !ok {
		t.Fatal("missing pods resource")
	}
	if pods.Kind != "Pod" {
		t.Errorf("pods.Kind = %q, want %q", pods.Kind, "Pod")
	}
	if pods.SingularName != "pod" {
		t.Errorf("pods.SingularName = %q, want %q", pods.SingularName, "pod")
	}
	if !pods.Namespaced {
		t.Error("pods should be namespaced")
	}
	if !hasVerb(pods.Verbs, "get") || !hasVerb(pods.Verbs, "list") {
		t.Errorf("pods.Verbs = %v, want get and list", pods.Verbs)
	}
	if pods.Group != "" {
		t.Errorf("pods.Group = %q, want empty for core v1", pods.Group)
	}
	if pods.Version != "v1" {
		t.Errorf("pods.Version = %q, want %q", pods.Version, "v1")
	}

	// Check namespaces.
	ns, ok := resByName["namespaces"]
	if !ok {
		t.Fatal("missing namespaces resource")
	}
	if ns.Namespaced {
		t.Error("namespaces should be cluster-scoped")
	}

	// Check services.
	svc, ok := resByName["services"]
	if !ok {
		t.Fatal("missing services resource")
	}
	if !svc.Namespaced {
		t.Error("services should be namespaced")
	}

	// Check status subresources for Pod.
	podStatus, ok := resByName["pods/status"]
	if !ok {
		t.Fatal("missing pods/status subresource; Pod should have a status subresource")
	}
	if podStatus.Kind != "Pod" {
		t.Errorf("pods/status.Kind = %q, want %q", podStatus.Kind, "Pod")
	}
	if !hasVerb(podStatus.Verbs, "get") || !hasVerb(podStatus.Verbs, "patch") || !hasVerb(podStatus.Verbs, "update") {
		t.Errorf("pods/status.Verbs = %v, want get, patch, update", podStatus.Verbs)
	}
	if podStatus.SingularName != "" {
		t.Errorf("pods/status.SingularName = %q, want empty", podStatus.SingularName)
	}
}

func TestBuildAPIResourceList_NamedGroup(t *testing.T) {
	objects := []*unstructured.Unstructured{
		makeUnstructured("apps/v1", "Deployment", "default", "deploy1"),
		makeUnstructured("apps/v1", "StatefulSet", "default", "sts1"),
		// This is in a different group/version and should be excluded.
		makeUnstructured("batch/v1", "Job", "default", "job1"),
	}
	store := NewStore(objects)

	got := buildAPIResourceList(store, "apps", "v1")

	// GroupVersion should be "apps/v1".
	if got.GroupVersion != "apps/v1" {
		t.Errorf("GroupVersion = %q, want %q", got.GroupVersion, "apps/v1")
	}

	// Should contain deployments and statefulsets, but NOT jobs.
	resNames := make(map[string]bool)
	for _, r := range got.APIResources {
		resNames[r.Name] = true
	}
	if !resNames["deployments"] {
		t.Error("missing deployments resource")
	}
	if !resNames["statefulsets"] {
		t.Error("missing statefulsets resource")
	}
	if resNames["jobs"] {
		t.Error("jobs should not appear in apps/v1 resource list")
	}

	// Resources should be sorted by name.
	for i := 1; i < len(got.APIResources); i++ {
		if got.APIResources[i-1].Name > got.APIResources[i].Name {
			t.Errorf("resources not sorted: %q > %q", got.APIResources[i-1].Name, got.APIResources[i].Name)
		}
	}

	// Each resource should have correct group/version and at least 'get' verb.
	for _, r := range got.APIResources {
		if r.Group != "apps" {
			t.Errorf("resource %q Group = %q, want %q", r.Name, r.Group, "apps")
		}
		if r.Version != "v1" {
			t.Errorf("resource %q Version = %q, want %q", r.Name, r.Version, "v1")
		}
		// Skip status subresources — they have different verbs.
		if strings.Contains(r.Name, "/") {
			continue
		}
		if !hasVerb(r.Verbs, "get") || !hasVerb(r.Verbs, "list") {
			t.Errorf("resource %q Verbs = %v, want get and list", r.Name, r.Verbs)
		}
	}
}

func TestBuildAPIResourceList_StatusSubresources(t *testing.T) {
	// Status subresources should be generated for known workload kinds.
	objects := []*unstructured.Unstructured{
		makeUnstructured("apps/v1", "Deployment", "default", "deploy1"),
	}
	store := NewStore(objects)

	got := buildAPIResourceList(store, "apps", "v1")

	resByName := make(map[string]metav1.APIResource)
	for _, r := range got.APIResources {
		resByName[r.Name] = r
	}

	statusRes, ok := resByName["deployments/status"]
	if !ok {
		t.Fatal("missing deployments/status subresource")
	}
	if statusRes.Kind != "Deployment" {
		t.Errorf("deployments/status.Kind = %q, want %q", statusRes.Kind, "Deployment")
	}
	if statusRes.Namespaced != true {
		t.Error("deployments/status should be namespaced")
	}
}

func TestBuildAPIResourceList_EmptyStore(t *testing.T) {
	store := NewStore(nil)

	got := buildAPIResourceList(store, "apps", "v1")

	if len(got.APIResources) != 0 {
		t.Errorf("expected no resources for empty store, got %d", len(got.APIResources))
	}
	if got.GroupVersion != "apps/v1" {
		t.Errorf("GroupVersion = %q, want %q", got.GroupVersion, "apps/v1")
	}
}

func TestBuildAPIResourceList_NoMatchingGroupVersion(t *testing.T) {
	objects := []*unstructured.Unstructured{
		makeUnstructured("apps/v1", "Deployment", "default", "deploy1"),
	}
	store := NewStore(objects)

	got := buildAPIResourceList(store, "batch", "v1")

	if len(got.APIResources) != 0 {
		t.Errorf("expected no resources for batch/v1, got %d", len(got.APIResources))
	}
}

// makeUnstructured creates a minimal Unstructured object for testing.
func makeUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}
	return obj
}

// hasVerb reports whether the verb list contains the given verb.
func hasVerb(verbs metav1.Verbs, want string) bool {
	for _, v := range verbs {
		if v == want {
			return true
		}
	}
	return false
}

// TestBuildAPIGroupList_EmptyStore verifies behavior with no objects.
func TestBuildAPIGroupList_EmptyStore(t *testing.T) {
	store := NewStore(nil)

	got := buildAPIGroupList(store)

	if len(got.Groups) != 0 {
		t.Errorf("expected no groups for empty store, got %d", len(got.Groups))
	}
	if got.Kind != "APIGroupList" {
		t.Errorf("Kind = %q, want %q", got.Kind, "APIGroupList")
	}
}

// TestBuildAPIResourceList_SingularName verifies that SingularName is the
// lowercased kind for the main resource and empty for subresources.
func TestBuildAPIResourceList_SingularName(t *testing.T) {
	objects := []*unstructured.Unstructured{
		makeUnstructured("v1", "Pod", "default", "pod1"),
	}
	store := NewStore(objects)

	got := buildAPIV1Resources(store)

	for _, r := range got.APIResources {
		if strings.Contains(r.Name, "/") {
			// Subresource: SingularName should be empty.
			if r.SingularName != "" {
				t.Errorf("subresource %q SingularName = %q, want empty", r.Name, r.SingularName)
			}
		} else {
			// Main resource: SingularName should be lowercase kind.
			expected := strings.ToLower(r.Kind)
			if diff := cmp.Diff(expected, r.SingularName); diff != "" {
				t.Errorf("resource %q SingularName mismatch: %s", r.Name, diff)
			}
		}
	}
}
