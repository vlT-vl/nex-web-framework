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

type Config struct {
	Name    string
	Version string
	Build   string
	Author  string

	Debug          bool
	Dist           embed.FS
	DistDir        string
	Addr           string
	PublicAddr     string
	IdleTimeout    time.Duration
	EnvFile        string
	PublicPrefix   string
	AllowedOrigins []string
	FSRoot         string

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

type App struct {
	cfg      Config
	sessions *session.Manager
	token    string
	handlers map[string]core.HandlerFunc
	env      *config.Env
	quit     context.CancelFunc

	nonceMu sync.Mutex
	nonces  map[string]time.Time

	sseMu   sync.RWMutex
	sseSubs map[chan sseEvent]struct{}
}

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

func (a *App) Register(method string, fn core.HandlerFunc) {
	a.handlers[method] = fn
}

func (a *App) Handle(method string, fn core.HandlerFunc) {
	if strings.HasPrefix(method, "sys.") {
		panic("nex-web: 'sys.' namespace is reserved")
	}
	a.handlers[method] = fn
}

func (a *App) Env() *config.Env { return a.env }

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
		default:
		}
	}
}

func (a *App) Quit() {
	if a.quit != nil {
		a.quit()
	}
}

func (a *App) sysAppInfo(_ *core.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"name":    a.cfg.Name,
		"version": a.cfg.Version,
		"build":   a.cfg.Build,
		"author":  a.cfg.Author,
		"public":  a.env.Public(a.cfg.PublicPrefix),
	}, nil
}

func (a *App) sysFrameworkInfo(_ *core.Context, _ json.RawMessage) (any, error) {
	return meta.Info(), nil
}

func (a *App) sysFrameworkStack(_ *core.Context, _ json.RawMessage) (any, error) {
	return meta.Stack(), nil
}

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
	mux.HandleFunc("/api/token", a.handleToken)
	mux.Handle("/api/rpc", a.secure(http.HandlerFunc(a.handleRPC)))
	mux.Handle("/api/session", a.secure(http.HandlerFunc(a.handleSession)))
	mux.HandleFunc("/api/events", a.handleSSE)
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

	if pubLn != nil {
		proxy := httputil.NewSingleHostReverseProxy(pubTarget)
		proxy.FlushInterval = -1
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

func (a *App) createNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(b)
	expiry := time.Now().Add(60 * time.Second)
	a.nonceMu.Lock()
	now := time.Now()
	for n, exp := range a.nonces {
		if now.After(exp) {
			delete(a.nonces, n)
		}
	}
	a.nonces[nonce] = expiry
	a.nonceMu.Unlock()
	return nonce, nil
}

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

func (a *App) handleToken(w http.ResponseWriter, _ *http.Request) {
	nonce, err := a.createNonce()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"nonce": nonce})
}

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

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

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
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
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

	nonce := r.URL.Query().Get("nonce")
	if !a.consumeNonce(nonce) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deliverToken := a.token

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan sseEvent, 16)
	a.sseMu.Lock()
	a.sseSubs[ch] = struct{}{}
	a.sseMu.Unlock()
	defer func() {
		a.sseMu.Lock()
		delete(a.sseSubs, ch)
		a.sseMu.Unlock()
	}()

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
