package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExecChildTerminal(t *testing.T) {
	for _, mode := range []string{"success", "redirected", "cancel", "signal", "trap-130", "trap-0", "start-error"} {
		t.Run(mode, func(t *testing.T) {
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = master.Close() })
			if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = slave.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecChildTerminalProcess$") //nolint:gosec // Runs this test binary.
			cmd.Env = append(os.Environ(), "K8Q_TERMINAL_TEST="+mode)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			cmd.Stdin = slave
			var output terminalOutput
			if strings.HasPrefix(mode, "trap-") {
				output.interrupt = master
			}
			cmd.Stdout, cmd.Stderr = &output, &output
			cmd.WaitDelay = time.Second
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if _, err := master.WriteString("hello\n"); err != nil {
				t.Error(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("terminal helper: %v\n%s", err, &output)
			}
			if mode != "start-error" && !strings.Contains(output.String(), "PROMPT\nREAD:hello\n") {
				t.Errorf("terminal interaction output = %q, want prompt and successful read", &output)
			}
			if strings.Contains(output.String(), "descendant survived") {
				t.Errorf("descendant was not killed: %s", &output)
			}
			if strings.HasPrefix(mode, "trap-") && !strings.Contains(output.String(), "TRAPPED\n") {
				t.Errorf("terminal Ctrl+C did not reach the shell trap: %s", &output)
			}
		})
	}
}

// os/exec serializes writes to the shared stdout/stderr writer. Send the actual
// terminal interrupt only after the descendant has installed its signal ignores.
type terminalOutput struct {
	buffer    bytes.Buffer
	interrupt *os.File
}

func (out *terminalOutput) String() string { return out.buffer.String() }

func (out *terminalOutput) Write(p []byte) (int, error) {
	n, err := out.buffer.Write(p)
	if out.interrupt != nil && strings.Contains(out.String(), "READY\n") {
		_, err = out.interrupt.WriteString("\x03")
		out.interrupt = nil
	}
	return n, err
}

func TestExecChildTerminalProcess(t *testing.T) { //nolint:gocyclo // Check the terminal lifecycle in one isolated process.
	mode := os.Getenv("K8Q_TERMINAL_TEST")
	if mode == "" {
		return
	}
	// A live registration must survive terminal handoff and restoration.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTTOU)
	defer signal.Stop(signals)
	before, err := unix.IoctlGetInt(0, unix.TIOCGPGRP)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	script := `printf 'PROMPT\n'; read value </dev/tty; printf 'READ:%s\n' "$value"`
	if mode == "cancel" {
		script += `; (sleep 5; printf 'descendant survived\n') & wait`
	}
	if mode == "signal" {
		script += `; (sleep 5; printf 'descendant survived\n') & kill -INT $$`
	}
	if code, ok := strings.CutPrefix(mode, "trap-"); ok {
		script += `; trap 'printf "TRAPPED\n"; exit ` + code + `' INT
(trap '' INT HUP; printf 'READY\n'; sleep 5; printf 'descendant survived\n') & wait`
	}
	command := []string{"/bin/sh", "-c", script}
	if mode == "start-error" {
		command = []string{"/nonexistent/k8q-test-command"}
	}
	if mode == "redirected" {
		input, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = input.Close() }()
		os.Stdin = input // Only this isolated helper process is affected.
	}
	err = execChild(ctx, command, "")
	if (mode == "success" || mode == "redirected" || mode == "trap-0") && err != nil {
		t.Errorf("execChild terminal read: %v", err)
	}
	if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("execChild cancellation = %v, want deadline exceeded", err)
	}
	if mode == "start-error" && err == nil {
		t.Error("execChild nonexistent command succeeded")
	}
	if mode == "signal" || mode == "trap-130" {
		var code exitCodeError
		if !errors.As(err, &code) || int(code) != 130 {
			t.Errorf("execChild interrupted = %v, want exit code 130", err)
		}
	}
	if strings.HasPrefix(mode, "trap-") && ctx.Err() != nil {
		t.Errorf("terminal Ctrl+C cancelled parent context: %v", ctx.Err())
	}
	after, err := unix.IoctlGetInt(0, unix.TIOCGPGRP)
	if err != nil || after != before {
		t.Errorf("terminal foreground = %d, %v; want %d", after, err, before)
	}
	select {
	case sig := <-signals:
		t.Errorf("terminal handoff signalled parent: %v", sig)
	default:
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTTOU); err != nil {
		t.Fatal(err)
	}
	select {
	case <-signals:
	case <-time.After(time.Second):
		t.Error("parent SIGTTOU registration was changed")
	}
}
