package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nex-web/internal/core"
	"nex-web/internal/hostenv"
	"nex-web/internal/updater"
)

func newTestApp(t *testing.T, cfg Config) *App {
	t.Helper()
	t.Setenv(hostenv.StateDirEnv, t.TempDir())
	if cfg.Name == "" {
		cfg.Name = "test-app"
	}
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}
	if cfg.Build == "" {
		cfg.Build = "R010126"
	}
	if cfg.EnvFile == "" {
		cfg.EnvFile = filepath.Join(t.TempDir(), "none.env")
	}
	return New(cfg)
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestQuiesceFailsWhenPublicPortIsNeverReleased(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	go func() {
		for {
			conn, err := held.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	a := newTestApp(t, Config{PublicAddr: held.Addr().String()})
	a.updHost.quiesceTimeout = 300 * time.Millisecond
	if err := a.updHost.Quiesce(); err == nil {
		t.Fatal("expected Quiesce to fail while the public port is held")
	}
	held.Close()
	if err := a.updHost.Quiesce(); err != nil {
		t.Fatalf("expected Quiesce to succeed once the port is free: %v", err)
	}
}

func TestQuiesceWithoutPublicAddr(t *testing.T) {
	a := newTestApp(t, Config{})
	if err := a.updHost.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestRelaunchEnvDropsChildMarker(t *testing.T) {
	t.Setenv(hostenv.ChildEnv, "1")
	t.Setenv("NEX_TEST_KEEP", "yes")
	a := newTestApp(t, Config{})
	var kept bool
	for _, item := range a.updHost.RelaunchEnv() {
		if strings.HasPrefix(item, hostenv.ChildEnv+"=") {
			t.Fatalf("%s must not be relaunched", hostenv.ChildEnv)
		}
		if item == "NEX_TEST_KEEP=yes" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("other variables must be preserved")
	}
}

func callInstall(a *App, request *http.Request) error {
	c := &core.Context{Ctx: context.Background(), Host: a, Request: request, Method: "sys.updater.install"}
	_, err := a.updaterInstall(c, nil)
	return err
}

func rpcCode(err error) string {
	if rpc, ok := err.(*core.RPCError); ok {
		return rpc.Code
	}
	return ""
}

func TestInstallDeniedForRemoteClients(t *testing.T) {
	a := newTestApp(t, Config{})

	remote := httptest.NewRequest(http.MethodPost, "/api/rpc", nil)
	remote.RemoteAddr = "203.0.113.5:4000"
	if code := rpcCode(callInstall(a, remote)); code != "unauthorized" {
		t.Fatalf("remote address: code = %q", code)
	}

	proxied := httptest.NewRequest(http.MethodPost, "/api/rpc", nil)
	proxied.RemoteAddr = "127.0.0.1:4000"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.5")
	if code := rpcCode(callInstall(a, proxied)); code != "unauthorized" {
		t.Fatalf("forwarded remote client: code = %q", code)
	}

	local := httptest.NewRequest(http.MethodPost, "/api/rpc", nil)
	local.RemoteAddr = "127.0.0.1:4000"
	local.Header.Set("X-Forwarded-For", "::1")
	if code := rpcCode(callInstall(a, local)); code != "not_applicable" {
		t.Fatalf("loopback client must pass the guard, code = %q", code)
	}

	a.cfg.UpdateAllowRemoteInstall = true
	if code := rpcCode(callInstall(a, remote)); code != "not_applicable" {
		t.Fatalf("remote install allowed by config must pass the guard, code = %q", code)
	}
}

func TestInstallRespectsPolicyHook(t *testing.T) {
	a := newTestApp(t, Config{OnAppControl: func(_ *core.Context, d core.SecurityDecision) error {
		if d.Operation == "update.install" {
			return core.Errorf("denied", "no")
		}
		return nil
	}})
	local := httptest.NewRequest(http.MethodPost, "/api/rpc", nil)
	local.RemoteAddr = "127.0.0.1:4000"
	if code := rpcCode(callInstall(a, local)); code != "unauthorized" {
		t.Fatalf("code = %q", code)
	}
}

func TestInstallWithoutGrantIsDenied(t *testing.T) {
	a := newTestApp(t, Config{})
	if err := a.updHost.Authorize("install"); err == nil {
		t.Fatal("install without a granted request must be denied")
	}
	if err := a.updHost.Authorize("check"); err != nil {
		t.Fatalf("internal check must be allowed: %v", err)
	}
}

func TestCheckIgnoresURLParameter(t *testing.T) {
	t.Setenv("NEXWEB_UPDATE_URL", "")
	a := newTestApp(t, Config{})
	c := &core.Context{Ctx: context.Background(), Host: a, Method: "sys.updater.check"}
	result, err := a.updaterCheck(c, json.RawMessage(`{"manual":true,"url":"https://github.com/someone/else"}`))
	if err != nil {
		t.Fatal(err)
	}
	state, ok := result.(updater.State)
	if !ok || state.Configured || state.Source != "" {
		t.Fatalf("a caller supplied url must not configure the updater: %+v", result)
	}
	if a.upd.State().Configured {
		t.Fatal("updater became configured from an RPC parameter")
	}
}

func TestUpdaterConfiguredOnlyWithValidIdentity(t *testing.T) {
	t.Setenv("NEXWEB_UPDATE_URL", "https://github.com/owner/repo")
	if a := newTestApp(t, Config{Version: "dev", Build: "-"}); a.upd.State().Configured {
		t.Fatal("dev identity must leave the updater unconfigured")
	}
	if a := newTestApp(t, Config{Version: "1.2.3", Build: "bad"}); a.upd.State().Configured {
		t.Fatal("malformed build must leave the updater unconfigured")
	}
	if a := newTestApp(t, Config{Version: "1.2.3", Build: "R281026"}); !a.upd.State().Configured {
		t.Fatal("valid identity with a source must configure the updater")
	}
}

func TestInstallModeFollowsEnvironment(t *testing.T) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running inside a container")
	}
	for _, key := range []string{"KUBERNETES_SERVICE_HOST", "INVOCATION_ID", "JOURNAL_STREAM", "XPC_SERVICE_NAME", "NEX_UPDATER_HEALTH", hostenv.ChildEnv} {
		t.Setenv(key, "")
	}
	a := newTestApp(t, Config{})
	if mode := a.updHost.InstallMode(); mode != updater.InstallNotify {
		t.Fatalf("foreground process: mode = %s", mode)
	}
	t.Setenv(hostenv.ChildEnv, "1")
	if mode := a.updHost.InstallMode(); mode != updater.InstallSelf {
		t.Fatalf("daemon child: mode = %s", mode)
	}
	t.Setenv("INVOCATION_ID", "abc")
	if mode := a.updHost.InstallMode(); mode != updater.InstallNotify {
		t.Fatalf("systemd supervised: mode = %s", mode)
	}
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if mode := a.updHost.InstallMode(); mode != updater.InstallNotify {
		t.Fatalf("kubernetes: mode = %s", mode)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv(hostenv.ChildEnv, "")
	t.Setenv("NEX_UPDATER_HEALTH", "/tmp/x")
	if mode := a.updHost.InstallMode(); mode != updater.InstallSelf {
		t.Fatalf("launched by the updater helper: mode = %s", mode)
	}
}

func TestHealthConfirmedWithoutAnyBrowser(t *testing.T) {
	health := filepath.Join(t.TempDir(), "health")
	t.Setenv("NEX_UPDATER_HEALTH", health)
	a := newTestApp(t, Config{Addr: freeAddr(t), PublicAddr: freeAddr(t)})
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(health); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health was not confirmed although no browser is connected")
		}
		time.Sleep(50 * time.Millisecond)
	}
	a.Quit()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Quit")
	}
}

