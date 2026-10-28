//go:build windows

package updater

import (
	"os/exec"
	"syscall"
)

func startDetached(path string, environment []string) (*exec.Cmd, error) {
	command := exec.Command(path)
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x08000000, HideWindow: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(0x00100000, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	result, err := syscall.WaitForSingleObject(handle, 0)
	return err == nil && result == syscall.WAIT_TIMEOUT
}
