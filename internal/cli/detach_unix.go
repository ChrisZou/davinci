//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// detach lets a started server outlive the command that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