func TestHealthNotConfirmedWhenNotLaunchedByHelper(t *testing.T) {
	health := filepath.Join(t.TempDir(), "health")
	t.Setenv("NEX_UPDATER_HEALTH", "")
	a := newTestApp(t, Config{Addr: freeAddr(t), PublicAddr: freeAddr(t)})
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	time.Sleep(500 * time.Millisecond)
	a.Quit()
	<-done
	if _, err := os.Stat(health); err == nil {
		t.Fatal("health file must not exist")
	}
}

func TestIsRemoteRequest(t *testing.T) {
	tests := []struct {
		remote string
		header map[string]string
		want   bool
	}{
		{"127.0.0.1:1", nil, false},
		{"[::1]:1", nil, false},
		{"192.168.1.5:1", nil, true},
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "127.0.0.1, 10.0.0.2"}, true},
		{"127.0.0.1:1", map[string]string{"X-Forwarded-For": "::1"}, false},
		{"127.0.0.1:1", map[string]string{"X-Real-Ip": "8.8.8.8"}, true},
		{"127.0.0.1:1", map[string]string{"Forwarded": "for=8.8.8.8"}, true},
	}
	for _, test := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = test.remote
		for k, v := range test.header {
			r.Header.Set(k, v)
		}
		if got := isRemoteRequest(r); got != test.want {
			t.Errorf("%s %v: got %v want %v", test.remote, test.header, got, test.want)
		}
	}
	if isRemoteRequest(nil) {
		t.Error("a nil request is internal")
	}
}
