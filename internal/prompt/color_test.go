package prompt

import (
	"strings"
	"testing"
)

func TestColorizeYAML(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  count: 42
  enabled: true
  description: "hello world"
  missing: null
---
apiVersion: apps/v1
kind: Deployment
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
`

	output := ColorizeYAML(input)

	// Verify output contains ANSI escape codes.
	if !strings.Contains(output, "\033[") {
		t.Error("output contains no ANSI escape codes")
	}

	// Verify document separator is grayed.
	if !strings.Contains(output, "\033[90m---") {
		t.Error("document separator not colorized as gray")
	}

	// Verify keys are bold cyan.
	if !strings.Contains(output, "\033[1m\033[36mapiVersion") {
		t.Error("key 'apiVersion' not bold cyan")
	}

	// Verify numeric values are yellow.
	if !strings.Contains(output, "\033[33m42\033[0m") {
		t.Error("numeric value '42' not yellow")
	}

	// Verify booleans are magenta.
	if !strings.Contains(output, "\033[35mtrue\033[0m") {
		t.Error("boolean 'true' not magenta")
	}

	// Verify null is gray.
	if !strings.Contains(output, "\033[90mnull\033[0m") {
		t.Error("null value not gray")
	}

	// Verify quoted strings are green.
	if !strings.Contains(output, "\033[32m\"hello world\"\033[0m") {
		t.Error("quoted string not green")
	}

	// Verify plain string values are green.
	if !strings.Contains(output, "\033[32mv1\033[0m") {
		t.Error("plain string 'v1' not green")
	}
}

func TestColorizeYAMLEmpty(t *testing.T) {
	if output := ColorizeYAML(""); output != "" {
		t.Errorf("expected empty output for empty input, got %q", output)
	}
}

func TestColorizeYAMLListItems(t *testing.T) {
	input := `items:
  - name: first
  - name: second
`
	output := ColorizeYAML(input)

	// List markers should be bold.
	if !strings.Contains(output, "\033[1m- \033[0m") {
		t.Error("list marker not bold")
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0", true},
		{"42", true},
		{"-1", true},
		{"3.14", true},
		{"1e10", true},
		{"-2.5e-3", true},
		{"", false},
		{"abc", false},
		{"12abc", false},
		{"true", false},
		{"-", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNoColorEnv(t *testing.T) {
	// Test that the function doesn't panic.
	_ = NoColorEnv()
}

func TestColorWriterPassthrough(t *testing.T) {
	var buf strings.Builder
	cw := NewColorWriter(&buf, false)

	input := "apiVersion: v1\n"
	n, err := cw.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != len(input) {
		t.Errorf("Write() returned %d, want %d", n, len(input))
	}
	if err := cw.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	// Passthrough mode: no colorization applied.
	if buf.String() != input {
		t.Errorf("passthrough output = %q, want %q", buf.String(), input)
	}
}

func TestColorWriterColorized(t *testing.T) {
	var buf strings.Builder
	cw := NewColorWriter(&buf, true)

	input := "kind: ConfigMap\n"
	cw.Write([]byte(input))
	cw.Close()

	// Should contain ANSI codes.
	if !strings.Contains(buf.String(), "\033[") {
		t.Errorf("colorized output missing ANSI codes: %q", buf.String())
	}
}
