// Package system registers web-compatible sys.* RPC handlers.
// Desktop-only handlers (native dialogs, OS notifications, window control)
// are intentionally absent from this web variant.
package system

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"nex-web/internal/core"
)

var (
	watchMu       sync.Mutex
	watchRegistry = map[string]context.CancelFunc{}
)

var (
	kvOnce  sync.Once
	kvMu    sync.Mutex
	kvStore map[string]json.RawMessage
)

const kvFile = ".nex-kv.json"

// ---- Security globals -------------------------------------------------------

// sysfsRoot is the optional filesystem jail root set by Register.
// Empty means unrestricted (default when FSRoot is not configured).
var sysfsRoot string

// checkPath validates p against the configured FSRoot jail.
// When FSRoot is empty the path is returned unchanged.
// Symlinks are resolved where possible to prevent directory-traversal escapes.
func checkPath(p string) (string, error) {
	if sysfsRoot == "" {
		return p, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", core.Errorf("fs", "invalid path")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", core.Errorf("fs", "cannot resolve path")
		}
		// File doesn't exist yet: resolve symlinks on the parent directory so a
		// symlink inside FSRoot pointing outside cannot be used as a write target.
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(abs))
		if parentErr == nil {
			resolved = filepath.Join(resolvedParent, filepath.Base(abs))
		} else {
			resolved = abs
		}
	}
	root := filepath.Clean(sysfsRoot)
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", core.Errorf("fs", "path outside allowed root")
	}
	return abs, nil
}

// checkGlobBase validates that the concrete directory portion of a glob pattern
// is within the FSRoot jail.
func checkGlobBase(pattern string) error {
	if sysfsRoot == "" {
		return nil
	}
	base := pattern
	for i, c := range pattern {
		if c == '*' || c == '?' || c == '[' {
			base = pattern[:i]
			break
		}
	}
	_, err := checkPath(filepath.Dir(base))
	return err
}

func Register(reg core.Registrar, fsRoot string) {
	if fsRoot != "" {
		root, err := filepath.Abs(fsRoot)
		if err == nil {
			if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
				root = resolved
			}
			sysfsRoot = filepath.Clean(root)
		} else {
			sysfsRoot = filepath.Clean(fsRoot)
		}
	}
	// OS info
	reg.Register("sys.os.info", osInfo)
	reg.Register("sys.os.host", osHost)
	reg.Register("sys.os.user", osUser)
	reg.Register("sys.os.runtime", osRuntime)
	reg.Register("sys.os.process", osProcess)
	reg.Register("sys.os.network", osNetwork)
	reg.Register("sys.os.disks", osDisks)
	reg.Register("sys.os.memory", osMemory)
	reg.Register("sys.os.time", osTime)
	reg.Register("sys.env.paths", envPaths)

	// Logging (frontend → backend log output)
	reg.Register("sys.log.print", logPrint)
	reg.Register("sys.log.trace", logLevel("trace"))
	reg.Register("sys.log.debug", logLevel("debug"))
	reg.Register("sys.log.info", logLevel("info"))
	reg.Register("sys.log.warning", logLevel("warning"))
	reg.Register("sys.log.error", logLevel("error"))

	// Shell
	reg.Register("sys.shell.exec", shellExec)
	reg.Register("sys.shell.run", shellExec)
	reg.Register("sys.shell.start", shellStart)

	// HTTP proxy
	reg.Register("sys.http.fetch", httpFetch)

	// Env
	reg.Register("sys.env.get", envGet)

	// Filesystem
	reg.Register("sys.fs.read", fsRead)
	reg.Register("sys.fs.write", fsWrite)
	reg.Register("sys.fs.list", fsList)
	reg.Register("sys.fs.exists", fsExists)
	reg.Register("sys.fs.stat", fsStat)
	reg.Register("sys.fs.mkdir", fsMkdir)
	reg.Register("sys.fs.remove", fsRemove)
	reg.Register("sys.fs.rename", fsRename)
	reg.Register("sys.fs.copy", fsCopy)
	reg.Register("sys.fs.watch", fsWatch)
	reg.Register("sys.fs.unwatch", fsUnwatch)

	// KV store (persistent JSON file)
	reg.Register("sys.kv.get", kvGet)
	reg.Register("sys.kv.set", kvSet)
	reg.Register("sys.kv.delete", kvDelete)
	reg.Register("sys.kv.list", kvList)

	// Network
	reg.Register("sys.net.resolve", netResolve)
	reg.Register("sys.net.port", netPort)

	// App
	reg.Register("sys.app.quit", appQuit)
	reg.Register("sys.app.reload", appReload)

	// Filesystem extras
	reg.Register("sys.fs.glob", fsGlob)
	reg.Register("sys.fs.abs", fsAbs)
	reg.Register("sys.fs.temp", fsTemp)

	// Process list
	reg.Register("sys.proc.list", procList)

	// KV extras
	reg.Register("sys.kv.clear", kvClear)

	// Env list
	reg.Register("sys.env.list", envList)

	// Security/capability introspection
	reg.Register("sys.security.capabilities", securityCapabilities)
}

