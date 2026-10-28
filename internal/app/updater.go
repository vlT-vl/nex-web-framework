package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"nex-web/internal/core"
	"nex-web/internal/hostenv"
	"nex-web/internal/updater"
)

type updaterHost struct {
	app            *App
	quiesceTimeout time.Duration
	mu             sync.Mutex
	grants         map[string]int
}

func (h *updaterHost) AppID() string {
	return h.app.cfg.Name
}

func (h *updaterHost) Version() (string, string) {
	return h.app.cfg.Version, h.app.cfg.Build
}

func (h *updaterHost) StateDir() string {
	return h.app.cfg.StateDir
}

func (h *updaterHost) grant(op string) func() {
	h.mu.Lock()
	h.grants[op]++
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.grants[op]--
		h.mu.Unlock()
	}
}

func (h *updaterHost) granted(op string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.grants[op] > 0
}

func (h *updaterHost) Authorize(op string) error {
	if h.granted(op) {
		return nil
	}
	if op == "install" {
		return errors.New("install requires an authorized request")
	}
	c := &core.Context{Ctx: context.Background(), Host: h.app, Method: "sys.updater." + op}
	return c.Authorize(core.SecurityDecision{Category: "app", Operation: "update." + op})
}

func (h *updaterHost) Emit(event string, payload any) {
	h.app.Emit(event, payload)
}

