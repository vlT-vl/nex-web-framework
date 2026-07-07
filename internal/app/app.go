// Package app wires together an HTTP server, session security, RPC dispatch,
// and a Server-Sent Events bridge for backend-to-frontend events.
// No native webview or CGO dependency.
package app

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"nex-web/config"
	"nex-web/internal/core"
	"nex-web/internal/meta"
	"nex-web/internal/session"
	"nex-web/internal/system"
)

// Config configures the web application.
// Name/Version/Build/Author describe the *app*, not the framework.
// Framework identity is available via sys.framework.info / sys.framework.stack.
type Config struct {
	Name    string // your app name (default "nex-web")
	Version string // your app version, e.g. "1.0.0"
	Build   string // your app build tag, e.g. "B20260626" (optional)
	Author  string // your app author (optional)

	Debug          bool
	Dist           embed.FS
	DistDir        string        // subdirectory inside Dist (default "frontend/dist")
	Addr           string        // backend listen addr, ":0" = OS-assigned random port (default)
	PublicAddr     string        // public proxy addr, e.g. ":3000" — empty disables the proxy
	IdleTimeout    time.Duration // session idle timeout (default 12h)
	EnvFile        string        // default ".env"
	PublicPrefix   string        // default "VITE_"
	AllowedOrigins []string      // if non-empty, restrict allowed CORS origins
	FSRoot         string        // if non-empty, all filesystem API calls are jailed to this directory

	// SecurityPolicy and focused hooks are optional. Nil means allow, preserving
	// the framework's trusted-frontend default.
	SecurityPolicy     core.SecurityPolicy
	OnSecurityDecision func(*core.Context, core.SecurityDecision) error
	OnShellCommand     func(*core.Context, core.SecurityDecision) error
	OnFileAccess       func(*core.Context, core.SecurityDecision) error
	OnHTTPFetch        func(*core.Context, core.SecurityDecision) error
	OnEnvAccess        func(*core.Context, core.SecurityDecision) error
	OnProcessList      func(*core.Context, core.SecurityDecision) error
	OnNetworkAccess    func(*core.Context, core.SecurityDecision) error
	OnKVAccess         func(*core.Context, core.SecurityDecision) error
	OnAppControl       func(*core.Context, core.SecurityDecision) error
}

type sseEvent struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// App is a running nex-web application.
type App struct {
	cfg      Config
	sessions *session.Manager
	token    string // session token — delivered to the browser only via SSE, never in a file
	handlers map[string]core.HandlerFunc
	env      *config.Env
	quit     context.CancelFunc

	nonceMu sync.Mutex
	nonces  map[string]time.Time // one-time nonces: nonce → expiry (60 s)

	sseMu   sync.RWMutex
	sseSubs map[chan sseEvent]struct{}
}

