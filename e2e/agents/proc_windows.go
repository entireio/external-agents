//go:build windows

package agents

import (
	"os/exec"
	"syscall"
)

func configureCmdProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	cmd.Cancel = func() error {
		return cmd.Process.Kill()
	}
}