// ---- OS Info ----------------------------------------------------------------

func osInfo(_ *core.Context, _ json.RawMessage) (any, error) {
	host := hostInfo()
	paths := pathsInfo()
	return map[string]any{
		"host":        host,
		"user":        userInfo(),
		"runtime":     runtimeInfo(),
		"process":     processInfo(),
		"paths":       paths,
		"network":     networkInfo(),
		"disks":       platformDiskInfo(),
		"memory":      memoryInfo(),
		"time":        timeInfo(),
		"environment": environmentInfo(),
		// Compact fields kept for backward compatibility.
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"home":     paths["home"],
		"hostname": host["hostname"],
		"cpus":     runtime.NumCPU(),
	}, nil
}

func osHost(_ *core.Context, _ json.RawMessage) (any, error) {
	return hostInfo(), nil
}

func osUser(_ *core.Context, _ json.RawMessage) (any, error) {
	return userInfo(), nil
}

func osRuntime(_ *core.Context, _ json.RawMessage) (any, error) {
	return runtimeInfo(), nil
}

func osProcess(_ *core.Context, _ json.RawMessage) (any, error) {
	return processInfo(), nil
}

func osNetwork(_ *core.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"interfaces": networkInfo()}, nil
}

func osDisks(_ *core.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"volumes": platformDiskInfo()}, nil
}

func osMemory(_ *core.Context, _ json.RawMessage) (any, error) {
	return memoryInfo(), nil
}

func osTime(_ *core.Context, _ json.RawMessage) (any, error) {
	return timeInfo(), nil
}

func envPaths(_ *core.Context, _ json.RawMessage) (any, error) {
	return pathsInfo(), nil
}

// ---- Info helpers -----------------------------------------------------------

func hostInfo() map[string]any {
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	return map[string]any{
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"compiler":    runtime.Compiler,
		"goVersion":   runtime.Version(),
		"hostname":    host,
		"home":        home,
		"cpus":        runtime.NumCPU(),
		"osVersion":   osVersionInfo(),
		"kernel":      platformKernelInfo(),
		"platform":    platformInfo(),
		"cpu":         platformCPUInfo(),
		"uptime":      platformUptimeInfo(),
		"machineName": host,
	}
}

func pathsInfo() map[string]any {
	home, _ := os.UserHomeDir()
	cfg, _ := os.UserConfigDir()
	cache, _ := os.UserCacheDir()
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	return map[string]any{
		"home": home, "config": cfg, "cache": cache,
		"temp": os.TempDir(), "exe": exe, "cwd": cwd,
	}
}

func userInfo() map[string]any {
	out := map[string]any{}
	u, err := user.Current()
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["uid"] = u.Uid
	out["gid"] = u.Gid
	out["username"] = u.Username
	out["name"] = u.Name
	out["homeDir"] = u.HomeDir
	if gids, err := u.GroupIds(); err == nil {
		out["groupIds"] = gids
	}
	return out
}