// New creates the application, loads .env, and registers all sys.* handlers.
func New(cfg Config) *App {
	if cfg.Name == "" {
		cfg.Name = "nex-web"
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.DistDir == "" {
		cfg.DistDir = "frontend/dist"
	}
	if cfg.Addr == "" {
		cfg.Addr = ":0"
	}
	if cfg.EnvFile == "" {
		cfg.EnvFile = ".env"
	}
	if cfg.PublicPrefix == "" {
		cfg.PublicPrefix = config.DefaultPublicPrefix
	}
	a := &App{
		cfg:      cfg,
		sessions: session.NewManager(cfg.IdleTimeout),
		handlers: map[string]core.HandlerFunc{},
		env:      config.Load(cfg.EnvFile),
		nonces:   map[string]time.Time{},
		sseSubs:  map[chan sseEvent]struct{}{},
	}
	system.Register(a, cfg.FSRoot)
	a.Register("sys.app.info", a.sysAppInfo)
	a.Register("sys.framework.info", a.sysFrameworkInfo)
	a.Register("sys.framework.stack", a.sysFrameworkStack)
	return a
}

// Register implements core.Registrar (internal use, no namespace guard).
func (a *App) Register(method string, fn core.HandlerFunc) {
	a.handlers[method] = fn
}

// Handle registers an application handler. The "sys." namespace is reserved.
func (a *App) Handle(method string, fn core.HandlerFunc) {
	if strings.HasPrefix(method, "sys.") {
		panic("nex-web: 'sys.' namespace is reserved")
	}
	a.handlers[method] = fn
}

// Env returns the loaded environment (for reading backend-only variables).
func (a *App) Env() *config.Env { return a.env }

// --- core.Host ---------------------------------------------------------------

// OnMain calls fn directly. In the web variant there is no single-threaded
// UI loop, so all goroutines are equally valid callers.
func (a *App) OnMain(fn func()) { fn() }

func (a *App) Authorize(c *core.Context, d core.SecurityDecision) error {
	if d.Method == "" && c != nil {
		d.Method = c.Method
	}
	if a.cfg.SecurityPolicy != nil {
		if err := a.cfg.SecurityPolicy.Allow(c, d); err != nil {
			return err
		}
	}
	if a.cfg.OnSecurityDecision != nil {
		if err := a.cfg.OnSecurityDecision(c, d); err != nil {
			return err
		}
	}
	switch d.Category {
	case "shell":
		if a.cfg.OnShellCommand != nil {
			return a.cfg.OnShellCommand(c, d)
		}
	case "filesystem":
		if a.cfg.OnFileAccess != nil {
			return a.cfg.OnFileAccess(c, d)
		}
	case "http":
		if a.cfg.OnHTTPFetch != nil {
			return a.cfg.OnHTTPFetch(c, d)
		}
	case "env":
		if a.cfg.OnEnvAccess != nil {
			return a.cfg.OnEnvAccess(c, d)
		}
	case "process":
		if a.cfg.OnProcessList != nil {
			return a.cfg.OnProcessList(c, d)
		}
	case "network":
		if a.cfg.OnNetworkAccess != nil {
			return a.cfg.OnNetworkAccess(c, d)
		}
	case "kv":
		if a.cfg.OnKVAccess != nil {
			return a.cfg.OnKVAccess(c, d)
		}
	case "app":
		if a.cfg.OnAppControl != nil {
			return a.cfg.OnAppControl(c, d)
		}
	}
	return nil
}

// Emit broadcasts an event to all connected SSE clients.
// The frontend receives it as a CustomEvent "nex:<event>" with the payload as detail.
func (a *App) Emit(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := sseEvent{Name: event, Payload: json.RawMessage(b)}
	a.sseMu.RLock()
	defer a.sseMu.RUnlock()
	for ch := range a.sseSubs {
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
		}
	}
}

// Quit initiates a graceful server shutdown.
func (a *App) Quit() {
	if a.quit != nil {
		a.quit()
	}
}

// --- sys.app.info / sys.framework.info ---------------------------------------

// sysAppInfo returns app-level metadata set by the developer via Config.
func (a *App) sysAppInfo(_ *core.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"name":    a.cfg.Name,
		"version": a.cfg.Version,
		"build":   a.cfg.Build,
		"author":  a.cfg.Author,
		"public":  a.env.Public(a.cfg.PublicPrefix),
	}, nil
}

// sysFrameworkInfo returns nex-web framework identity from internal/meta.
// The response includes a nested "stack" field with Go version and build settings.
func (a *App) sysFrameworkInfo(_ *core.Context, _ json.RawMessage) (any, error) {
	return meta.Info(), nil
}

// sysFrameworkStack returns the resolved runtime stack (Go version, build settings, deps).
func (a *App) sysFrameworkStack(_ *core.Context, _ json.RawMessage) (any, error) {
	return meta.Stack(), nil
}

// --- Run ---------------------------------------------------------------------

