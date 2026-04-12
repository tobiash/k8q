package serve

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// Kubeconfig holds the generated kubeconfig file path and temp directory.
type Kubeconfig struct {
	// Dir is the temporary directory holding the kubeconfig.
	Dir string
	// Path is the file path of the kubeconfig.
	Path string
}

// WriteKubeconfig generates a kubeconfig file pointing at the mock server.
func WriteKubeconfig(port int, caCertPEM []byte) (*Kubeconfig, error) {
	dir, err := os.MkdirTemp("", "k8q-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	caData := base64.StdEncoding.EncodeToString(caCertPEM)

	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:%d
    certificate-authority-data: %s
  name: k8q
contexts:
- context:
    cluster: k8q
    user: k8q
  name: k8q
current-context: k8q
users:
- name: k8q
  user:
    token: k8q
`, port, caData)

	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("writing kubeconfig: %w", err)
	}

	return &Kubeconfig{Dir: dir, Path: path}, nil
}

// Cleanup removes the temp directory and kubeconfig file.
func (k *Kubeconfig) Cleanup() {
	if k.Dir != "" {
		_ = os.RemoveAll(k.Dir)
	}
}