func runtimeInfo() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	build := map[string]any{}
	if info, ok := debug.ReadBuildInfo(); ok {
		build["goVersion"] = info.GoVersion
		build["path"] = info.Path
		build["main"] = map[string]any{
			"path":    info.Main.Path,
			"version": info.Main.Version,
			"sum":     info.Main.Sum,
		}
		settings := map[string]string{}
		for _, s := range info.Settings {
			settings[s.Key] = s.Value
		}
		build["settings"] = settings
	}

	return map[string]any{
		"goos":          runtime.GOOS,
		"goarch":        runtime.GOARCH,
		"compiler":      runtime.Compiler,
		"goVersion":     runtime.Version(),
		"numCPU":        runtime.NumCPU(),
		"gomaxprocs":    runtime.GOMAXPROCS(0),
		"goroutines":    runtime.NumGoroutine(),
		"cgoCalls":      runtime.NumCgoCall(),
		"mem":           memStatsMap(m),
		"build":         build,
		"defaultGOROOT": runtime.GOROOT(),
	}
}

func processInfo() map[string]any {
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	return map[string]any{
		"pid":         os.Getpid(),
		"ppid":        os.Getppid(),
		"executable":  exe,
		"cwd":         cwd,
		"args":        os.Args,
		"pageSize":    os.Getpagesize(),
		"tempDir":     os.TempDir(),
		"environment": environmentInfo(),
	}
}

func environmentInfo() map[string]any {
	public := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, "VITE_") {
			public[k] = v
		}
	}
	return map[string]any{
		"count":        len(os.Environ()),
		"publicPrefix": "VITE_",
		"public":       public,
	}
}

func networkInfo() []map[string]any {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []map[string]any{{"error": err.Error()}}
	}
	out := make([]map[string]any, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		addrList := []string{}
		if err == nil {
			for _, addr := range addrs {
				addrList = append(addrList, addr.String())
			}
		}
		out = append(out, map[string]any{
			"name":         iface.Name,
			"index":        iface.Index,
			"mtu":          iface.MTU,
			"flags":        iface.Flags.String(),
			"hardwareAddr": iface.HardwareAddr.String(),
			"addrs":        addrList,
		})
	}
	return out
}

func memoryInfo() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]any{
		"go":     memStatsMap(m),
		"system": platformMemoryInfo(),
	}
}

func timeInfo() map[string]any {
	now := time.Now()
	name, offset := now.Zone()
	return map[string]any{
		"local":          now.Format(time.RFC3339Nano),
		"utc":            now.UTC().Format(time.RFC3339Nano),
		"unix":           now.Unix(),
		"unixNano":       now.UnixNano(),
		"timezone":       name,
		"timezoneOffset": offset,
	}
}

func memStatsMap(m runtime.MemStats) map[string]any {
	return map[string]any{
		"alloc":         m.Alloc,
		"totalAlloc":    m.TotalAlloc,
		"sys":           m.Sys,
		"lookups":       m.Lookups,
		"mallocs":       m.Mallocs,
		"frees":         m.Frees,
		"heapAlloc":     m.HeapAlloc,
		"heapSys":       m.HeapSys,
		"heapIdle":      m.HeapIdle,
		"heapInuse":     m.HeapInuse,
		"heapReleased":  m.HeapReleased,
		"heapObjects":   m.HeapObjects,
		"stackInuse":    m.StackInuse,
		"stackSys":      m.StackSys,
		"nextGC":        m.NextGC,
		"lastGC":        m.LastGC,
		"pauseTotalNs":  m.PauseTotalNs,
		"numGC":         m.NumGC,
		"numForcedGC":   m.NumForcedGC,
		"gcCPUFraction": m.GCCPUFraction,
	}
}

// ---- Utility helpers --------------------------------------------------------
//
// Platform-specific OS-info gathering (kernel, platform, CPU, uptime, memory,
// disks, process list) lives in osinfo_linux.go / osinfo_darwin.go /
// osinfo_windows.go — pure Go/syscall, no subprocess. Shared helpers used by
// those files (charsToString, percentOf, durationString, unescapeMountField)
// live in osinfo_common.go.

func readKeyValueFile(path, sep string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, sep)
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && out[k] == "" {
			out[k] = v
		}
	}
	return out
}

// ---- Logging ----------------------------------------------------------------

