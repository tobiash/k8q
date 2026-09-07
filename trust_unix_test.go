//go:build unix

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServeCLICancelsDescendants(t *testing.T) {
	// The descendant inherits stdout and outlives its shell if only the shell
	// is killed. runCLI waits for EOF on both output pipes, not just CLI exit.
	script := `printf '%s\n' "$KUBECONFIG"
(sleep 5; printf 'descendant survived cancellation\n') &
kill -TERM "$PPID"
wait`
	start := time.Now()
	out, stderr, code := runCLI(t, "", "serve", "--", "sh", "-c", script)
	if elapsed := time.Since(start); elapsed >= 4*time.Second {
		t.Errorf("cancellation took %s; inherited output pipes did not close promptly", elapsed)
	}
	if code != 2 {
		t.Errorf("cancelled serve exit = %d, want 2; stderr=%s", code, stderr)
	}
	if strings.Contains(out, "descendant survived") {
		t.Errorf("descendant survived cancellation and retained stdout: %s", out)
	}
	path, _, _ := strings.Cut(out, "\n")
	if path == "" {
		t.Fatalf("child did not report kubeconfig: stdout=%s stderr=%s", out, stderr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cancelled serve left kubeconfig %s: %v", path, err)
	}
}
