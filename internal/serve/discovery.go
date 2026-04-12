package serve

import (
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildAPIVersions returns the response for GET /api.
func buildAPIVersions(addr string) *metav1.APIVersions {
	return &metav1.APIVersions{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIVersions",
			APIVersion: "v1",
		},
		Versions: []string{"v1"},
		ServerAddressByClientCIDRs: []metav1.ServerAddressByClientCIDR{
			{ClientCIDR: "0.0.0.0/0", ServerAddress: addr},
		},
	}
}

// buildAPIGroupList returns the response for GET /apis.
func buildAPIGroupList(store *Store) *metav1.APIGroupList {
	gvs := store.GroupVersions()
	groupVersions := make(map[string][]metav1.GroupVersionForDiscovery) // group → versions
	for gv := range gvs {
		if gv.Group == "" {
			continue // Core group is served under /api, not /apis.
		}
		groupVersions[gv.Group] = append(groupVersions[gv.Group], metav1.GroupVersionForDiscovery{
			GroupVersion: gv.Group + "/" + gv.Version,
			Version:      gv.Version,
		})
	}

	groups := make([]metav1.APIGroup, 0, len(groupVersions))
	for name, versions := range groupVersions {
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].Version < versions[j].Version
		})
		groups = append(groups, metav1.APIGroup{
			TypeMeta: metav1.TypeMeta{
				Kind:       "APIGroup",
				APIVersion: "v1",
			},
			Name:             name,
			Versions:         versions,
			PreferredVersion: versions[0],
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return &metav1.APIGroupList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIGroupList",
			APIVersion: "v1",
		},
		Groups: groups,
	}
}

// buildAPIV1Resources returns the response for GET /api/v1.
func buildAPIV1Resources(store *Store) *metav1.APIResourceList {
	return buildAPIResourceList(store, "", "v1")
}

// buildAPIResourceList returns the response for a group/version discovery endpoint.
func buildAPIResourceList(store *Store, group, version string) *metav1.APIResourceList {
	gvrs := store.GVRSet()
	var resources []metav1.APIResource

	for gvr, info := range gvrs {
		if gvr.Group != group || gvr.Version != version {
			continue
		}
		kind := store.KindForGVR(gvr)
		if kind == "" {
			continue
		}

		singular := strings.ToLower(kind)
		resources = append(resources, metav1.APIResource{
			Name:         gvr.Resource,
			SingularName: singular,
			Namespaced:   info.Namespaced,
			Group:        group,
			Version:      version,
			Kind:         kind,
			Verbs:        metav1.Verbs{"get", "list"},
		})

		// Add status subresource for common workload types.
		if isStatusSubresourceKind(kind) {
			resources = append(resources, metav1.APIResource{
				Name:         gvr.Resource + "/status",
				SingularName: "",
				Namespaced:   info.Namespaced,
				Group:        group,
				Version:      version,
				Kind:         kind,
				Verbs:        metav1.Verbs{"get", "patch", "update"},
			})
		}
	}

	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})

	gv := version
	if group != "" {
		gv = group + "/" + version
	}

	return &metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIResourceList",
			APIVersion: "v1",
		},
		GroupVersion:  gv,
		APIResources:  resources,
	}
}

// isStatusSubresourceKind reports whether a kind typically has a /status subresource.
func isStatusSubresourceKind(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet",
		"Pod", "Service", "Namespace", "Node", "PersistentVolume", "PersistentVolumeClaim",
		"Ingress", "NetworkPolicy", "CronJob", "Job",
		"CustomResourceDefinition":
		return true
	}
	return false
}
