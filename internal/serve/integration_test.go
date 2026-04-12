package serve

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// testManifests contains a Deployment and ConfigMap in the default namespace.
const testManifests = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.25
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
`

// startTestServer creates a Store from the given manifests, creates a Server,
// starts it in the background, and returns the server and a TLS client ready
// to use. The caller must defer srv.Shutdown(ctx) and srv.Cleanup().
func startTestServer(t *testing.T, manifests string) (*Server, *http.Client) {
	t.Helper()

	objects, err := ParseManifests(strings.NewReader(manifests))
	if err != nil {
		t.Fatalf("ParseManifests: %v", err)
	}

	store := NewStore(objects)
	srv, err := NewServer(store, 0)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() { _ = srv.Serve() }()

	// Wait for the server to accept connections.
	_, port, _ := net.SplitHostPort(srv.Addr())
	addr := "127.0.0.1:" + port
	for i := 0; i < 100; i++ {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Millisecond}, "tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Build an HTTP client that trusts the server's self-signed CA.
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(srv.caCert) {
		t.Fatal("failed to parse server CA cert")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		},
		Timeout: 5 * time.Second,
	}

	return srv, client
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request to %s: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func post(t *testing.T, client *http.Client, url string, body string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building POST to %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return b
}

func mustDecodeJSON(t *testing.T, data []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decoding JSON: %v\nbody: %s", err, data)
	}
}

// decodePEMBlocks splits PEM data into individual blocks.
func decodePEMBlocks(pemData []byte) []*pem.Block {
	var blocks []*pem.Block
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// --- TestServerDirect ---

//nolint:gocyclo
func TestServerDirect(t *testing.T) {
	t.Parallel()

	srv, client := startTestServer(t, testManifests)
	defer func() { _ = srv.Shutdown(context.Background()) }()
	defer srv.Cleanup()

	base := "https://" + srv.Addr()

	t.Run("Healthz", func(t *testing.T) {
		resp := get(t, client, base+"/healthz")
		if resp.StatusCode != http.StatusOK {
			body := readBody(t, resp)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var result map[string]string
		mustDecodeJSON(t, readBody(t, resp), &result)
		if result["status"] != "ok" {
			t.Errorf("expected status=ok, got %q", result["status"])
		}
	})

	t.Run("ListDeployments", func(t *testing.T) {
		resp := get(t, client, base+"/apis/apps/v1/namespaces/default/deployments")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var list unstructured.UnstructuredList
		mustDecodeJSON(t, body, &list)
		if list.GetKind() != "DeploymentList" {
			t.Errorf("expected kind DeploymentList, got %q", list.GetKind())
		}
		if len(list.Items) == 0 {
			t.Fatal("expected at least one deployment, got zero")
		}
		found := false
		for _, item := range list.Items {
			if item.GetName() == "nginx" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("deployment nginx not found in list; items: %v", itemNames(list.Items))
		}
	})

	t.Run("ListConfigMaps", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/namespaces/default/configmaps")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var list unstructured.UnstructuredList
		mustDecodeJSON(t, body, &list)
		if list.GetKind() != "ConfigMapList" {
			t.Errorf("expected kind ConfigMapList, got %q", list.GetKind())
		}
		found := false
		for _, item := range list.Items {
			if item.GetName() == "test-cm" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("configmap test-cm not found; items: %v", itemNames(list.Items))
		}
	})

	t.Run("ListNamespaces", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/namespaces")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var list unstructured.UnstructuredList
		mustDecodeJSON(t, body, &list)
		if list.GetKind() != "NamespaceList" {
			t.Errorf("expected kind NamespaceList, got %q", list.GetKind())
		}
		found := false
		for _, item := range list.Items {
			if item.GetName() == "default" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("synthesized namespace 'default' not found; items: %v", itemNames(list.Items))
		}
	})

	t.Run("GetDeployment", func(t *testing.T) {
		resp := get(t, client, base+"/apis/apps/v1/namespaces/default/deployments/nginx")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var obj unstructured.Unstructured
		mustDecodeJSON(t, body, &obj)
		if obj.GetName() != "nginx" {
			t.Errorf("expected name=nginx, got %q", obj.GetName())
		}
		if obj.GetKind() != "Deployment" {
			t.Errorf("expected kind Deployment, got %q", obj.GetKind())
		}
	})

	t.Run("GetConfigMap", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/namespaces/default/configmaps/test-cm")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var obj unstructured.Unstructured
		mustDecodeJSON(t, body, &obj)
		if obj.GetName() != "test-cm" {
			t.Errorf("expected name=test-cm, got %q", obj.GetName())
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/namespaces/default/pods/my-pod")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
		}
		var status metav1.Status
		mustDecodeJSON(t, body, &status)
		if status.Status != "Failure" {
			t.Errorf("expected status=Failure, got %q", status.Status)
		}
		if status.Code != http.StatusNotFound {
			t.Errorf("expected code 404, got %d", status.Code)
		}
	})

	t.Run("APIVersions", func(t *testing.T) {
		resp := get(t, client, base+"/api")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var versions metav1.APIVersions
		mustDecodeJSON(t, body, &versions)
		if versions.Kind != "APIVersions" {
			t.Errorf("expected kind APIVersions, got %q", versions.Kind)
		}
		found := false
		for _, v := range versions.Versions {
			if v == "v1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected v1 in versions, got %v", versions.Versions)
		}
	})

	t.Run("APIGroupList", func(t *testing.T) {
		resp := get(t, client, base+"/apis")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var groupList metav1.APIGroupList
		mustDecodeJSON(t, body, &groupList)
		if groupList.Kind != "APIGroupList" {
			t.Errorf("expected kind APIGroupList, got %q", groupList.Kind)
		}
		found := false
		for _, g := range groupList.Groups {
			if g.Name == "apps" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'apps' group in APIGroupList, got %v", groupNames(groupList.Groups))
		}
	})

	t.Run("APIResourceList", func(t *testing.T) {
		resp := get(t, client, base+"/apis/apps/v1")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var resourceList metav1.APIResourceList
		mustDecodeJSON(t, body, &resourceList)
		if resourceList.Kind != "APIResourceList" {
			t.Errorf("expected kind APIResourceList, got %q", resourceList.Kind)
		}
		found := false
		for _, r := range resourceList.APIResources {
			if r.Name == "deployments" {
				found = true
				if r.Kind != "Deployment" {
					t.Errorf("expected kind Deployment for deployments resource, got %q", r.Kind)
				}
				if !r.Namespaced {
					t.Error("expected deployments to be namespaced")
				}
				break
			}
		}
		if !found {
			t.Errorf("deployments not found in APIResourceList; resources: %v", resourceNames(resourceList.APIResources))
		}
	})

	t.Run("APIV1Resources", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var resourceList metav1.APIResourceList
		mustDecodeJSON(t, body, &resourceList)
		found := false
		for _, r := range resourceList.APIResources {
			if r.Name == "configmaps" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("configmaps not found in /api/v1 resources; got %v", resourceNames(resourceList.APIResources))
		}
	})

	t.Run("SelfSubjectAccessReview", func(t *testing.T) {
		resp := post(t, client, base+"/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", `{
			"kind": "SelfSubjectAccessReview",
			"apiVersion": "authorization.k8s.io/v1",
			"metadata": {},
			"spec": {
				"resourceAttributes": {
					"namespace": "default",
					"verb": "get",
					"resource": "deployments"
				}
			}
		}`)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var result map[string]interface{}
		mustDecodeJSON(t, body, &result)
		status, ok := result["status"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected status object, got %T", result["status"])
		}
		allowed, ok := status["allowed"].(bool)
		if !ok || !allowed {
			t.Errorf("expected allowed=true, got %v", status["allowed"])
		}
	})

	t.Run("SelfSubjectRulesReview", func(t *testing.T) {
		resp := post(t, client, base+"/apis/authorization.k8s.io/v1/selfsubjectrulesreviews", `{
			"kind": "SelfSubjectRulesReview",
			"apiVersion": "authorization.k8s.io/v1",
			"metadata": {},
			"spec": {
				"namespace": "default"
			}
		}`)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var result map[string]interface{}
		mustDecodeJSON(t, body, &result)
		if result["kind"] != "SelfSubjectRulesReview" {
			t.Errorf("expected kind SelfSubjectRulesReview, got %v", result["kind"])
		}
	})

	t.Run("KubeconfigGenerated", func(t *testing.T) {
		path := srv.KubeconfigPath()
		if path == "" {
			t.Fatal("KubeconfigPath returned empty string")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("kubeconfig file does not exist at %s: %v", path, err)
		}
		data, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			t.Fatalf("reading kubeconfig: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "server: https://127.0.0.1:") {
			t.Errorf("kubeconfig does not contain expected server URL")
		}
		if !strings.Contains(content, "certificate-authority-data:") {
			t.Errorf("kubeconfig does not contain CA data")
		}
	})

	t.Run("CACertIsValid", func(t *testing.T) {
		blocks := decodePEMBlocks(srv.caCert)
		if len(blocks) == 0 {
			t.Fatal("caCert contains no PEM blocks")
		}
		cert, err := x509.ParseCertificate(blocks[0].Bytes)
		if err != nil {
			t.Fatalf("parsing CA cert: %v", err)
		}
		if !cert.IsCA {
			t.Error("CA cert does not have IsCA=true")
		}
		if cert.Subject.CommonName != "k8q CA" {
			t.Errorf("expected CN 'k8q CA', got %q", cert.Subject.CommonName)
		}
	})
}

// --- TestKubectlIntegration ---

func TestKubectlIntegration(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not found in PATH")
	}

	// Build the k8q binary to a temp directory.
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "k8q")

	// Determine the repo root (go.mod lives in the serve worktree root).
	repoRoot := filepath.Join("..", "..")
	goMod := filepath.Join(repoRoot, "go.mod")
	if _, err := os.Stat(goMod); err != nil {
		t.Fatalf("cannot find go.mod at %s: %v", goMod, err)
	}

	buildCmd := exec.Command("go", "build", "-o", binPath, ".") //nolint:gosec
	buildCmd.Dir = repoRoot
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("building k8q binary: %v", err)
	}

	srv, _ := startTestServer(t, testManifests)
	defer func() { _ = srv.Shutdown(context.Background()) }()
	defer srv.Cleanup()

	base := "https://" + srv.Addr()

	// kubectl needs to trust the server's CA. Write the CA cert to a temp file
	// and pass it via --certificate-authority.
	caCertPath := filepath.Join(tmpDir, "ca.crt")
	if err := os.WriteFile(caCertPath, srv.caCert, 0600); err != nil {
		t.Fatalf("writing CA cert: %v", err)
	}

	// Extract the token from the generated kubeconfig (it's hardcoded to "k8q").
	token := "k8q"

	kubectl := func(args ...string) string {
		t.Helper()
		allArgs := append([]string{
			"--server=" + base,
			"--certificate-authority=" + caCertPath,
			"--token=" + token,
		}, args...)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "kubectl", allArgs...) //nolint:gosec
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("kubectl %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr.String())
		}
		return stdout.String()
	}

	t.Run("GetDeployments", func(t *testing.T) {
		output := kubectl("get", "deployments", "-o", "name", "-n", "default")
		if !strings.Contains(output, "deployment.apps/nginx") {
			t.Errorf("expected 'deployment.apps/nginx' in output, got:\n%s", output)
		}
	})

	t.Run("GetConfigMaps", func(t *testing.T) {
		output := kubectl("get", "configmaps", "-o", "name", "-n", "default")
		if !strings.Contains(output, "configmap/test-cm") {
			t.Errorf("expected 'configmap/test-cm' in output, got:\n%s", output)
		}
	})

	t.Run("GetNamespaces", func(t *testing.T) {
		output := kubectl("get", "namespaces", "-o", "name")
		if !strings.Contains(output, "namespace/default") {
			t.Errorf("expected 'namespace/default' in output, got:\n%s", output)
		}
	})

	t.Run("GetDeploymentDirect", func(t *testing.T) {
		output := kubectl("get", "deployment", "nginx", "-o", "name", "-n", "default")
		if !strings.Contains(output, "deployment.apps/nginx") {
			t.Errorf("expected 'deployment.apps/nginx' in output, got:\n%s", output)
		}
	})

	// Use the generated kubeconfig file directly instead of manual flags.
	t.Run("WithKubeconfigFile", func(t *testing.T) {
		kubeconfigPath := srv.KubeconfigPath()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "kubectl", //nolint:gosec
			"--kubeconfig="+kubeconfigPath,
			"get", "deployments", "-o", "name", "-n", "default",
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("kubectl with kubeconfig: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "deployment.apps/nginx") {
			t.Errorf("expected 'deployment.apps/nginx' in output, got:\n%s", stdout.String())
		}
	})
}

// --- Helpers ---

func itemNames(items []unstructured.Unstructured) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.GetName()
	}
	return names
}

func groupNames(groups []metav1.APIGroup) []string {
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	return names
}

func resourceNames(resources []metav1.APIResource) []string {
	names := make([]string, len(resources))
	for i, r := range resources {
		names[i] = r.Name
	}
	return names
}
