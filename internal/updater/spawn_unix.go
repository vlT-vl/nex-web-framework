//go:build !windows

package updater

import (
	"errors"
	"os/exec"
	"syscall"
)

func startDetached(path string, environment []string) (*exec.Cmd, error) {
	command := exec.Command(path)
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
