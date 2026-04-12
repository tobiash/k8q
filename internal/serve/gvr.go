// Package serve implements a mock Kubernetes API server that serves
// piped-in YAML manifests over HTTPS.
package serve

import "strings"

// GVR identifies a Group-Version-Resource triple.
type GVR struct {
	Group    string
	Version  string
	Resource string
}

// GVK identifies a Group-Version-Kind triple.
type GVK struct {
	Group   string
	Version string
	Kind    string
}

// APIVersion returns the apiVersion string for this GVK.
func (g GVK) APIVersion() string {
	if g.Group == "" {
		return g.Version
	}
	return g.Group + "/" + g.Version
}

// TypeInfo holds metadata about a Kubernetes resource type.
type TypeInfo struct {
	Plural     string
	Namespaced bool
}

// LookupType returns the TypeInfo for a given GVK. For known types, it uses
// the built-in lookup table. For unknown types, it applies English
// pluralization and assumes namespaced scope.
func LookupType(gvk GVK) TypeInfo {
	if info, ok := knownTypes[gvk]; ok {
		return info
	}
	return TypeInfo{
		Plural:     Pluralize(gvk.Kind),
		Namespaced: true,
	}
}

// Pluralize applies English pluralization rules to a kind name,
// returning the lowercase plural form.
func Pluralize(kind string) string {
	s := strings.ToLower(kind)
	if s == "" {
		return s
	}

	n := len(s)

	// Words ending in -sis → -ses (e.g. "analysis" → "analyses").
	if n > 3 && strings.HasSuffix(s, "sis") {
		return s[:n-3] + "ses"
	}

	// Words ending in -is → -es (e.g. "axis" → "axes").
	// But not -sis (handled above) or -lis/-nis etc.
	if n > 2 && s[n-2] == 'i' && s[n-1] == 's' && !(n > 3 && s[n-3] == 's') {
		return s[:n-2] + "es"
	}

	// -s, -x, -z → add "es".
	if s[n-1] == 's' || s[n-1] == 'x' || s[n-1] == 'z' {
		return s + "es"
	}

	// -ch, -sh → add "es".
	if n > 1 && ((s[n-2] == 'c' && s[n-1] == 'h') || (s[n-2] == 's' && s[n-1] == 'h')) {
		return s + "es"
	}

	// Consonant + y → -ies.
	if n > 1 && s[n-1] == 'y' && !isVowel(rune(s[n-2])) {
		return s[:n-1] + "ies"
	}

	return s + "s"
}

func isVowel(r rune) bool {
	return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u'
}

