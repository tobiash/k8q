//go:build !unix

package serve

import "os/exec"

// Platforms without Unix process groups retain CommandContext's immediate-child
// cancellation. Descendant termination is only guaranteed on supported Unix.
func configureChildCancellation(_ *exec.Cmd) (func() error, error) {
	return func() error { return nil }, nil
}

func cleanupChild(_ *exec.Cmd) error {
	// There is no owned group to clean up. CommandContext already handled any
	// in-flight cancellation; killing a waited Windows process returns EINVAL.
	return nil
}
