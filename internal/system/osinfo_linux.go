//go:build linux

// Linux implementations of the platform-specific OS-info helpers.
// Every value here comes from stdlib file reads (/proc, /etc/os-release) or
// the stdlib syscall package (Uname, Statfs) — no subprocess is spawned.
package system

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func osVersionInfo() string {
	kv := readKeyValueFile("/etc/os-release", "=")
	if pretty := strings.Trim(kv["PRETTY_NAME"], `"`); pretty != "" {
		return pretty
	}
	if name := strings.Trim(kv["NAME"], `"`); name != "" {
		if ver := strings.Trim(kv["VERSION_ID"], `"`); ver != "" {
			return name + " " + ver
		}
		return name
	}
	return ""
}

// platformKernelInfo reads the kernel identity via the uname(2) syscall
// (stdlib syscall.Uname) instead of shelling out to `uname -a`.
func platformKernelInfo() map[string]any {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return map[string]any{}
	}
	return map[string]any{
		"sysname":  charsToString(uts.Sysname[:]),
		"nodename": charsToString(uts.Nodename[:]),
		"release":  charsToString(uts.Release[:]),
		"version":  charsToString(uts.Version[:]),
		"machine":  charsToString(uts.Machine[:]),
	}
}

func platformInfo() map[string]any {
	out := map[string]any{"os": "linux"}
	for k, v := range readKeyValueFile("/etc/os-release", "=") {
		out[strings.ToLower(k)] = strings.Trim(v, `"`)
	}
	return out
}

func platformCPUInfo() map[string]any {
	out := map[string]any{"logical": runtime.NumCPU()}
	cpu := readKeyValueFile("/proc/cpuinfo", ":")
	if v := cpu["model name"]; v != "" {
		out["model"] = v
	}
	if v := cpu["cpu cores"]; v != "" {
		out["coresPerSocket"] = v
	}
	return out
}

func platformUptimeInfo() map[string]any {
	out := map[string]any{}
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return out
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return out
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return out
	}
	out["seconds"] = seconds
	out["human"] = durationString(seconds)
	return out
}

func platformMemoryInfo() map[string]any {
	out := map[string]any{}
	for k, v := range readKeyValueFile("/proc/meminfo", ":") {
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			out[k] = v
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			n *= 1024
		}
		out[k] = n
	}
	return out
}

// platformDiskInfo enumerates mounted filesystems from /proc/mounts and
// reads usage via statfs(2) (stdlib syscall.Statfs) — no `df` subprocess.
func platformDiskInfo() []map[string]any {
	out := []map[string]any{}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return out
	}
	defer f.Close()

	skipFstype := map[string]bool{
		"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
		"devpts": true, "securityfs": true, "pstore": true, "bpf": true,
		"tracefs": true, "debugfs": true, "mqueue": true, "hugetlbfs": true,
		"autofs": true, "binfmt_misc": true, "configfs": true, "fusectl": true,
		"rpc_pipefs": true, "devtmpfs": true, "nsfs": true,
	}

	sc := bufio.NewScanner(f)
	seen := map[string]bool{}
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := unescapeMountField(fields[0]), unescapeMountField(fields[1]), fields[2]
		if skipFstype[fstype] || seen[mount] {
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		bsize := uint64(st.Bsize)
		total := st.Blocks * bsize
		if total == 0 {
			continue
		}
		free := st.Bfree * bsize
		avail := st.Bavail * bsize
		used := total - free
		seen[mount] = true
		out = append(out, map[string]any{
			"filesystem": device,
			"size":       total,
			"used":       used,
			"available":  avail,
			"usePercent": fmt.Sprintf("%d%%", percentOf(used, total)),
			"mount":      mount,
		})
	}
	return out
}

// platformProcList walks /proc/<pid> directly (no `ps` subprocess).
func platformProcList() []map[string]any {
	out := []map[string]any{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			continue
		}
		name := ""
		ppid := 0
		for _, line := range strings.Split(string(status), "\n") {
			if v, ok := strings.CutPrefix(line, "Name:"); ok {
				name = strings.TrimSpace(v)
			} else if v, ok := strings.CutPrefix(line, "PPid:"); ok {
				ppid, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		username := ""
		if info, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				if u, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil {
					username = u.Username
				}
			}
		}
		out = append(out, map[string]any{
			"pid": pid, "ppid": ppid, "user": username, "name": name,
		})
	}
	return out
}

func platformProcListSupported() bool { return true }