// Run starts the HTTP backend on a random (or configured) port, optionally
// starts a public proxy on PublicAddr (default :3000), and blocks until
// Quit() is called or a fatal server error occurs.
func (a *App) Run() error {
	sess, err := a.sessions.Create()
	if err != nil {
		return err
	}
	a.token = sess.Token

	dist, err := fs.Sub(a.cfg.Dist, a.cfg.DistDir)
	if err != nil {
		return fmt.Errorf("invalid dist dir %q: %w", a.cfg.DistDir, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.quit = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/nex.js", a.handleNexJS)
	mux.HandleFunc("/api/token", a.handleToken) // unauthenticated — returns a one-time nonce
	mux.Handle("/api/rpc", a.secure(http.HandlerFunc(a.handleRPC)))
	mux.Handle("/api/session", a.secure(http.HandlerFunc(a.handleSession)))
	mux.HandleFunc("/api/events", a.handleSSE) // auth handled inline: nonce or session token
	mux.Handle("/", spaHandler(dist))

	ln, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return err
	}
	backendAddr := ln.Addr().String()
	log.Printf("nex-web backend on http://%s", backendAddr)

	var pubLn net.Listener
	var pubTarget *url.URL
	if a.cfg.PublicAddr != "" {
		pubTarget, _ = url.Parse("http://" + backendAddr)
		pubLn, err = net.Listen("tcp", a.cfg.PublicAddr)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("public addr %s unavailable: %w", a.cfg.PublicAddr, err)
		}
	}

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		_ = srv.Shutdown(shutCtx)
	}()

	srvErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			srvErr <- err
		}
	}()

	// Public proxy: forwards :PublicAddr → backend random port.
	// This allows clients to always connect to a well-known port (3000)
	// while the internal backend uses an unpredictable port.
	if pubLn != nil {
		proxy := httputil.NewSingleHostReverseProxy(pubTarget)
		proxy.FlushInterval = -1 // flush immediately for SSE streaming
		log.Printf("nex-web listening on http://localhost%s", a.cfg.PublicAddr)
		pubSrv := &http.Server{Handler: proxy}
		go func() {
			<-ctx.Done()
			shutCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
			defer sc()
			_ = pubSrv.Shutdown(shutCtx)
		}()
		go func() {
			if err := pubSrv.Serve(pubLn); err != nil && err != http.ErrServerClosed {
				log.Printf("nex-web public: %v", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-srvErr:
		return err
	}
}

// --- Nonce helpers -----------------------------------------------------------

// createNonce generates a single-use 16-byte hex nonce valid for 60 s.
// The nonce lets the browser open SSE without carrying the real session token
// in any file or URL that could be logged or cached.
func (a *App) createNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(b)
	expiry := time.Now().Add(60 * time.Second)
	a.nonceMu.Lock()
	now := time.Now()
	for n, exp := range a.nonces { // sweep expired entries
		if now.After(exp) {
			delete(a.nonces, n)
		}
	}
	a.nonces[nonce] = expiry
	a.nonceMu.Unlock()
	return nonce, nil
}

// consumeNonce validates and atomically deletes a nonce (one-time use).
func (a *App) consumeNonce(nonce string) bool {
	if nonce == "" {
		return false
	}
	a.nonceMu.Lock()
	defer a.nonceMu.Unlock()
	exp, ok := a.nonces[nonce]
	if !ok {
		return false
	}
	delete(a.nonces, nonce)
	return time.Now().Before(exp)
}

// --- /nex.js + /api/token ----------------------------------------------------

// handleToken returns a fresh single-use nonce for SSE authentication.
// The nonce alone grants no access — it only authorises opening the SSE stream,
// which then delivers the real session token as the connected event payload.
// Unauthenticated callers get a harmless nonce, not the session token.
func (a *App) handleToken(w http.ResponseWriter, _ *http.Request) {
	nonce, err := a.createNonce()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce})
}

// handleNexJS serves the bootstrap script.
// It embeds a per-request one-time nonce (not the session token).
// The real token is delivered by the server inside the first SSE "connected"
// event and stored in window.__NEX__.token — it never appears in a file.
// On SSE disconnect the script calls /api/token for a fresh nonce and
// reopens the stream, recovering from server restarts automatically.
func (a *App) handleNexJS(w http.ResponseWriter, _ *http.Request) {
	nonce, err := a.createNonce()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `(function(){
  window.__NEX__={token:"",base:""};
  window.__nexEmit=function(n,p){
    window.dispatchEvent(new CustomEvent("nex:"+n,{detail:p}));
  };
  function openSSE(param){
    var es=new EventSource("/api/events?"+param);
    es.onmessage=function(e){
      try{
        var d=JSON.parse(e.data);
        if(d.name==="connected"&&d.payload&&d.payload.token){
          window.__NEX__.token=d.payload.token;
        }
        window.__nexEmit(d.name,d.payload);
      }catch(_){}
    };
    es.onerror=function(){
      es.close();
      window.__nexEmit("disconnected",null);
      setTimeout(reconnect,1500);
    };
  }
  function reconnect(){
    fetch("/api/token")
      .then(function(r){return r.ok?r.json():Promise.reject();})
      .then(function(d){openSSE("nonce="+encodeURIComponent(d.nonce));})
      .catch(function(){setTimeout(reconnect,1500);});
  }
  openSSE("nonce="+encodeURIComponent(%q));
})();`, nonce)
}

// --- Security middleware -----------------------------------------------------

