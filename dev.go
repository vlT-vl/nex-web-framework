//go:build ignore

// Development runner for nex-web.
// Usage: go run dev.go
//
// Starts Vite HMR then runs the Go server with -tags dev (daemon disabled).
// The Go server listens on 127.0.0.1:34116; Vite proxies /api and /nex.js to it.
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"
)

const (
	frontendDir = "frontend"
	mainPkg     = "."
	viteHost    = "127.0.0.1"
	vitePort    = "5181"
	viteAddr    = viteHost + ":" + vitePort
	viteURL     = "http://" + viteAddr
	apiPort     = "34116"
	apiAddr     = viteHost + ":" + apiPort
)

func main() {
	if err := doDev(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func doDev() error {
	if err := ensureGoModules(); err != nil {
		return err
	}
	if _, err := os.Stat(frontendDir + "/node_modules"); os.IsNotExist(err) {
		if err := runCmd(frontendDir, nil, "npm", "install"); err != nil {
			return err
		}
	}
	if err := stopPreviousOnPort(apiPort, apiAddr, "nex-web backend"); err != nil {
		return err
	}

	// Start the Go backend *before* Vite. `go run` has to compile the package
	// first, which takes a moment — if Vite (and the browser) come up first,
	// every /api/* call proxies straight into ECONNREFUSED (surfaced to the
	// browser as HTTP 502) until the backend finishes starting. Waiting for
	// the backend to actually accept connections here removes that race.
	appCmd := exec.Command("go", "run", "-tags", "dev", mainPkg)
	appCmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"NEXWEB_ADDR="+apiAddr,
		"NEXWEB_PUBLIC_ADDR=", // disable public proxy in dev mode (Vite handles it)
	)
	appCmd.Stdout = os.Stdout
	appCmd.Stderr = os.Stderr
	if err := appCmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- appCmd.Wait() }()

	if err := waitForAddr(apiAddr, 20*time.Second, done); err != nil {
		_ = appCmd.Process.Kill()
		return err
	}

	stopVite, err := ensureVite()
	if err != nil {
		_ = appCmd.Process.Kill()
		<-done
		return err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		// Ctrl+C or kill from console: bring down backend then Vite
		_ = appCmd.Process.Kill()
		<-done
	case <-done:
		// backend exited on its own (e.g. app.quit())
	}

	stopVite()
	return nil
}

// waitForAddr polls addr until it accepts TCP connections, the backend
// process exits early (compile error, panic, port conflict — reported via
// done), or timeout elapses.
func waitForAddr(addr string, timeout time.Duration, done <-chan error) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("backend exited before it became ready: %w", err)
			}
			return fmt.Errorf("backend exited before it became ready")
		default:
		}
		if portOpen(addr) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("backend did not become ready at %s within %s", addr, timeout)
}

func ensureGoModules() error {
	return runCmd(".", nil, "go", "mod", "tidy")
}

func ensureVite() (func(), error) {
	if err := stopPreviousOnPort(vitePort, viteAddr, "vite server"); err != nil {
		return nil, err
	}

	vite := exec.Command("npm", "run", "dev")
	vite.Dir = frontendDir
	vite.Env = append(os.Environ(),
		"NEX_VITE_HOST="+viteHost,
		"NEX_VITE_PORT="+vitePort,
	)
	vite.Stdout = os.Stdout
	vite.Stderr = os.Stderr
	if err := vite.Start(); err != nil {
		return nil, fmt.Errorf("starting vite: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- vite.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err != nil {
				return nil, fmt.Errorf("vite exited early: %w", err)
			}
			return nil, fmt.Errorf("vite exited early")
		default:
			if portOpen(viteAddr) {
				return func() {
					_ = vite.Process.Kill()
					<-done
				}, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	_ = vite.Process.Kill()
	<-done
	return nil, fmt.Errorf("vite did not become ready at %s", viteURL)
}

func stopPreviousOnPort(port, addr, label string) error {
	pids, err := pidsOnPort(port)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		return nil
	}
	fmt.Printf("-> stopping previous %s on %s (%s)\n", label, addr, strings.Join(pids, ", "))
	for _, pid := range pids {
		if err := killPID(pid); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !portOpen(addr) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("port %s is still in use after stopping previous %s", port, label)
}

func pidsOnPort(port string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return windowsPIDsOnPort(port)
	}
	out, err := exec.Command("lsof", "-ti", "tcp:"+port).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) == 0 && len(out) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("finding process on port %s: %w", port, err)
	}
	return splitLines(string(out)), nil
}

func windowsPIDsOnPort(port string) ([]string, error) {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return nil, fmt.Errorf("finding process on port %s: %w", port, err)
	}
	seen := map[string]bool{}
	var pids []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		local := fields[1]
		state := fields[3]
		pid := fields[4]
		if strings.HasSuffix(local, ":"+port) && state == "LISTENING" && !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func killPID(pid string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("taskkill", "/PID", pid, "/T", "/F")
	} else {
		cmd = exec.Command("kill", pid)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stopping process %s: %w", pid, err)
	}
	return nil
}

func portOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func runCmd(dir string, env []string, name string, args ...string) error {
	fmt.Printf("-> (%s) %s %s\n", dir, name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	c.Dir = dir
	if env != nil {
		c.Env = env
	}
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
