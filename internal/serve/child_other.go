//go:build !unix

package serve

import "os/exec"

// Platforms without Unix process groups retain CommandContext's immediate-child
// cancellation. Descendant termination is only guaranteed on supported Unix.
func configureChildCancellation(_ *exec.Cmd) {}
