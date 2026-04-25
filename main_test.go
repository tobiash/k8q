package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDescribeCLI(t *testing.T) {
	var out bytes.Buffer
	g := &Globals{Out: &out}
	cli := &CLI{Globals: *g}

	err := describeCLI(&out, "k8q", "Test description", "test-version", cli)
	if err != nil {
		t.Fatalf("describeCLI() error: %v", err)
	}

	var desc cliDescription
	if err := json.Unmarshal(out.Bytes(), &desc); err != nil {
		t.Fatalf("parsing describe output: %v", err)
	}

	if desc.Name != "k8q" {
		t.Errorf("name = %q, want %q", desc.Name, "k8q")
	}
	if desc.Description != "Test description" {
		t.Errorf("description = %q, want %q", desc.Description, "Test description")
	}
	if desc.Version != "test-version" {
		t.Errorf("version = %q, want %q", desc.Version, "test-version")
	}
	if len(desc.Commands) == 0 {
		t.Error("expected non-empty commands list")
	}

	// Verify command names are normalized (e.g., GetCmd -> get)
	var hasGet bool
	for _, cmd := range desc.Commands {
		if cmd.Name == "get" {
			hasGet = true
		}
		if cmd.Name == "getcmd" {
			t.Error("command name not normalized: got getcmd, want get")
		}
	}
	if !hasGet {
		t.Error("expected 'get' command in describe output")
	}
}

func TestDescribeCLIHasDiffFlags(t *testing.T) {
	var out bytes.Buffer
	g := &Globals{Out: &out}
	cli := &CLI{Globals: *g}

	err := describeCLI(&out, "k8q", "Test", "v1", cli)
	if err != nil {
		t.Fatalf("describeCLI() error: %v", err)
	}

	var desc cliDescription
	if err := json.Unmarshal(out.Bytes(), &desc); err != nil {
		t.Fatalf("parsing describe output: %v", err)
	}

	var diffCmd *commandDesc
	for i := range desc.Commands {
		if desc.Commands[i].Name == "diff" {
			diffCmd = &desc.Commands[i]
			break
		}
	}
	if diffCmd == nil {
		t.Fatal("expected 'diff' command in describe output")
	}

	if len(diffCmd.Flags) == 0 {
		t.Error("expected diff command to have flags")
	}
}

func TestResolveInputStdin(t *testing.T) {
	input := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"
	g := &Globals{In: bytes.NewReader([]byte(input))}

	r, err := g.resolveInput()
	if err != nil {
		t.Fatalf("resolveInput() error: %v", err)
	}

	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("reading from resolved input: %v", err)
	}

	if out.String() != input {
		t.Errorf("got %q, want %q", out.String(), input)
	}
}

func TestResolveInputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	g := &Globals{Files: []string{path}}
	r, err := g.resolveInput()
	if err != nil {
		t.Fatalf("resolveInput() error: %v", err)
	}

	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("reading from resolved input: %v", err)
	}

	if out.String() != content {
		t.Errorf("got %q, want %q", out.String(), content)
	}
}

func TestResolveInputMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.yaml")
	path2 := filepath.Join(dir, "b.yaml")
	content1 := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"
	content2 := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: b\n"
	if err := os.WriteFile(path1, []byte(content1), 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
	if err := os.WriteFile(path2, []byte(content2), 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	g := &Globals{Files: []string{path1, path2}}
	r, err := g.resolveInput()
	if err != nil {
		t.Fatalf("resolveInput() error: %v", err)
	}

	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("reading from resolved input: %v", err)
	}

	want := content1 + "\n---\n" + content2
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestResolveInputMissingFile(t *testing.T) {
	g := &Globals{Files: []string{"/nonexistent/path.yaml"}}
	_, err := g.resolveInput()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, ErrUserInput) {
		t.Errorf("expected ErrUserInput, got: %v", err)
	}
}

func TestDiffExitError(t *testing.T) {
	err := DiffExitError(1)
	if err.Error() != "differences found" {
		t.Errorf("got error message %q, want %q", err.Error(), "differences found")
	}

	var e DiffExitError
	if !errors.As(err, &e) {
		t.Error("expected DiffExitError to be detectable via errors.As")
	}
	if e != 1 {
		t.Errorf("got exit code %d, want 1", e)
	}
}

func TestUserError(t *testing.T) {
	err := userError("something went wrong: %s", "detail")
	if !errors.Is(err, ErrUserInput) {
		t.Errorf("expected ErrUserInput, got: %v", err)
	}
	want := "user input error: something went wrong: detail"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestGetCmd_JSONOutput(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
---
apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
`
	var out bytes.Buffer
	g := &Globals{In: bytes.NewReader([]byte(input)), Out: &out, Output: "json"}
	cmd := &GetCmd{Kind: "ConfigMap"}

	err := cmd.Run(g)
	if err != nil {
		t.Fatalf("GetCmd.Run() error = %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if result["apiVersion"] != "v1" {
		t.Errorf("expected apiVersion v1, got %v", result["apiVersion"])
	}
	if result["kind"] != "List" {
		t.Errorf("expected kind List, got %v", result["kind"])
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatal("expected items to be an array")
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0].(map[string]interface{})
	if item["kind"] != "ConfigMap" {
		t.Errorf("expected kind ConfigMap, got %v", item["kind"])
	}
}

func TestCountCmd_JSONOutput(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm2
---
apiVersion: v1
kind: Secret
metadata:
  name: secret1
`
	var out bytes.Buffer
	g := &Globals{In: bytes.NewReader([]byte(input)), Out: &out, Output: "json"}
	cmd := &CountCmd{}

	err := cmd.Run(g)
	if err != nil {
		t.Fatalf("CountCmd.Run() error = %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if result["count"] == nil {
		t.Error("expected count field in JSON output")
	}
}

func TestDiffCmd_ExitCode(t *testing.T) {
	before := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: default
data:
  key: old
`
	after := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: default
data:
  key: new
`
	var out bytes.Buffer
	g := &Globals{In: bytes.NewReader([]byte(after)), Out: &out, Output: "yaml"}
	cmd := &DiffCmd{Files: []string{"-"}}

	// Create a temp file for before
	dir := t.TempDir()
	beforeFile := filepath.Join(dir, "before.yaml")
	if err := os.WriteFile(beforeFile, []byte(before), 0600); err != nil {
		t.Fatalf("writing before file: %v", err)
	}
	cmd.Base = beforeFile

	err := cmd.Run(g)
	if err == nil {
		t.Fatal("expected error for diff with changes")
	}

	var diffErr DiffExitError
	if !errors.As(err, &diffErr) {
		t.Errorf("expected DiffExitError, got: %T %v", err, err)
	}
	if diffErr != 1 {
		t.Errorf("expected exit code 1, got %d", diffErr)
	}
}

func TestDiffCmd_NoChanges(t *testing.T) {
	yaml := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: default
`
	var out bytes.Buffer
	g := &Globals{In: bytes.NewReader([]byte(yaml)), Out: &out, Output: "yaml"}
	cmd := &DiffCmd{Files: []string{"-"}}

	// Create a temp file with same content
	dir := t.TempDir()
	beforeFile := filepath.Join(dir, "before.yaml")
	if err := os.WriteFile(beforeFile, []byte(yaml), 0600); err != nil {
		t.Fatalf("writing before file: %v", err)
	}
	cmd.Base = beforeFile

	err := cmd.Run(g)
	if err != nil {
		t.Fatalf("expected no error for identical manifests, got: %v", err)
	}
}
