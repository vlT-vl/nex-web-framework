//go:build windows

package updater

import (
	"syscall"
	"unsafe"
)

var kernel32Space = syscall.NewLazyDLL("kernel32.dll")
var getDiskFreeSpaceExW = kernel32Space.NewProc("GetDiskFreeSpaceExW")

func availableSpace(path string) (uint64, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	result, _, callErr := getDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&available)), 0, 0)
	if result == 0 {
		return 0, callErr
	}
	return available, nil
}
