//go:build windows

package system

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")

	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetTickCount64           = kernel32.NewProc("GetTickCount64")
	procGlobalMemoryStatusEx     = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetLogicalDrives         = kernel32.NewProc("GetLogicalDrives")
	procGetDiskFreeSpaceExW      = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetVolumeInformationW    = kernel32.NewProc("GetVolumeInformationW")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

const (
	hkeyLocalMachine   = 0x80000002
	regKeyRead         = 0x20019
	th32csSnapProcess  = 0x00000002
	invalidHandleValue = ^uintptr(0)
	currentVersionKey  = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	cpuDescriptionKey  = `HARDWARE\DESCRIPTION\System\CentralProcessor\0`
)

func regReadString(root uintptr, path, name string) (string, bool) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	var hKey syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(pathPtr)), 0, regKeyRead, uintptr(unsafe.Pointer(&hKey)))
	if r != 0 {
		return "", false
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var valType, bufLen uint32
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(hKey), uintptr(unsafe.Pointer(namePtr)), 0,
		uintptr(unsafe.Pointer(&valType)), 0, uintptr(unsafe.Pointer(&bufLen)),
	)
	if r != 0 || bufLen == 0 {
		return "", false
	}
	buf := make([]uint16, bufLen/2+1)
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(hKey), uintptr(unsafe.Pointer(namePtr)), 0,
		uintptr(unsafe.Pointer(&valType)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bufLen)),
	)
	if r != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func archLabel() string {
	switch runtime.GOARCH {
	case "amd64":
		return "64-bit"
	case "arm64":
		return "ARM64"
	default:
		return runtime.GOARCH
	}
}

func osVersionInfo() string {
	name, _ := regReadString(hkeyLocalMachine, currentVersionKey, "ProductName")
	build, _ := regReadString(hkeyLocalMachine, currentVersionKey, "CurrentBuildNumber")
	if name != "" && build != "" {
		return fmt.Sprintf("%s (Build %s)", name, build)
	}
	return name
}

func platformKernelInfo() map[string]any {
	out := map[string]any{}
	if v, ok := regReadString(hkeyLocalMachine, currentVersionKey, "CurrentBuildNumber"); ok {
		out["release"] = v
	}
	return out
}

func platformInfo() map[string]any {
	out := map[string]any{"os": "windows"}
	if v, ok := regReadString(hkeyLocalMachine, currentVersionKey, "ProductName"); ok {
		out["Caption"] = v
	}
	if v, ok := regReadString(hkeyLocalMachine, currentVersionKey, "CurrentBuildNumber"); ok {
		out["BuildNumber"] = v
	}
	if v, ok := regReadString(hkeyLocalMachine, currentVersionKey, "DisplayVersion"); ok {
		out["Version"] = v
	} else if v, ok := regReadString(hkeyLocalMachine, currentVersionKey, "ReleaseId"); ok {
		out["Version"] = v
	}
	out["OSArchitecture"] = archLabel()
	return out
}

func platformCPUInfo() map[string]any {
	out := map[string]any{"logical": runtime.NumCPU()}
	if name, ok := regReadString(hkeyLocalMachine, cpuDescriptionKey, "ProcessorNameString"); ok {
		out["model"] = strings.TrimSpace(name)
	}
	return out
}

func platformUptimeInfo() map[string]any {
	r, _, _ := procGetTickCount64.Call()
	seconds := uint64(r) / 1000
	return map[string]any{
		"seconds": seconds,
		"human":   (time.Duration(seconds) * time.Second).String(),
	}
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func platformMemoryInfo() map[string]any {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return map[string]any{"supported": false}
	}
	return map[string]any{
		"MemoryLoad":    m.MemoryLoad,
		"TotalPhys":     m.TotalPhys,
		"AvailPhys":     m.AvailPhys,
		"TotalPageFile": m.TotalPageFile,
		"AvailPageFile": m.AvailPageFile,
		"TotalVirtual":  m.TotalVirtual,
		"AvailVirtual":  m.AvailVirtual,
	}
}

func platformDiskInfo() []map[string]any {
	out := []map[string]any{}
	r, _, _ := procGetLogicalDrives.Call()
	mask := uint32(r)
	if mask == 0 {
		return out
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + `:\`
		path, err := syscall.UTF16PtrFromString(letter)
		if err != nil {
			continue
		}

		var freeAvail, total, totalFree uint64
		ret, _, _ := procGetDiskFreeSpaceExW.Call(
			uintptr(unsafe.Pointer(path)),
			uintptr(unsafe.Pointer(&freeAvail)),
			uintptr(unsafe.Pointer(&total)),
			uintptr(unsafe.Pointer(&totalFree)),
		)
		if ret == 0 || total == 0 {
			continue
		}

		volName := make([]uint16, 261)
		fsName := make([]uint16, 261)
		_, _, _ = procGetVolumeInformationW.Call(
			uintptr(unsafe.Pointer(path)),
			uintptr(unsafe.Pointer(&volName[0])), uintptr(len(volName)),
			0, 0, 0,
			uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)),
		)

		used := total - totalFree
		out = append(out, map[string]any{
			"filesystem": syscall.UTF16ToString(fsName),
			"size":       total,
			"used":       used,
			"available":  freeAvail,
			"usePercent": fmt.Sprintf("%d%%", percentOf(used, total)),
			"mount":      letter,
			"volumeName": syscall.UTF16ToString(volName),
		})
	}
	return out
}

type processEntry32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

func platformProcList() []map[string]any {
	out := []map[string]any{}
	h, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if h == 0 || h == invalidHandleValue {
		return out
	}
	defer procCloseHandle.Call(h)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	r, _, _ := procProcess32FirstW.Call(h, uintptr(unsafe.Pointer(&entry)))
	for r != 0 {
		out = append(out, map[string]any{
			"pid":  entry.ProcessID,
			"ppid": entry.ParentProcessID,
			"name": syscall.UTF16ToString(entry.ExeFile[:]),
		})
		entry = processEntry32W{Size: uint32(unsafe.Sizeof(entry))}
		r, _, _ = procProcess32NextW.Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	return out
}

func platformProcListSupported() bool { return true }
