//go:build unix

package serve

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func configureChildCancellation(cmd *exec.Cmd) {
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
}
