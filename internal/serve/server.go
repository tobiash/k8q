package serve

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// exitCodeError is returned when a child process exits with a non-zero code.
type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("child exited with code %d", int(e))
}

// ExitCode extracts the exit code from an error. Returns (code, true) if the
// error is a child exit code, or (0, false) otherwise.
func ExitCode(err error) (int, bool) {
	if e, ok := err.(exitCodeError); ok {
		return int(e), true
	}
	return 0, false
}

// Run is the main entry point for the serve command. It reads manifests from in,
// starts a mock API server, and either executes the given command with a
// kubeconfig pointing to the server or waits for a signal (interactive mode).
// Returns nil on success or an exitCodeError when the child exits non-zero.
func Run(in io.Reader, port int, command []string) error {
	objects, err := ParseManifests(in)
	if err != nil {
		return fmt.Errorf("parsing manifests: %w", err)
	}

	store := NewStore(objects)

	srv, err := NewServer(store, port)
	if err != nil {
		return fmt.Errorf("starting server: %w", err)
	}
	_ = srv.Shutdown(context.Background())
	defer srv.Cleanup()

	go func() { _ = srv.Serve() }()

	if len(command) > 0 {
		return execChild(command, srv.KubeconfigPath())
	}

	fmt.Fprintf(os.Stderr, "kubeconfig: %s\n", srv.KubeconfigPath())
	fmt.Fprintf(os.Stderr, "server: https://%s\n", srv.Addr())
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n")
	waitForSignal()
	return nil
}

// Server is a mock Kubernetes API server that serves piped-in manifests.
type Server struct {
	store    *Store
	listener net.Listener
	server   *http.Server
	addr     string
	cfg      *Kubeconfig
	caCert   []byte
}

// NewServer creates a new mock API server. If port is 0, an ephemeral port is used.
func NewServer(store *Store, port int) (*Server, error) {
	caCertPEM, tlsCert, err := generateTLS()
	if err != nil {
		return nil, fmt.Errorf("generating TLS: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("listening: %w", err)
	}

	s := &Server{
		store:    store,
		listener: listener,
		caCert:   caCertPEM,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Handler:      mux,
		IdleTimeout:  120 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.addr = listener.Addr().String()

	// Determine the actual port (in case port was 0).
	_, portStr, _ := net.SplitHostPort(s.addr)
	var portNum int
	_, _ = fmt.Sscanf(portStr, "%d", &portNum)

	cfg, err := WriteKubeconfig(portNum, caCertPEM)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("writing kubeconfig: %w", err)
	}
	s.cfg = cfg

	return s, nil
}

// Addr returns the server's listen address (host:port).
func (s *Server) Addr() string {
	return s.addr
}

// KubeconfigPath returns the path to the generated kubeconfig file.
func (s *Server) KubeconfigPath() string {
	return s.cfg.Path
}

// Serve starts the server. It blocks until the server exits or is shut down.
func (s *Server) Serve() error {
	return s.server.Serve(s.listener)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Cleanup removes the temp kubeconfig directory.
func (s *Server) Cleanup() {
	if s.cfg != nil {
		s.cfg.Cleanup()
	}
}

// registerRoutes sets up all HTTP handlers on the given mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Discovery
	mux.HandleFunc("GET /api", s.handleAPI)
	mux.HandleFunc("GET /api/v1", s.handleAPIV1)
	mux.HandleFunc("GET /apis", s.handleAPIs)
	mux.HandleFunc("GET /apis/{group}/{version}", s.handleGroupVersionDiscovery)

	// Core v1 resources — longer patterns first for correct routing.
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/{resource}/{name}", s.handleGetCoreNamespaced)
	mux.HandleFunc("GET /api/v1/namespaces/{ns}/{resource}", s.handleListCoreNamespaced)
	mux.HandleFunc("GET /api/v1/{resource}/{name}", s.handleGetCoreCluster)
	mux.HandleFunc("GET /api/v1/{resource}", s.handleListCoreCluster)

	// Named group resources — longer patterns first.
	mux.HandleFunc("GET /apis/{group}/{version}/namespaces/{ns}/{resource}/{name}", s.handleGetGroupNamespaced)
	mux.HandleFunc("GET /apis/{group}/{version}/namespaces/{ns}/{resource}", s.handleListGroupNamespaced)
	mux.HandleFunc("GET /apis/{group}/{version}/{resource}/{name}", s.handleGetGroupCluster)
	mux.HandleFunc("GET /apis/{group}/{version}/{resource}", s.handleListGroupCluster)

	// Auth
	mux.HandleFunc("POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews", s.handleSelfSubjectAccessReview)
	mux.HandleFunc("POST /apis/authorization.k8s.io/v1/selfsubjectrulesreviews", s.handleSelfSubjectRulesReview)
}

// --- Discovery handlers ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAPI(w http.ResponseWriter, _ *http.Request) {
	_, port, _ := net.SplitHostPort(s.addr)
	writeJSON(w, http.StatusOK, buildAPIVersions("127.0.0.1:"+port))
}

func (s *Server) handleAPIV1(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildAPIV1Resources(s.store))
}

func (s *Server) handleAPIs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildAPIGroupList(s.store))
}

func (s *Server) handleGroupVersionDiscovery(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	version := r.PathValue("version")
	writeJSON(w, http.StatusOK, buildAPIResourceList(s.store, group, version))
}

// --- Core v1 resource handlers ---

