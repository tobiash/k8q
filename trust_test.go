package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("K8Q_CLI_TEST") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"k8q"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing CLI args")
}

func runCLI(t *testing.T, input string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...) //nolint:gosec // Executes this test binary with test-owned arguments.
	cmd.Env = append(os.Environ(), "K8Q_CLI_TEST=1")
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %s", &stderr)
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), stderr.String(), code
}

func TestCLIJSONTrust(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("NAME=test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers:\n  - name: c\n    resources:\n      requests:\n        cpu: 2000m\n        memory: 1Mi\n"
	for _, tt := range []struct {
		name  string
		args  []string
		input string
		code  int
		field string
	}{
		{"drop", []string{"drop", "--kind=Secret"}, manifest, 0, "items"},
		{"subst", []string{"subst", "--env-file=" + env}, strings.ReplaceAll(manifest, "name: test", "name: ${NAME}"), 0, "items"},
		{"threshold invalid", []string{"sum", "--max-cpu-requests=bad"}, manifest, 2, "error"},
		{"threshold exceeded", []string{"sum", "--max-cpu-requests=1"}, manifest, 1, "assertions"},
		{"threshold passed", []string{"sum", "--max-cpu-requests=3"}, manifest, 0, "assertions"},
		{"missing resources", []string{"sum", "--require-limits"}, manifest, 1, "assertions"},
		{"invalid cpu", []string{"sum", "--max-cpu-requests=100"}, strings.Replace(manifest, "2000m", "bad", 1), 2, "error"},
		{"invalid replicas", []string{"sum", "--max-cpu-requests=100"}, strings.Replace(manifest, "spec:\n", "spec:\n  replicas: broken\n", 1), 2, "error"},
		{"negative replicas", []string{"sum", "--max-cpu-requests=100"}, strings.Replace(manifest, "spec:\n", "spec:\n  replicas: -1\n", 1), 2, "error"},
		{"quoted cpu", []string{"sum", "--max-cpu-requests=1"}, strings.Replace(manifest, "2000m", "'2'", 1), 1, "assertions"},
		{"invalid selector", []string{"get", "--selector=app in ("}, manifest, 2, "error"},
		{"invalid yaml", []string{"get", "--kind=Pod"}, "[invalid", 2, "error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, stderr, code := runCLI(t, tt.input, append([]string{"--output=json"}, tt.args...)...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d; stdout=%s stderr=%s", code, tt.code, out, stderr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("expected exactly one JSON value: %v; output=%s", err, out)
			}
			if result[tt.field] == nil {
				t.Errorf("missing %s in %s", tt.field, out)
			}
			if assertions, ok := result["assertions"].(map[string]any); ok {
				if assertions["passed"] != (tt.code == 0) {
					t.Errorf("assertion passed = %v, exit=%d", assertions["passed"], tt.code)
				}
				if tt.name == "threshold exceeded" && assertions["cpuRequestsExceeded"] != true {
					t.Errorf("missing threshold detail: %s", out)
				}
				if tt.name == "missing resources" && assertions["missingResources"] == nil {
					t.Errorf("missing resource detail: %s", out)
				}
			}
		})
	}
}

func TestCLIDiffOutcomes(t *testing.T) {
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"
	base := filepath.Join(t.TempDir(), "base.yaml")
	if err := os.WriteFile(base, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, input string
		code        int
	}{
		{"identical", manifest, 0},
		{"changed", manifest + "data:\n  key: value\n", 1},
		{"malformed", "[invalid", 2},
		{"duplicate", manifest + "---\n" + manifest, 2},
		{"invalid identity", "kind: ConfigMap\n", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, stderr, code := runCLI(t, tt.input, "diff", "--base="+base, "--output=json")
			if code != tt.code {
				t.Errorf("exit=%d, want %d; out=%s stderr=%s", code, tt.code, out, stderr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("expected exactly one JSON result: %v: %s", err, out)
			}
			if tt.code == 2 && result["error"] == nil {
				t.Errorf("missing error: %s", out)
			}
			if tt.code < 2 && result["modified"] == nil {
				t.Errorf("missing diff arrays: %s", out)
			}
		})
	}
}