// knownTypes maps known GVKs to their resource metadata.
// Group names are lowercase, versions are exact, kinds are PascalCase.
var knownTypes = map[GVK]TypeInfo{
	// Core v1
	{"", "v1", "Binding"}:                {"bindings", true},
	{"", "v1", "ConfigMap"}:              {"configmaps", true},
	{"", "v1", "Endpoints"}:              {"endpoints", true},
	{"", "v1", "Event"}:                  {"events", true},
	{"", "v1", "LimitRange"}:             {"limitranges", true},
	{"", "v1", "Namespace"}:              {"namespaces", false},
	{"", "v1", "Node"}:                   {"nodes", false},
	{"", "v1", "PersistentVolume"}:       {"persistentvolumes", false},
	{"", "v1", "PersistentVolumeClaim"}:  {"persistentvolumeclaims", true},
	{"", "v1", "Pod"}:                    {"pods", true},
	{"", "v1", "PodTemplate"}:            {"podtemplates", true},
	{"", "v1", "ReplicationController"}:  {"replicationcontrollers", true},
	{"", "v1", "ResourceQuota"}:          {"resourcequotas", true},
	{"", "v1", "Secret"}:                 {"secrets", true},
	{"", "v1", "Service"}:                {"services", true},
	{"", "v1", "ServiceAccount"}:         {"serviceaccounts", true},

	// apps/v1
	{"apps", "v1", "ControllerRevision"}: {"controllerrevisions", true},
	{"apps", "v1", "DaemonSet"}:          {"daemonsets", true},
	{"apps", "v1", "Deployment"}:         {"deployments", true},
	{"apps", "v1", "ReplicaSet"}:         {"replicasets", true},
	{"apps", "v1", "StatefulSet"}:        {"statefulsets", true},

	// autoscaling
	{"autoscaling", "v1", "HorizontalPodAutoscaler"}:  {"horizontalpodautoscalers", true},
	{"autoscaling", "v2", "HorizontalPodAutoscaler"}:  {"horizontalpodautoscalers", true},
	{"autoscaling", "v2beta1", "HorizontalPodAutoscaler"}: {"horizontalpodautoscalers", true},
	{"autoscaling", "v2beta2", "HorizontalPodAutoscaler"}: {"horizontalpodautoscalers", true},

	// batch
	{"batch", "v1", "CronJob"}: {"cronjobs", true},
	{"batch", "v1", "Job"}:     {"jobs", true},

	// certificates.k8s.io
	{"certificates.k8s.io", "v1", "CertificateSigningRequest"}: {"certificatesigningrequests", false},

	// coordination.k8s.io
	{"coordination.k8s.io", "v1", "Lease"}: {"leases", true},

	// discovery.k8s.io
	{"discovery.k8s.io", "v1", "EndpointSlice"}: {"endpointslices", true},

	// events.k8s.io
	{"events.k8s.io", "v1", "Event"}: {"events", true},

	// networking.k8s.io
	{"networking.k8s.io", "v1", "Ingress"}:      {"ingresses", true},
	{"networking.k8s.io", "v1", "IngressClass"}:  {"ingressclasses", false},
	{"networking.k8s.io", "v1", "NetworkPolicy"}: {"networkpolicies", true},

	// node.k8s.io
	{"node.k8s.io", "v1", "RuntimeClass"}: {"runtimeclasses", false},

	// policy
	{"policy", "v1", "PodDisruptionBudget"}: {"poddisruptionbudgets", true},

	// rbac.authorization.k8s.io
	{"rbac.authorization.k8s.io", "v1", "ClusterRole"}:        {"clusterroles", false},
	{"rbac.authorization.k8s.io", "v1", "ClusterRoleBinding"}:  {"clusterrolebindings", false},
	{"rbac.authorization.k8s.io", "v1", "Role"}:                {"roles", true},
	{"rbac.authorization.k8s.io", "v1", "RoleBinding"}:         {"rolebindings", true},

	// storage.k8s.io
	{"storage.k8s.io", "v1", "CSIDriver"}:          {"csidrivers", false},
	{"storage.k8s.io", "v1", "CSINode"}:             {"csinodes", false},
	{"storage.k8s.io", "v1", "CSIStorageCapacity"}:  {"csistoragecapacities", true},
	{"storage.k8s.io", "v1", "StorageClass"}:        {"storageclasses", false},
	{"storage.k8s.io", "v1", "VolumeAttachment"}:    {"volumeattachments", false},

	// apiextensions.k8s.io
	{"apiextensions.k8s.io", "v1", "CustomResourceDefinition"}: {"customresourcedefinitions", false},

	// scheduling.k8s.io
	{"scheduling.k8s.io", "v1", "PriorityClass"}: {"priorityclasses", false},

	// admissionregistration.k8s.io
	{"admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration"}:          {"mutatingwebhookconfigurations", false},
	{"admissionregistration.k8s.io", "v1", "ValidatingAdmissionPolicy"}:             {"validatingadmissionpolicies", false},
	{"admissionregistration.k8s.io", "v1", "ValidatingAdmissionPolicyBinding"}:      {"validatingadmissionpolicybindings", false},
	{"admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration"}:        {"validatingwebhookconfigurations", false},

	// authorization.k8s.io
	{"authorization.k8s.io", "v1", "LocalSubjectAccessReview"}: {"localsubjectaccessreviews", true},
	{"authorization.k8s.io", "v1", "SelfSubjectAccessReview"}:  {"selfsubjectaccessreviews", false},
	{"authorization.k8s.io", "v1", "SelfSubjectRulesReview"}:   {"selfsubjectrulesreviews", false},
	{"authorization.k8s.io", "v1", "SubjectAccessReview"}:      {"subjectaccessreviews", false},

	// authentication.k8s.io
	{"authentication.k8s.io", "v1", "TokenReview"}:  {"tokenreviews", false},
	{"authentication.k8s.io", "v1", "TokenRequest"}: {"tokenrequests", false},

	// gateway.networking.k8s.io
	{"gateway.networking.k8s.io", "v1", "GatewayClass"}:    {"gatewayclasses", false},
	{"gateway.networking.k8s.io", "v1", "Gateway"}:         {"gateways", true},
	{"gateway.networking.k8s.io", "v1", "HTTPRoute"}:       {"httproutes", true},
	{"gateway.networking.k8s.io", "v1", "GRPCRoute"}:       {"grpcroutes", true},
	{"gateway.networking.k8s.io", "v1beta1", "ReferenceGrant"}: {"referencegrants", true},

	// cert-manager.io
	{"cert-manager.io", "v1", "Certificate"}:         {"certificates", true},
	{"cert-manager.io", "v1", "CertificateRequest"}:  {"certificaterequests", true},
	{"cert-manager.io", "v1", "Challenge"}:            {"challenges", true},
	{"cert-manager.io", "v1", "ClusterIssuer"}:        {"clusterissuers", false},
	{"cert-manager.io", "v1", "Issuer"}:               {"issuers", true},
	{"cert-manager.io", "v1", "Order"}:                {"orders", true},

	// argoproj.io
	{"argoproj.io", "v1alpha1", "AnalysisRun"}:      {"analysisruns", true},
	{"argoproj.io", "v1alpha1", "AnalysisTemplate"}:  {"analysistemplates", true},
	{"argoproj.io", "v1alpha1", "Experiment"}:        {"experiments", true},
	{"argoproj.io", "v1alpha1", "Rollout"}:           {"rollouts", true},

	// fluxcd
	{"source.toolkit.fluxcd.io", "v1", "Bucket"}:        {"buckets", true},
	{"source.toolkit.fluxcd.io", "v1", "GitRepository"}:   {"gitrepositories", true},
	{"source.toolkit.fluxcd.io", "v1", "HelmChart"}:       {"helmcharts", true},
	{"source.toolkit.fluxcd.io", "v1", "HelmRepository"}:  {"helmrepositories", true},
	{"source.toolkit.fluxcd.io", "v1", "OCIRepository"}:   {"ocirepositories", true},
	{"kustomize.toolkit.fluxcd.io", "v1", "Kustomization"}: {"kustomizations", true},
	{"helm.toolkit.fluxcd.io", "v2beta1", "HelmRelease"}:  {"helmreleases", true},
}
