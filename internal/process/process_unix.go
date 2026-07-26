//go:build darwin || linux

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

var errProcessDone = os.ErrProcessDone

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return errProcessDone
	}
	return err
}

func killProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func exitSignal(exitErr *exec.ExitError) string {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