func (s *Server) handleListCoreCluster(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{Group: "", Version: "v1", Resource: r.PathValue("resource")}
	sel := parseSelector(r.URL.Query().Get("labelSelector"))
	s.respondList(w, gvr, "", sel)
}

func (s *Server) handleGetCoreCluster(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{Group: "", Version: "v1", Resource: r.PathValue("resource")}
	s.respondGet(w, gvr, "", r.PathValue("name"))
}

func (s *Server) handleListCoreNamespaced(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{Group: "", Version: "v1", Resource: r.PathValue("resource")}
	sel := parseSelector(r.URL.Query().Get("labelSelector"))
	s.respondList(w, gvr, r.PathValue("ns"), sel)
}

func (s *Server) handleGetCoreNamespaced(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{Group: "", Version: "v1", Resource: r.PathValue("resource")}
	s.respondGet(w, gvr, r.PathValue("ns"), r.PathValue("name"))
}

// --- Named group resource handlers ---

func (s *Server) handleListGroupCluster(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{
		Group:    r.PathValue("group"),
		Version:  r.PathValue("version"),
		Resource: r.PathValue("resource"),
	}
	sel := parseSelector(r.URL.Query().Get("labelSelector"))
	s.respondList(w, gvr, "", sel)
}

func (s *Server) handleGetGroupCluster(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{
		Group:    r.PathValue("group"),
		Version:  r.PathValue("version"),
		Resource: r.PathValue("resource"),
	}
	s.respondGet(w, gvr, "", r.PathValue("name"))
}

func (s *Server) handleListGroupNamespaced(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{
		Group:    r.PathValue("group"),
		Version:  r.PathValue("version"),
		Resource: r.PathValue("resource"),
	}
	sel := parseSelector(r.URL.Query().Get("labelSelector"))
	s.respondList(w, gvr, r.PathValue("ns"), sel)
}

func (s *Server) handleGetGroupNamespaced(w http.ResponseWriter, r *http.Request) {
	gvr := GVR{
		Group:    r.PathValue("group"),
		Version:  r.PathValue("version"),
		Resource: r.PathValue("resource"),
	}
	s.respondGet(w, gvr, r.PathValue("ns"), r.PathValue("name"))
}

// --- Auth handlers ---

func (s *Server) handleSelfSubjectAccessReview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"kind":       "SelfSubjectAccessReview",
		"apiVersion": "authorization.k8s.io/v1",
		"metadata":   map[string]interface{}{},
		"status":     map[string]interface{}{"allowed": true},
	})
}

func (s *Server) handleSelfSubjectRulesReview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"kind":       "SelfSubjectRulesReview",
		"apiVersion": "authorization.k8s.io/v1",
		"metadata":   map[string]interface{}{},
		"status": map[string]interface{}{
			"resourceRules": []interface{}{
				map[string]interface{}{
					"verbs":     []string{"*"},
					"apiGroups": []string{"*"},
					"resources": []string{"*"},
				},
			},
			"nonResourceRules": []interface{}{
				map[string]interface{}{
					"verbs":           []string{"*"},
					"nonResourceURLs": []string{"*"},
				},
			},
		},
	})
}

// --- Response helpers ---

func (s *Server) respondList(w http.ResponseWriter, gvr GVR, ns string, sel labels.Selector) {
	items := s.store.List(gvr, ns, sel)

	kind := s.store.KindForGVR(gvr)
	if kind == "" {
		kind = "Unknown"
	}

	apiVersion := gvr.Version
	if gvr.Group != "" {
		apiVersion = gvr.Group + "/" + gvr.Version
	}

	list := &unstructured.UnstructuredList{}
	list.SetKind(kind + "List")
	list.SetAPIVersion(apiVersion)
	list.SetResourceVersion("1")
	list.Items = items

	writeJSON(w, http.StatusOK, list)
}

func (s *Server) respondGet(w http.ResponseWriter, gvr GVR, ns, name string) {
	obj := s.store.Get(gvr, ns, name)
	if obj == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", gvr.Resource, name))
		return
	}
	writeJSON(w, http.StatusOK, obj)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":       "Status",
		"apiVersion": "v1",
		"metadata":   map[string]interface{}{},
		"status":     "Failure",
		"message":    message,
		"code":       code,
	})
}

func parseSelector(s string) labels.Selector {
	if s == "" {
		return labels.Everything()
	}
	sel, err := labels.Parse(s)
	if err != nil {
		return labels.Everything()
	}
	return sel
}

// --- Child process execution ---

func execChild(command []string, kubeconfigPath string) error {
	cmd := exec.Command(command[0], command[1:]...) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)

	err := cmd.Run()
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitCodeError(exitErr.ExitCode())
	}

	return fmt.Errorf("executing %s: %w", strings.Join(command, " "), err)
}

// waitForSignal blocks until SIGINT or SIGTERM is received.
func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

// --- TLS generation ---

func generateTLS() (caCertPEM []byte, tlsCert tls.Certificate, err error) {
	// Generate CA key and certificate.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("generating CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		Subject:               pkix.Name{CommonName: "k8q CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("creating CA certificate: %w", err)
	}

	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	// Generate server key and certificate.
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("generating server key: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		Subject:      pkix.Name{CommonName: "k8q"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	caCert, _ := x509.ParseCertificate(caCertDER)
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("creating server certificate: %w", err)
	}

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("marshaling server key: %w", err)
	}

	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER})

	tlsCert, err = tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("creating TLS certificate: %w", err)
	}

	return caCertPEM, tlsCert, nil
}
