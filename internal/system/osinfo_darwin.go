//go:build darwin

// Darwin implementations of the platform-specific OS-info helpers.
// Values come from the SystemVersion.plist file, the stdlib syscall package's
// Sysctl/SysctlUint32/Getfsstat wrappers, or os.Hostname — no subprocess is
// spawned (no sw_vers, sysctl(8), uname, ps, or df).
//
// Three data points are intentionally reported as unsupported rather than
// decoded from raw sysctl/Mach structs or silently truncated: boot time
// (kern.boottime is a binary timeval, not a NUL-terminated string), total
// physical memory (hw.memsize is a 64-bit sysctl and the stdlib has no
// SysctlUint64 — only SysctlUint32, which would wrap/truncate on any machine
// with >4GB RAM), and the full process table (kern.proc.all is the
// undocumented, version-sensitive kinfo_proc struct). All three would require
// hand-decoding a byte layout that cannot be verified without a real macOS
// build/run cycle — getting it wrong risks a crash via unsafe.Pointer misuse,
// a worse outcome than the subprocess it would replace.
package system

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"runtime"
	"syscall"
)

func osVersionInfo() string {
	version, build := macSystemVersion()
	if version != "" && build != "" {
		return version + " (" + build + ")"
	}
	return version
}

// macSystemVersion reads the same plist `sw_vers` reads, without a subprocess.
func macSystemVersion() (version, build string) {
	data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return "", ""
	}
	kv := parsePlistStringDict(data)
	return kv["ProductVersion"], kv["ProductBuildVersion"]
}

// parsePlistStringDict does a minimal parse of a flat <dict> of <key>/<string>
// (or <integer>) pairs — exactly the shape of Apple's SystemVersion.plist.
func parsePlistStringDict(data []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	lastKey := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var v string
			if dec.DecodeElement(&v, &start) == nil {
				lastKey = v
			}
		case "string", "integer":
			if lastKey == "" {
				continue
			}
			var v string
			if dec.DecodeElement(&v, &start) == nil {
				out[lastKey] = v
			}
			lastKey = ""
		}
	}
	return out
}

func platformKernelInfo() map[string]any {
	release, _ := syscall.Sysctl("kern.osrelease")
	version, _ := syscall.Sysctl("kern.version")
	nodename, _ := os.Hostname()
	return map[string]any{
		"sysname":  "Darwin",
		"nodename": nodename,
		"release":  release,
		"version":  version,
		"machine":  runtime.GOARCH,
	}
}

func platformInfo() map[string]any {
	out := map[string]any{"os": "darwin"}
	version, build := macSystemVersion()
	swVers := map[string]string{"ProductName": "macOS"}
	if version != "" {
		swVers["ProductVersion"] = version
	}
	if build != "" {
		swVers["BuildVersion"] = build
	}
	out["swVers"] = swVers
	if model, err := syscall.Sysctl("hw.model"); err == nil {
		out["model"] = model
	}
	return out
}

func platformCPUInfo() map[string]any {
	out := map[string]any{"logical": runtime.NumCPU()}
	if brand, err := syscall.Sysctl("machdep.cpu.brand_string"); err == nil {
		out["model"] = brand
	}
	if physical, err := syscall.SysctlUint32("hw.physicalcpu"); err == nil {
		out["physical"] = physical
	}
	if logical, err := syscall.SysctlUint32("hw.logicalcpu"); err == nil {
		out["logicalFromSysctl"] = logical
	}
	return out
}

// platformUptimeInfo: kern.boottime is a binary struct timeval, not decodable
// via the stdlib's string-oriented Sysctl helper. Reported as unsupported
// rather than risking an unverified raw-struct decode.
func platformUptimeInfo() map[string]any {
	return map[string]any{
		"supported": false,
		"note":      "boot time requires decoding the raw kern.boottime sysctl struct (unavailable without cgo)",
	}
}

// platformMemoryInfo: hw.memsize is a 64-bit sysctl. The stdlib syscall
// package only exposes Sysctl (NUL-terminated C string — wrong for a binary
// uint64 and would silently truncate/garble the value) and SysctlUint32
// (would truncate to 32 bits on any machine with >4GB RAM, i.e. virtually
// all of them). There is no SysctlUint64 in the stdlib (only in
// golang.org/x/sys, an external dependency). Reported as unsupported rather
// than a silently wrong value.
func platformMemoryInfo() map[string]any {
	return map[string]any{
		"supported": false,
		"note":      "total physical memory (hw.memsize) is a 64-bit sysctl; the stdlib syscall package has no safe accessor for it without cgo or golang.org/x/sys",
		"vmStat": map[string]any{
			"supported": false,
			"note":      "page-in/out counters require the Mach host_statistics64 API (unavailable without cgo)",
		},
	}
}

// mntNowait is BSD's MNT_NOWAIT flag (sys/mount.h) — use cached filesystem
// statistics instead of forcing each mount to refresh synchronously. Not
// exported by the stdlib syscall package, so the literal value is used.
const mntNowait = 2

// platformDiskInfo enumerates mounted filesystems via getfsstat(2) (stdlib
// syscall.Getfsstat), which returns usage stats directly — no `df`.
func platformDiskInfo() []map[string]any {
	n, err := syscall.Getfsstat(nil, mntNowait)
	if err != nil || n <= 0 {
		return []map[string]any{}
	}
	buf := make([]syscall.Statfs_t, n)
	n, err = syscall.Getfsstat(buf, mntNowait)
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, n)
	for _, st := range buf[:n] {
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		if total == 0 {
			continue
		}
		free := st.Bfree * bsize
		avail := st.Bavail * bsize
		used := total - free
		out = append(out, map[string]any{
			"filesystem": charsToString(st.Mntfromname[:]),
			"size":       total,
			"used":       used,
			"available":  avail,
			"usePercent": fmt.Sprintf("%d%%", percentOf(used, total)),
			"mount":      charsToString(st.Mntonname[:]),
		})
	}
	return out
}

// platformProcList: see the package-level note above — kern.proc.all decoding
// is intentionally not attempted. Reported as unsupported (empty, not an
// error) so callers can distinguish "no processes" from "not implemented".
func platformProcList() []map[string]any { return []map[string]any{} }

func platformProcListSupported() bool { return false }