func TestCLIParseErrorsJSON(t *testing.T) {
	for _, args := range [][]string{{"--output=json", "get", "--unknown"}, {"get", "--unknown", "--output=json"}, {"get", "--unknown", "-o", "json"}, {"-ojson", "get", "--unknown"}, {"get", "--unknown", "-ojson"}} {
		out, stderr, code := runCLI(t, "", args...)
		var result jsonErrorEnvelope
		if err := json.Unmarshal([]byte(out), &result); err != nil || code != 2 || result.Error.Message == "" {
			t.Errorf("parse %v: code=%d out=%s stderr=%s JSON error=%v", args, code, out, stderr, err)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDiffOutputError(t *testing.T) {
	base := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(base, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := DiffCmd{Base: base}
	g := Globals{In: strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"), Out: failingWriter{}}
	if err := cmd.Run(&g); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("DiffCmd output failure = %v, want closed pipe", err)
	}
}

func TestDescribeActualSchema(t *testing.T) { //nolint:gocyclo // Assert the complete discovery contract together.
	var out bytes.Buffer
	if err := describeCLI(&out, "k8q", "test", "v1", &CLI{}); err != nil {
		t.Fatal(err)
	}
	var desc cliDescription
	if err := json.Unmarshal(out.Bytes(), &desc); err != nil {
		t.Fatal(err)
	}
	commands := map[string]commandDesc{}
	for _, cmd := range desc.Commands {
		commands[cmd.Name] = cmd
	}
	for _, name := range []string{"set-image", "set-namespace", "get", "diff", "serve"} {
		cmd, ok := commands[name]
		if !ok {
			t.Errorf("missing command %s", name)
			continue
		}
		if len(cmd.Args) == 0 {
			t.Errorf("%s missing positional args", name)
		}
		flags := map[string]flagDesc{}
		for _, flag := range cmd.Flags {
			flags[flag.Name] = flag
		}
		if flags["output"].Type != "string" || flags["no-color"].Type != "bool" || flags["file"].Type == "" {
			t.Errorf("%s globals = %+v", name, flags)
		}
	}
	if !commands["serve"].SideEffects || commands["serve"].Idempotent {
		t.Error("serve incorrectly described as safe/read-only")
	}
	if args := commands["get"].Args; len(args) != 1 || args[0].Required {
		t.Errorf("get args = %+v", args)
	}
	if args := commands["set-image"].Args; len(args) != 2 || !args[0].Required || args[1].Required {
		t.Errorf("set-image args = %+v", args)
	}
	for _, flag := range commands["subst"].Flags {
		if flag.Name == "env-file" && !flag.Required {
			t.Error("env-file must be required")
		}
	}
}

func TestServeHealthChild(t *testing.T) { //nolint:gocyclo // Check TLS, health, manifests and cleanup through the real child path.
	if os.Getenv("K8Q_SERVE_PROBE") != "1" {
		return
	}
	data, err := os.ReadFile(os.Getenv("KUBECONFIG")) //nolint:gosec // Kubeconfig is generated by the test's server.
	if err != nil {
		t.Fatal(err)
	}
	n, err := yaml.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	server, err := n.Pipe(yaml.Lookup("clusters", "0", "cluster", "server"))
	if err != nil || server == nil {
		t.Fatalf("kubeconfig server: %v", err)
	}
	ca, err := n.Pipe(yaml.Lookup("clusters", "0", "cluster", "certificate-authority-data"))
	if err != nil || ca == nil {
		t.Fatalf("kubeconfig CA: %v", err)
	}
	cert, err := base64.StdEncoding.DecodeString(ca.YNode().Value)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		t.Fatal("invalid CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, path := range []string{"/healthz", "/api/v1/configmaps"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.YNode().Value+path, nil) //nolint:gosec // URL belongs to the test's loopback server.
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req) //nolint:gosec // Request targets the test's loopback server.
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d %s (%v)", path, resp.StatusCode, body, err)
		}
		if path == "/api/v1/configmaps" && !bytes.Contains(body, []byte(`"name":"test"`)) {
			t.Fatalf("server did not return input manifest: %s", body)
		}
	}
	fmtPath := os.Getenv("K8Q_PROBE_PATH")
	if fmtPath != "" {
		if err := os.WriteFile(fmtPath, []byte(os.Getenv("KUBECONFIG")), 0600); err != nil { //nolint:gosec // Parent supplies a path in t.TempDir.
			t.Fatal(err)
		}
	}
}

func TestServeCLIRealRun(t *testing.T) {
	t.Setenv("K8Q_SERVE_PROBE", "1")
	probePath := filepath.Join(t.TempDir(), "kubeconfig-path")
	t.Setenv("K8Q_PROBE_PATH", probePath)
	out, stderr, code := runCLI(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n", "serve", "--", os.Args[0], "-test.run=^TestServeHealthChild$")
	if code != 0 {
		t.Fatalf("serve exit=%d stdout=%s stderr=%s", code, out, stderr)
	}
	path, err := os.ReadFile(probePath) //nolint:gosec // Test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(path)); !errors.Is(err, os.ErrNotExist) { //nolint:gosec // Path recorded by the test server's child.
		t.Errorf("kubeconfig not cleaned up: %s: %v", path, err)
	}
}

func TestServeExitChild(t *testing.T) {
	if os.Getenv("K8Q_EXIT_CHILD") == "1" {
		os.Exit(17)
	}
}

func TestServeCLIExitStatus(t *testing.T) {
	t.Setenv("K8Q_EXIT_CHILD", "1")
	out, stderr, code := runCLI(t, "", "serve", "--", os.Args[0], "-test.run=^TestServeExitChild$")
	if code != 17 || out != "" || stderr != "" {
		t.Errorf("child status: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
	for _, args := range [][]string{{"serve", "--port=-1"}, {"serve", "--", filepath.Join(t.TempDir(), "missing-program")}} {
		out, stderr, code := runCLI(t, "", append([]string{"--output=json"}, args...)...)
		var result jsonErrorEnvelope
		if err := json.Unmarshal([]byte(out), &result); err != nil || code != 2 || result.Error.Message == "" {
			t.Errorf("serve error: code=%d stdout=%s stderr=%s JSON error=%v", code, out, stderr, err)
		}
	}
}

func TestServeCLISignalCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIProcess$", "--", "serve") //nolint:gosec // Runs this test binary.
	cmd.Env = append(os.Environ(), "K8Q_CLI_TEST=1")
	cmd.Stdin = strings.NewReader("")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stderr).ReadString('\n')
	if err != nil {
		t.Fatalf("waiting for serve startup: %v", err)
	}
	if !strings.HasPrefix(line, "kubeconfig: ") {
		t.Fatalf("unexpected serve startup: %s", line)
	}
	path := strings.TrimSpace(strings.TrimPrefix(line, "kubeconfig: "))
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve signal exit: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("signal left kubeconfig %s: %v", path, err)
	}
}