func (h *updaterHost) Quiesce() error {
	h.app.Quit()
	addr := h.app.cfg.PublicAddr
	if addr == "" {
		return nil
	}
	probe := dialAddr(addr)
	deadline := time.Now().Add(h.quiesceTimeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", probe, 250*time.Millisecond)
		if err != nil {
			return nil
		}
		conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("public port was not released")
}

func (h *updaterHost) RelaunchEnv() []string {
	environment := os.Environ()
	out := make([]string, 0, len(environment))
	prefix := hostenv.ChildEnv + "="
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return out
}

func (h *updaterHost) HealthMode() updater.HealthMode {
	return updater.HealthBackend
}

func (h *updaterHost) InstallMode() updater.InstallMode {
	if hostenv.Supervised() {
		return updater.InstallNotify
	}
	if hostenv.DaemonChild() || hostenv.UpdaterLaunched() {
		return updater.InstallSelf
	}
	return updater.InstallNotify
}

func dialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func updaterIdentityValid(version, build string) bool {
	_, _, ok := updater.ParseTag("v" + version + "-" + build)
	return ok
}

func (a *App) setupUpdater() {
	if a.cfg.StateDir == "" {
		a.cfg.StateDir = hostenv.StateDir(a.cfg.Name)
	}
	source := os.Getenv("NEXWEB_UPDATE_URL")
	if !updaterIdentityValid(a.cfg.Version, a.cfg.Build) {
		source = ""
	}
	a.updHost = &updaterHost{app: a, quiesceTimeout: 10 * time.Second, grants: map[string]int{}}
	a.upd = updater.New(updater.Config{
		AppID:        a.cfg.Name,
		SourceURL:    source,
		AssetPattern: os.Getenv("NEXWEB_UPDATE_ASSET"),
		Layout:       "binary",
		AllowedHosts: a.cfg.UpdateAllowedHosts,
		PublicKeys:   a.cfg.UpdatePublicKeys,
	}, a.updHost)

	a.Register("sys.updater.getState", a.updaterGetState)
	a.Register("sys.updater.check", a.updaterCheck)
	a.Register("sys.updater.download", a.updaterDownload)
	a.Register("sys.updater.install", a.updaterInstall)
	a.Register("sys.updater.confirm", a.updaterConfirm)
	a.Register("sys.updater.getLastResult", a.updaterLastResult)
	a.Register("sys.updater.setSettings", a.updaterSetSettings)
}

func (a *App) startUpdater(backendAddr string) {
	a.startHealthProbe(backendAddr)
	a.startAutoUpdate()
}

func (a *App) startAutoUpdate() {
	a.updOnce.Do(func() {
		go func() {
			state := a.upd.State()
			if !state.Configured || !state.Settings.AutoCheck {
				return
			}
			ctx := context.Background()
			state, err := a.upd.Check(ctx, false)
			if err != nil {
				log.Printf("%s updater check: %v", a.cfg.Name, err)
				return
			}
			if state.Status == "available" && state.Settings.AutoDownload && state.Update != nil && state.Update.Installable && !state.Update.RolledBack {
				if err := a.upd.Download(ctx); err != nil {
					log.Printf("%s updater download: %v", a.cfg.Name, err)
				}
			}
		}()
	})
}

func (a *App) startHealthProbe(backendAddr string) {
	if !hostenv.UpdaterLaunched() {
		return
	}
	go func() {
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			if a.selfProbe(backendAddr) {
				if _, err := updater.Confirm(); err != nil {
					log.Printf("%s updater confirm: %v", a.cfg.Name, err)
				}
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		log.Printf("%s updater: health probe did not succeed", a.cfg.Name)
	}()
}

func (a *App) selfProbe(backendAddr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	if a.cfg.PublicAddr != "" {
		response, err := client.Get("http://" + dialAddr(a.cfg.PublicAddr) + "/")
		if err != nil {
			return false
		}
		response.Body.Close()
		if response.StatusCode >= http.StatusInternalServerError {
			return false
		}
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+dialAddr(backendAddr)+"/api/rpc", bytes.NewReader([]byte(`{"method":"sys.app.info"}`)))
	if err != nil {
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-nex-Token", a.token)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var body struct {
		Result struct {
			Version string `json:"version"`
			Build   string `json:"build"`
		} `json:"result"`
	}
	if json.NewDecoder(response.Body).Decode(&body) != nil {
		return false
	}
	return body.Result.Version == a.cfg.Version && body.Result.Build == a.cfg.Build
}

func loopbackIP(value string) bool {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	ip := net.ParseIP(strings.Trim(value, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isRemoteRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !loopbackIP(r.RemoteAddr) {
		return true
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		for _, value := range r.Header.Values(header) {
			for _, part := range strings.Split(value, ",") {
				if strings.TrimSpace(part) != "" && !loopbackIP(part) {
					return true
				}
			}
		}
	}
	return r.Header.Get("Forwarded") != ""
}

func (a *App) updaterGuard(c *core.Context, op string) (func(), error) {
	decision := core.SecurityDecision{Category: "app", Operation: "update." + op}
	if c.Request != nil {
		decision.Host = c.Request.RemoteAddr
	}
	if op == "install" && isRemoteRequest(c.Request) && !a.cfg.UpdateAllowRemoteInstall {
		return nil, core.Errorf("unauthorized", "update install is not allowed for remote clients")
	}
	if err := c.Authorize(decision); err != nil {
		return nil, core.Errorf("unauthorized", "%v", err)
	}
	return a.updHost.grant(op), nil
}

func updaterError(err error) error {
	if err == nil {
		return nil
	}
	if code := updater.ErrorCode(err); code != "" {
		return core.Errorf(code, "%v", err)
	}
	return core.Errorf("updater", "%v", err)
}

func (a *App) updaterGetState(_ *core.Context, _ json.RawMessage) (any, error) {
	return a.upd.State(), nil
}

func (a *App) updaterCheck(c *core.Context, params json.RawMessage) (any, error) {
	var p struct {
		Manual bool `json:"manual"`
	}
	if len(params) > 0 {
		if err := c.Bind(params, &p); err != nil {
			return nil, core.Errorf("bad_request", "%v", err)
		}
	}
	state := a.upd.State()
	if !state.Configured {
		return state, nil
	}
	release, err := a.updaterGuard(c, "check")
	if err != nil {
		return nil, err
	}
	defer release()
	state, err = a.upd.Check(c.Ctx, p.Manual)
	if err != nil {
		return nil, updaterError(err)
	}
	return state, nil
}

func (a *App) updaterDownload(c *core.Context, _ json.RawMessage) (any, error) {
	release, err := a.updaterGuard(c, "download")
	if err != nil {
		return nil, err
	}
	defer release()
	if err := a.upd.Download(c.Ctx); err != nil {
		return nil, updaterError(err)
	}
	return a.upd.State(), nil
}

func (a *App) updaterInstall(c *core.Context, _ json.RawMessage) (any, error) {
	release, err := a.updaterGuard(c, "install")
	if err != nil {
		return nil, err
	}
	defer release()
	if err := a.upd.Install(c.Ctx); err != nil {
		return nil, updaterError(err)
	}
	return map[string]any{"status": "installing"}, nil
}

func (a *App) updaterConfirm(_ *core.Context, _ json.RawMessage) (any, error) {
	confirmed, err := a.upd.Confirm()
	if err != nil {
		return nil, updaterError(err)
	}
	return map[string]any{"confirmed": confirmed}, nil
}

func (a *App) updaterLastResult(_ *core.Context, _ json.RawMessage) (any, error) {
	result, err := a.upd.LastResult()
	if err != nil {
		return nil, updaterError(err)
	}
	return result, nil
}

func (a *App) updaterSetSettings(c *core.Context, params json.RawMessage) (any, error) {
	var settings updater.Settings
	if err := c.Bind(params, &settings); err != nil {
		return nil, core.Errorf("bad_request", "%v", err)
	}
	if err := a.upd.SetSettings(settings); err != nil {
		return nil, updaterError(err)
	}
	return a.upd.State(), nil
}