func logPrint(_ *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Message string `json:"message"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, core.Errorf("bad_request", "%v", err)
		}
	}
	log.Print(p.Message)
	return map[string]any{"ok": true}, nil
}

func logLevel(level string) core.HandlerFunc {
	return func(_ *core.Context, params json.RawMessage) (any, error) {
		var p struct {
			Message string `json:"message"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, core.Errorf("bad_request", "%v", err)
			}
		}
		log.Printf("[%s] %s", level, p.Message)
		return map[string]any{"ok": true, "level": level}, nil
	}
}

// ---- Shell ------------------------------------------------------------------

func shellExec(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Command   string            `json:"command"`
		Cwd       string            `json:"cwd"`
		Env       map[string]string `json:"env"`
		TimeoutMS int               `json:"timeoutMs"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Command == "" {
		return nil, core.Errorf("bad_request", "command is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "shell", Operation: "exec", Command: p.Command, Params: params,
	}); err != nil {
		return nil, err
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}

	ctx, cancel := context.WithTimeout(c.Ctx, timeout)
	defer cancel()

	name, args, shellName := nativeShellCommand(p.Command)
	cmd := exec.CommandContext(ctx, name, args...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	if len(p.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range p.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)
	timedOut := ctx.Err() == context.DeadlineExceeded
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return map[string]any{
		"ok":         err == nil,
		"command":    p.Command,
		"shell":      shellName,
		"exitCode":   exitCode,
		"stdout":     stdout.String(),
		"stderr":     stderr.String(),
		"timedOut":   timedOut,
		"durationMs": duration.Milliseconds(),
	}, nil
}

func shellStart(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Command string            `json:"command"`
		Cwd     string            `json:"cwd"`
		Env     map[string]string `json:"env"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Command == "" {
		return nil, core.Errorf("bad_request", "command is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "shell", Operation: "start", Command: p.Command, Params: params,
	}); err != nil {
		return nil, err
	}
	name, args, shellName := nativeShellCommand(p.Command)
	cmd := exec.Command(name, args...)
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	if len(p.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range p.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, core.Errorf("shell", "%v", err)
	}
	pid := cmd.Process.Pid
	// Reap the detached child in the background instead of Release(), which
	// leaves an unreaped zombie on Unix once the process exits.
	go func() { _ = cmd.Wait() }()
	return map[string]any{
		"ok":      true,
		"command": p.Command,
		"shell":   shellName,
		"pid":     pid,
	}, nil
}

func nativeShellCommand(command string) (string, []string, string) {
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		return shell, []string{"/C", command}, "cmd"
	}
	return "/bin/sh", []string{"-c", command}, "sh"
}

// ---- Filesystem -------------------------------------------------------------

func fsRead(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "read", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	enc := p.Encoding
	if enc == "" {
		enc = "utf8"
	}
	out := string(data)
	if enc == "base64" {
		out = base64.StdEncoding.EncodeToString(data)
	}
	return map[string]any{"data": out, "encoding": enc}, nil
}

