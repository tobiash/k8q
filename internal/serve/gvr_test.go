package serve

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPluralize(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		// Regular words.
		{kind: "Pod", want: "pods"},
		{kind: "Service", want: "services"},
		{kind: "Node", want: "nodes"},
		{kind: "Secret", want: "secrets"},

		// Words ending in s/x/z → add "es".
		{kind: "Ingress", want: "ingresses"},
		{kind: "Axis", want: "axes"},
		{kind: "Box", want: "boxes"},
		{kind: "Buzz", want: "buzzes"},

		// Words ending in ch/sh → add "es".
		{kind: "Watch", want: "watches"},
		{kind: "Mesh", want: "meshes"},
		{kind: "Patch", want: "patches"},
		{kind: "Push", want: "pushes"},

		// Consonant + y → -ies.
		{kind: "NetworkPolicy", want: "networkpolicies"},
		{kind: "Inventory", want: "inventories"},
		{kind: "Entry", want: "entries"},

		// Vowel + y → just add "s" (y unchanged).
		{kind: "Key", want: "keys"},
		{kind: "Boy", want: "boys"},
		{kind: "Day", want: "days"},

		// -sis → -ses.
		{kind: "Analysis", want: "analyses"},
		{kind: "Diagnosis", want: "diagnoses"},

		// -is (not -sis) → -es (axis rule).
		{kind: "Axis", want: "axes"},

		// Empty string.
		{kind: "", want: ""},

		// Already lowercase input.
		{kind: "pod", want: "pods"},
		{kind: "service", want: "services"},

		// Single character.
		{kind: "s", want: "ses"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := Pluralize(tt.kind)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Pluralize(%q) = %q, want %q\n%s", tt.kind, got, tt.want, diff)
			}
		})
	}
}

func TestLookupType(t *testing.T) {
	tests := []struct {
		name string
		gvk  GVK
		want TypeInfo
	}{
		{
			name: "core v1 Pod is namespaced",
			gvk:  GVK{"", "v1", "Pod"},
			want: TypeInfo{Plural: "pods", Namespaced: true},
		},
		{
			name: "core v1 Namespace is cluster-scoped",
			gvk:  GVK{"", "v1", "Namespace"},
			want: TypeInfo{Plural: "namespaces", Namespaced: false},
		},
		{
			name: "apps/v1 Deployment is namespaced",
			gvk:  GVK{"apps", "v1", "Deployment"},
			want: TypeInfo{Plural: "deployments", Namespaced: true},
		},
		{
			name: "core v1 Node is cluster-scoped",
			gvk:  GVK{"", "v1", "Node"},
			want: TypeInfo{Plural: "nodes", Namespaced: false},
		},
		{
			name: "networking v1 Ingress is namespaced",
			gvk:  GVK{"networking.k8s.io", "v1", "Ingress"},
			want: TypeInfo{Plural: "ingresses", Namespaced: true},
		},
		{
			name: "unknown type uses pluralizer and assumes namespaced",
			gvk:  GVK{"example.com", "v1", "Widget"},
			want: TypeInfo{Plural: "widgets", Namespaced: true},
		},
		{
			name: "unknown type with -is suffix",
			gvk:  GVK{"example.com", "v1", "Axis"},
			want: TypeInfo{Plural: "axes", Namespaced: true},
		},
		{
			name: "unknown type ending in s",
			gvk:  GVK{"example.com", "v1", "Status"},
			want: TypeInfo{Plural: "statuses", Namespaced: true},
		},
		{
			name: "unknown type with consonant+y",
			gvk:  GVK{"example.com", "v1", "NetworkPolicy"},
			want: TypeInfo{Plural: "networkpolicies", Namespaced: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupType(tt.gvk)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("LookupType(%+v) = %+v, want %+v\n%s", tt.gvk, got, tt.want, diff)
			}
		})
	}
}

func TestGVK_APIVersion(t *testing.T) {
	tests := []struct {
		name string
		gvk  GVK
		want string
	}{
		{
			name: "core group returns version only",
			gvk:  GVK{"", "v1", "Pod"},
			want: "v1",
		},
		{
			name: "named group returns group/version",
			gvk:  GVK{"apps", "v1", "Deployment"},
			want: "apps/v1",
		},
		{
			name: "networking group",
			gvk:  GVK{"networking.k8s.io", "v1", "Ingress"},
			want: "networking.k8s.io/v1",
		},
		{
			name: "batch group",
			gvk:  GVK{"batch", "v1", "Job"},
			want: "batch/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gvk.APIVersion()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GVK(%+v).APIVersion() = %q, want %q\n%s", tt.gvk, got, tt.want, diff)
			}
		})
	}
}
