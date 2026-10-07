//go:build darwin

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

func macSystemVersion() (version, build string) {
	data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return "", ""
	}
	kv := parsePlistStringDict(data)
	return kv["ProductVersion"], kv["ProductBuildVersion"]
}

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

func platformUptimeInfo() map[string]any {
	return map[string]any{
		"supported": false,
		"note":      "boot time requires decoding the raw kern.boottime sysctl struct (unavailable without cgo)",
	}
}

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

const mntNowait = 2

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

func platformProcList() []map[string]any { return []map[string]any{} }

func platformProcListSupported() bool { return false }
