//go:build !linux

package launcher

import "os/exec"

// Non-Linux platforms are supported for unit-level development only. The
// Kubernetes accelerator runtime target is Linux, where descendant process
// groups are terminated on cancellation.
func configureProcess(cmd *exec.Cmd) {}
