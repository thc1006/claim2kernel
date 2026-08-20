//go:build linux

package launcher

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProcess isolates the kernel in a process group. On cancellation we
// kill the whole group so a timed-out signed kernel cannot strand descendants.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
}
