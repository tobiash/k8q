//go:build unix

package serve

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureChildCancellation(cmd *exec.Cmd) (func() error, error) {
	// Isolate the child from k8q's group so cancellation also kills descendants
	// holding inherited pipes, without signalling k8q or its caller.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = sync.OnceValue(func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	})

	// /dev/tty also covers commands that prompt there when stdin is a pipe.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.ENODEV) || errors.Is(err, os.ErrNotExist) {
			return func() error { return nil }, nil
		}
		return nil, fmt.Errorf("opening controlling terminal: %w", err)
	}
	foreground, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
	if err != nil {
		_ = tty.Close() // No buffered terminal data to flush; preserve the ioctl error.
		return nil, fmt.Errorf("reading terminal foreground group: %w", err)
	}
	if foreground != syscall.Getpgrp() {
		// A background invocation must not steal its caller's terminal.
		return tty.Close, nil
	}
	cmd.SysProcAttr.Foreground = true
	cmd.SysProcAttr.Ctty = int(tty.Fd())
	return func() error {
		defer func() { _ = tty.Close() }() // This descriptor is only used for ioctls.
		// Go blocks SIGTTOU around the child's foreground ioctl, before exec.
		// Do restoration in that child too: changing signal.Ignore/Notify in
		// this process would interfere with the caller and concurrent goroutines.
		restore := exec.Command("/bin/sh", "-c", ":")
		restore.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Pgid: foreground, Ctty: int(tty.Fd())}
		if err := restore.Run(); err != nil {
			return fmt.Errorf("restoring terminal foreground group: %w", err)
		}
		return nil
	}, nil
}

func cleanupChild(cmd *exec.Cmd) error {
	return cmd.Cancel()
}