// secure validates the X-nex-Token header and optionally checks CORS origin.
func (a *App) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.originAllowed(r) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		if _, ok := a.sessions.Validate(r.Header.Get("X-nex-Token")); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed decides whether a browser-originated request may reach a
// sensitive endpoint. Requests with no Origin header (non-browser callers:
// curl, server-to-server, same-machine tooling) are always allowed — Origin
// is a browser-enforced header, so its absence is not itself cross-origin
// browser traffic.
//
// If Config.AllowedOrigins is set, it is the sole authority (explicit
// override). Otherwise the default allows any loopback Origin (localhost,
// 127.0.0.1, ::1) regardless of port, and rejects everything else. Checking
// the Origin's own host — rather than comparing it against the request's
// Host header — is deliberate: reverse proxies (the public :3000 proxy, the
// Vite dev proxy) do not reliably preserve the original Host header when
// forwarding, so an Origin-vs-Host comparison breaks depending on proxy
// configuration. A loopback Origin can only come from a page served from the
// same machine, which is exactly the local-first / dev-proxy scenario this
// framework targets; a real remote attacker's page never has one. Anything
// non-loopback (a real public deployment) must opt in via AllowedOrigins.
func (a *App) originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	if len(a.cfg.AllowedOrigins) > 0 {
		for _, allowed := range a.cfg.AllowedOrigins {
			if allowed == o {
				return true
			}
		}
		return false
	}
	origin, err := url.Parse(o)
	if err != nil {
		return false
	}
	return isLoopbackHost(origin.Hostname())
}

// isLoopbackHost reports whether host (an Origin hostname, no port) refers to
// the local machine: "localhost" or a loopback IP (127.0.0.0/8, ::1).
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// --- Handlers ----------------------------------------------------------------

func (a *App) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, _ := a.sessions.Validate(r.Header.Get("X-nex-Token"))
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        sess.ID,
		"expiresAt": sess.ExpiresAt.Format(time.RFC3339),
	})
}

func (a *App) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRPCErr(w, "method_not_allowed", "use POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20) // 8 MiB limit
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCErr(w, "bad_request", "invalid JSON")
		return
	}
	fn, ok := a.handlers[req.Method]
	if !ok {
		writeRPCErr(w, "not_found", "unknown method: "+req.Method)
		return
	}
	sess, _ := a.sessions.Validate(r.Header.Get("X-nex-Token"))
	c := &core.Context{Ctx: r.Context(), Host: a, Session: sess, Request: r, Method: req.Method}
	result, err := fn(c, req.Params)
	if err != nil {
		if rpcErr, ok2 := err.(*core.RPCError); ok2 {
			writeJSON(w, http.StatusOK, map[string]any{"error": rpcErr})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"error": &core.RPCError{Code: "internal", Message: err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

// handleSSE streams backend events to the browser via Server-Sent Events.
// Auth is handled inline: accepts either a one-time ?nonce= (first connect,
// issued by /nex.js) or a ?token= session token (reconnect path).
// When a nonce is consumed the real session token is delivered in the
// "connected" event payload so the browser can use it for subsequent RPC calls.
func (a *App) handleSSE(w http.ResponseWriter, r *http.Request) {
	if !a.originAllowed(r) {
		http.Error(w, "bad origin", http.StatusForbidden)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Auth: always via one-time nonce (prevents the real token from appearing in URLs or access logs).
	// The nonce is issued by /nex.js (first connect) or /api/token (reconnect).
	nonce := r.URL.Query().Get("nonce")
	if !a.consumeNonce(nonce) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deliverToken := a.token // send real token inside the connected event

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	ch := make(chan sseEvent, 16)
	a.sseMu.Lock()
	a.sseSubs[ch] = struct{}{}
	a.sseMu.Unlock()
	defer func() {
		a.sseMu.Lock()
		delete(a.sseSubs, ch)
		a.sseMu.Unlock()
	}()

	// Send connected event with the real token in the payload.
	tokenJSON, _ := json.Marshal(deliverToken)
	fmt.Fprintf(w, "data: {\"name\":\"connected\",\"payload\":{\"token\":%s}}\n\n", tokenJSON)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- Helpers -----------------------------------------------------------------

func spaHandler(dist fs.FS) http.Handler {
	fs_ := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			http.ServeFileFS(w, r2, dist, "index.html")
			return
		}
		fs_.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCErr(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"error": &core.RPCError{Code: code, Message: msg},
	})
}