func fsWrite(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path     string `json:"path"`
		Data     string `json:"data"`
		Encoding string `json:"encoding"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "write", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	var raw []byte
	if p.Encoding == "base64" {
		b, err := base64.StdEncoding.DecodeString(p.Data)
		if err != nil {
			return nil, core.Errorf("bad_request", "invalid base64: %v", err)
		}
		raw = b
	} else {
		raw = []byte(p.Data)
	}
	if err := os.WriteFile(safePath, raw, 0o644); err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{"ok": true, "bytes": len(raw)}, nil
}

func fsList(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "list", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(safePath)
	if err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
	}
	return map[string]any{"entries": out}, nil
}

func fsExists(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "exists", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	info, err := os.Stat(safePath)
	if os.IsNotExist(err) {
		return map[string]any{"exists": false}, nil
	}
	if err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{"exists": true, "isDir": info.IsDir()}, nil
}

func fsStat(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "stat", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	info, err := os.Stat(safePath)
	if err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{
		"name":    info.Name(),
		"size":    info.Size(),
		"isDir":   info.IsDir(),
		"modTime": info.ModTime().Format(time.RFC3339),
	}, nil
}

func fsMkdir(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "mkdir", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(safePath, 0o755); err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{"ok": true}, nil
}

func fsRemove(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "remove", Path: safePath, Recursive: p.Recursive, Params: params,
	}); err != nil {
		return nil, err
	}
	if p.Recursive {
		err = os.RemoveAll(safePath)
	} else {
		err = os.Remove(safePath)
	}
	if err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{"ok": true}, nil
}

func fsRename(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	safeFrom, err := checkPath(p.From)
	if err != nil {
		return nil, err
	}
	safeTo, err := checkPath(p.To)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "rename", From: safeFrom, To: safeTo, Params: params,
	}); err != nil {
		return nil, err
	}
	if err := os.Rename(safeFrom, safeTo); err != nil {
		return nil, core.Errorf("io", "%v", err)
	}
	return map[string]any{"ok": true}, nil
}

// ---- HTTP fetch -------------------------------------------------------------

func httpFetch(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		URL        string            `json:"url"`
		Method     string            `json:"method"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
		TimeoutMS  int               `json:"timeoutMs"`
		Encoding   string            `json:"encoding"`   // "utf8" (default) | "base64"
		SkipVerify bool              `json:"skipVerify"` // skip TLS cert check (HTTPS only)
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.URL == "" {
		return nil, core.Errorf("bad_request", "url is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "http", Operation: "fetch", URL: p.URL, Params: params,
	}); err != nil {
		return nil, err
	}
	if p.Method == "" {
		p.Method = "GET"
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(c.Ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if p.Body != "" {
		bodyReader = strings.NewReader(p.Body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(p.Method), p.URL, bodyReader)
	if err != nil {
		return nil, core.Errorf("http", "invalid request: %v", err)
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	transport := http.DefaultTransport
	if p.SkipVerify {
		transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Re-run the same authorization path (SecurityPolicy + OnHTTPFetch)
		// against every redirect target, not just the original URL — a
		// remote server can otherwise redirect an allowed request to a
		// URL the configured policy would have denied (e.g. an internal
		// or metadata address) and bypass it silently.
		CheckRedirect: func(nextReq *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if err := c.Authorize(core.SecurityDecision{
				Category: "http", Operation: "fetch", URL: nextReq.URL.String(), Params: params,
			}); err != nil {
				return err
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, core.Errorf("http", "%v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64 MiB response cap
	if err != nil {
		return nil, core.Errorf("http", "reading response: %v", err)
	}

	respHeaders := map[string]string{}
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	body := string(raw)
	encoding := "utf8"
	if p.Encoding == "base64" {
		body = base64.StdEncoding.EncodeToString(raw)
		encoding = "base64"
	}

	finalURL := p.URL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return map[string]any{
		"ok":         resp.StatusCode >= 200 && resp.StatusCode < 300,
		"status":     resp.StatusCode,
		"statusText": resp.Status,
		"headers":    respHeaders,
		"body":       body,
		"encoding":   encoding,
		"url":        finalURL,
		"redirected": finalURL != p.URL,
	}, nil
}

// ---- Env get ----------------------------------------------------------------

func envGet(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Key == "" {
		return nil, core.Errorf("bad_request", "key is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "env", Operation: "get", Key: p.Key, Params: params,
	}); err != nil {
		return nil, err
	}
	v, found := os.LookupEnv(p.Key)
	return map[string]any{"key": p.Key, "value": v, "found": found}, nil
}

// ---- FS watch ---------------------------------------------------------------

func fsWatch(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path       string `json:"path"`
		ID         string `json:"id"`
		IntervalMs int    `json:"intervalMs"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Path == "" {
		return nil, core.Errorf("bad_request", "path is required")
	}
	safePath, err := checkPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "watch", Path: safePath, Params: params,
	}); err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	interval := time.Duration(p.IntervalMs) * time.Millisecond
	if interval < 200*time.Millisecond {
		interval = time.Second
	}

	watchMu.Lock()
	if cancel, ok := watchRegistry[p.ID]; ok {
		cancel() // stop existing watcher with same ID
	}
	ctx, cancel := context.WithCancel(context.Background())
	watchRegistry[p.ID] = cancel
	watchMu.Unlock()

	host := c.Host
	go func() {
		defer func() {
			watchMu.Lock()
			delete(watchRegistry, p.ID)
			watchMu.Unlock()
		}()
		var lastMod time.Time
		var lastSize int64
		var existed bool
		if info, err := os.Stat(safePath); err == nil {
			lastMod = info.ModTime()
			lastSize = info.Size()
			existed = true
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(safePath)
				if err != nil {
					if existed {
						existed = false
						host.Emit("fs.changed", map[string]any{
							"id": p.ID, "path": safePath, "event": "deleted",
						})
					}
					continue
				}
				if !existed {
					existed = true
					lastMod = info.ModTime()
					lastSize = info.Size()
					host.Emit("fs.changed", map[string]any{
						"id": p.ID, "path": safePath, "event": "created",
						"size": info.Size(), "modTime": info.ModTime().Format(time.RFC3339),
					})
					continue
				}
				if info.ModTime() != lastMod || info.Size() != lastSize {
					lastMod = info.ModTime()
					lastSize = info.Size()
					host.Emit("fs.changed", map[string]any{
						"id": p.ID, "path": safePath, "event": "modified",
						"size": info.Size(), "modTime": info.ModTime().Format(time.RFC3339),
					})
				}
			}
		}
	}()

	return map[string]any{"ok": true, "id": p.ID, "path": safePath, "intervalMs": interval.Milliseconds()}, nil
}

func fsUnwatch(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ID == "" {
		return nil, core.Errorf("bad_request", "id is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "unwatch", Key: p.ID, Params: params,
	}); err != nil {
		return nil, err
	}
	watchMu.Lock()
	cancel, ok := watchRegistry[p.ID]
	if ok {
		cancel()
		delete(watchRegistry, p.ID)
	}
	watchMu.Unlock()
	return map[string]any{"ok": true, "stopped": ok, "id": p.ID}, nil
}

// ---- FS copy ----------------------------------------------------------------

func fsCopy(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Src       string `json:"src"`
		Dst       string `json:"dst"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Src == "" || p.Dst == "" {
		return nil, core.Errorf("bad_request", "src and dst are required")
	}
	safeSrc, err := checkPath(p.Src)
	if err != nil {
		return nil, err
	}
	safeDst, err := checkPath(p.Dst)
	if err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "copy", From: safeSrc, To: safeDst, Params: params,
	}); err != nil {
		return nil, err
	}
	info, err := os.Stat(safeSrc)
	if err != nil {
		return nil, core.Errorf("not_found", "%v", err)
	}
	if info.IsDir() {
		err = copyDir(safeSrc, safeDst, p.Overwrite)
	} else {
		err = copyFile(safeSrc, safeDst, p.Overwrite)
	}
	if err != nil {
		return nil, core.Errorf("fs", "%v", err)
	}
	return map[string]any{"ok": true, "src": safeSrc, "dst": safeDst}, nil
}

func copyFile(src, dst string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("destination already exists: %s", dst)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string, overwrite bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if _, err := checkPath(path); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, overwrite)
	})
}

// ---- KV store ---------------------------------------------------------------

func kvLoad() {
	kvOnce.Do(func() {
		kvStore = map[string]json.RawMessage{}
		data, err := os.ReadFile(kvFile)
		if err != nil {
			return
		}
		_ = json.Unmarshal(data, &kvStore)
	})
}

func kvFlush() error {
	data, err := json.MarshalIndent(kvStore, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(kvFile, data, 0644)
}

func kvGet(_ *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Key == "" {
		return nil, core.Errorf("bad_request", "key is required")
	}
	kvLoad()
	kvMu.Lock()
	raw, found := kvStore[p.Key]
	kvMu.Unlock()
	var value any
	if found {
		_ = json.Unmarshal(raw, &value)
	}
	return map[string]any{"key": p.Key, "value": value, "found": found}, nil
}

func kvSet(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Key == "" {
		return nil, core.Errorf("bad_request", "key is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "kv", Operation: "set", Key: p.Key, Params: params,
	}); err != nil {
		return nil, err
	}
	kvLoad()
	kvMu.Lock()
	kvStore[p.Key] = p.Value
	err := kvFlush()
	kvMu.Unlock()
	if err != nil {
		return nil, core.Errorf("kv", "flush: %v", err)
	}
	return map[string]any{"ok": true, "key": p.Key}, nil
}

func kvDelete(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Key == "" {
		return nil, core.Errorf("bad_request", "key is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "kv", Operation: "delete", Key: p.Key, Params: params,
	}); err != nil {
		return nil, err
	}
	kvLoad()
	kvMu.Lock()
	_, had := kvStore[p.Key]
	delete(kvStore, p.Key)
	err := kvFlush()
	kvMu.Unlock()
	if err != nil {
		return nil, core.Errorf("kv", "flush: %v", err)
	}
	return map[string]any{"ok": true, "key": p.Key, "deleted": had}, nil
}

func kvList(_ *core.Context, _ json.RawMessage) (any, error) {
	kvLoad()
	kvMu.Lock()
	entries := make([]map[string]any, 0, len(kvStore))
	for k, raw := range kvStore {
		var v any
		_ = json.Unmarshal(raw, &v)
		entries = append(entries, map[string]any{"key": k, "value": v})
	}
	kvMu.Unlock()
	return map[string]any{"entries": entries, "count": len(entries)}, nil
}

// ---- Network ----------------------------------------------------------------

func netResolve(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Host == "" {
		return nil, core.Errorf("bad_request", "host is required")
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "network", Operation: "resolve", Host: p.Host, Params: params,
	}); err != nil {
		return nil, err
	}
	addrs, err := net.LookupHost(p.Host)
	if err != nil {
		return nil, core.Errorf("net", "%v", err)
	}
	cname, _ := net.LookupCNAME(p.Host)
	return map[string]any{
		"host":      p.Host,
		"addresses": addrs,
		"cname":     strings.TrimSuffix(cname, "."),
	}, nil
}

// ---- App --------------------------------------------------------------------

func appQuit(c *core.Context, _ json.RawMessage) (any, error) {
	if err := c.Authorize(core.SecurityDecision{Category: "app", Operation: "quit"}); err != nil {
		return nil, err
	}
	c.Quit()
	return map[string]any{"ok": true}, nil
}

func appReload(c *core.Context, _ json.RawMessage) (any, error) {
	if err := c.Authorize(core.SecurityDecision{Category: "app", Operation: "reload"}); err != nil {
		return nil, err
	}
	c.Host.Emit("reload", nil)
	return map[string]any{"ok": true}, nil
}

// ---- Filesystem extras -------------------------------------------------------

func fsGlob(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Pattern string `json:"pattern"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Pattern == "" {
		return nil, core.Errorf("bad_request", "pattern is required")
	}
	if err := checkGlobBase(p.Pattern); err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "glob", Path: p.Pattern, Params: params,
	}); err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(p.Pattern)
	if err != nil {
		return nil, core.Errorf("fs", "invalid pattern: %v", err)
	}
	if matches == nil {
		matches = []string{}
	}
	return map[string]any{"matches": matches, "count": len(matches)}, nil
}

func fsAbs(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Path == "" {
		return nil, core.Errorf("bad_request", "path is required")
	}
	abs, err := filepath.Abs(p.Path)
	if err != nil {
		return nil, core.Errorf("fs", "%v", err)
	}
	if _, err := checkPath(abs); err != nil {
		return nil, err
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "abs", Path: abs, Params: params,
	}); err != nil {
		return nil, err
	}
	return map[string]any{"path": abs}, nil
}

func fsTemp(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Dir    string `json:"dir"`
		Prefix string `json:"prefix"`
		IsDir  bool   `json:"isDir"`
	}
	if len(params) > 0 {
		_ = c.Bind(params, &p)
	}
	// Validate explicitly specified dir; empty dir uses os.TempDir() (always permitted).
	if p.Dir != "" {
		if _, err := checkPath(p.Dir); err != nil {
			return nil, err
		}
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "filesystem", Operation: "temp", Path: p.Dir, Params: params,
	}); err != nil {
		return nil, err
	}
	if p.IsDir {
		dir, err := os.MkdirTemp(p.Dir, p.Prefix)
		if err != nil {
			return nil, core.Errorf("fs", "%v", err)
		}
		return map[string]any{"path": dir, "isDir": true}, nil
	}
	f, err := os.CreateTemp(p.Dir, p.Prefix)
	if err != nil {
		return nil, core.Errorf("fs", "%v", err)
	}
	_ = f.Close()
	return map[string]any{"path": f.Name(), "isDir": false}, nil
}

// ---- Process list -----------------------------------------------------------

// procList delegates to the per-platform pure-Go implementation
// (platformProcList in osinfo_linux.go / osinfo_darwin.go / osinfo_windows.go).
// "supported" is false on platforms where enumerating processes without a
// subprocess isn't safely implementable (see osinfo_darwin.go).
func procList(c *core.Context, _ json.RawMessage) (any, error) {
	if err := c.Authorize(core.SecurityDecision{Category: "process", Operation: "list"}); err != nil {
		return nil, err
	}
	procs := platformProcList()
	return map[string]any{
		"processes": procs,
		"count":     len(procs),
		"supported": platformProcListSupported(),
	}, nil
}

// ---- Network port check -----------------------------------------------------

func netPort(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		TimeoutMs int    `json:"timeoutMs"`
	}
	if err := c.Bind(params, &p); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if p.Port <= 0 || p.Port > 65535 {
		return nil, core.Errorf("bad_request", "port must be 1-65535")
	}
	if p.Host == "" {
		p.Host = "localhost"
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "network", Operation: "port", Host: p.Host, Port: p.Port, Params: params,
	}); err != nil {
		return nil, err
	}
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	addr := fmt.Sprintf("%s:%d", p.Host, p.Port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return map[string]any{"open": false, "host": p.Host, "port": p.Port, "addr": addr}, nil
	}
	_ = conn.Close()
	return map[string]any{"open": true, "host": p.Host, "port": p.Port, "addr": addr}, nil
}

// ---- KV clear ---------------------------------------------------------------

func kvClear(c *core.Context, _ json.RawMessage) (any, error) {
	if err := c.Authorize(core.SecurityDecision{Category: "kv", Operation: "clear"}); err != nil {
		return nil, err
	}
	kvLoad()
	kvMu.Lock()
	count := len(kvStore)
	kvStore = map[string]json.RawMessage{}
	err := kvFlush()
	kvMu.Unlock()
	if err != nil {
		return nil, core.Errorf("kv", "flush: %v", err)
	}
	return map[string]any{"ok": true, "cleared": count}, nil
}

// ---- Env list ---------------------------------------------------------------

func envList(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Prefix string `json:"prefix"`
	}
	if len(params) > 0 {
		_ = c.Bind(params, &p)
	}
	if err := c.Authorize(core.SecurityDecision{
		Category: "env", Operation: "list", Key: p.Prefix, Params: params,
	}); err != nil {
		return nil, err
	}
	environ := os.Environ()
	out := make([]map[string]string, 0, len(environ))
	for _, entry := range environ {
		k, v, _ := strings.Cut(entry, "=")
		if p.Prefix == "" || strings.HasPrefix(k, p.Prefix) {
			out = append(out, map[string]string{"key": k, "value": v})
		}
	}
	return map[string]any{"vars": out, "count": len(out)}, nil
}

// ---- Security capabilities --------------------------------------------------

func securityCapabilities(_ *core.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"policyHooks": map[string]bool{
			"available": true,
		},
		"filesystem": map[string]any{
			"available": true,
			"jail":      sysfsRoot != "",
			"root":      sysfsRoot,
		},
		"shell": map[string]any{
			"available": shellAvailable(),
			"shell":     defaultShellName(),
		},
		"http": map[string]any{
			"available":  true,
			"serverSide": true,
		},
		"env": map[string]any{
			"available": true,
		},
		"process": map[string]any{
			"available": processListAvailable(),
		},
		"network": map[string]any{
			"available": true,
		},
		"app": map[string]any{
			"quit":   true,
			"reload": true,
		},
	}, nil
}

func shellAvailable() bool {
	name, _, _ := nativeShellCommand("echo ok")
	_, err := exec.LookPath(name)
	return err == nil
}

func defaultShellName() string {
	_, _, shellName := nativeShellCommand("echo ok")
	return shellName
}

// processListAvailable reflects the real pure-Go platformProcList capability
// (see osinfo_*.go), not the presence of an external tool on PATH.
func processListAvailable() bool {
	return platformProcListSupported()
}
