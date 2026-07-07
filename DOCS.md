<p align="center">
  <img src="res/nexweb.svg" alt="nex-web" width="165" />
</p>

<p align="center">
  Single-binary web framework: React/Vite frontend + Go backend<br/>
  <sub>No CGO · No native webview · Embedded assets · Daemon mode · Garble obfuscation</sub>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-0.1.0--R070726-blue?style=flat-square" alt="version"/>
  <img src="https://img.shields.io/badge/go-1.26.4-00ADD8?style=flat-square&logo=go" alt="go"/>
  <img src="https://img.shields.io/badge/react-19.2.7-61DAFB?style=flat-square&logo=react&logoColor=white" alt="react"/>
  <img src="https://img.shields.io/badge/react--icons-5.7.0-61DAFB?style=flat-square&logo=react&logoColor=white" alt="react-icons"/>
  <img src="https://img.shields.io/badge/vite-8.1.3-646CFF?style=flat-square&logo=vite&logoColor=white" alt="vite"/>
  <img src="https://img.shields.io/badge/CGO-disabled-success?style=flat-square" alt="cgo"/>
  <img src="https://img.shields.io/badge/license-proprietary-critical?style=flat-square" alt="license"/>
</p>

---

## Documentation scope

This file is the complete nex-web framework manual: architecture, session security,
build system, environment model, and full API reference.
For the short project entry point and quick start, see [README.md](README.md).

Maintenance rule: every framework change must update **README.md**, **DOCS.md**,
and **handoff.md** in the same pass.

---

## What is nex-web?

nex-web builds self-contained web server applications with a React/Vite UI and a Go
backend. The Vite build output is embedded into the Go binary with `//go:embed`.
At runtime nex-web starts an HTTP backend on a random internal port, starts a public
reverse proxy on port 3000, and serves the embedded SPA with a typed RPC layer and
a Server-Sent Events bridge.

```
Browser → http://localhost:3000      (public proxy, stable port)
                 ↓  transparent reverse proxy
Go HTTP backend  http://127.0.0.1:<random>
   /            → embedded Vite SPA  (embed.FS)
   /nex.js      → bootstrap script  (one-time nonce + SSE setup)
   /api/token   → returns a fresh single-use nonce for reconnect
   /api/rpc     → typed RPC dispatch  (X-nex-Token required)
   /api/session → session metadata
   /api/events  → SSE stream for backend→frontend events
```

Unlike the desktop variant there is no native webview, no CGO, and no platform C
toolchain required. Any `go build` cross-compiles to macOS, Linux, and Windows from
any host machine.

---

## Features

| Category | What nex-web provides |
|---|---|
| Packaging | Single binary; frontend assets embedded via `//go:embed` |
| Transport | Pure HTTP — no CGO, no native webview |
| RPC | `POST /api/rpc` with typed JSON responses and errors |
| Events | `app.Emit(event, payload)` via SSE → `nex.on(event, cb)` |
| Session security | One-time nonce bootstrap; token delivered only via SSE; sliding 12h expiry |
| Daemon mode | Self-fork at startup; parent exits; child runs detached |
| Port routing | Backend on random port; public proxy on `:3000` |
| App identity | Name, version, build, author — all from `.env` at runtime |
| Filesystem | read, write, list, exists, stat, mkdir, remove, rename, copy, glob, abs, temp |
| Shell | exec / run / start (server-side command execution) |
| OS info | host, user, runtime, process, network, disks, memory, time — pure Go/syscall, no subprocess |
| HTTP proxy | server-side HTTP/HTTPS fetch — bypasses CORS (`http.fetch()`) |
| Env | read specific or list all backend env vars (`env.get` / `env.list`) |
| Security hooks | `SecurityPolicy` + focused hooks before sensitive RPC execution |
| Capabilities | inspect sensitive API surfaces (`security.capabilities()`) |
| File watch | poll path for changes, emit SSE events (`fs.watch` / `fs.unwatch`) |
| KV store | persistent JSON key-value store (`kv.get/set/delete/list/clear`) |
| DNS resolve | hostname → IP addresses + CNAME (`netutil.resolve`) |
| Port check | TCP dial check — port open/closed (`netutil.port`) |
| Process list | list running server processes (`proc.list`) |
| Browser reload | force-reload all clients from backend (`app.reload`) |
| Logging | frontend→backend structured log (`logger.info/debug/error/…`) |
| Env model | `VITE_*` vars are public; all others stay backend-only |
| App icon | override favicon via `VITE_APP_ICON` in `.env` — no rebuild |
| App title | override browser tab title via `VITE_APP_TITLE` in `.env` |
| Docs modal | built-in rendered docs modal (sanitized HTML + anchor nav) — opens DOCS.md |
| License modal | built-in plain-text license modal — opens `LICENSE` from repo root via Vite `?raw` import, never hardcoded |
| API risk levels | `apiRiskLevel(id)` → `"safe"` / `"user"` / `"privileged"` — colored badges in template |
| Build | `go run build.go` / `make` — `CGO_ENABLED=0`, cross-compiles anywhere |
| Dev mode | `go run dev.go` / `make dev` — Vite HMR + Go backend, no daemon |
| Obfuscation | garble by default; `--plain` flag to disable |

---

## Architecture

### Daemon + port routing

When the release binary starts it checks for the `_NEXWEB_CHILD` env variable.
If absent, it is the **parent** process: it re-executes itself as a detached
child (Unix: `setsid`; Windows: `CREATE_NEW_PROCESS_GROUP` + hidden window),
sets `_NEXWEB_CHILD=1`, prints the public URL, and exits immediately.

The **child** (daemon) process:

1. Starts the HTTP backend on an OS-assigned random TCP port (`:0`).
2. Starts a reverse proxy on `:3000` forwarding all traffic to the random port.
3. Runs indefinitely serving browser requests through the stable `:3000` entry point.

In **dev mode** (`make dev`), daemonisation is disabled at compile time via
`-tags dev`, the backend binds to `127.0.0.1:34116` for the Vite proxy, and
port 3000 is not started.

### Session and token flow

The session token never appears in any file on disk or any static HTTP response.

1. Browser fetches `/nex.js` → receives a **one-time nonce** (16-byte random hex, valid 60 s).
2. Browser opens `GET /api/events?nonce=<nonce>` — server **consumes the nonce atomically** (single-use, deleted immediately on first use).
3. Server sends a `connected` SSE event with the real session token as payload: `{ token: "…" }`.
4. Browser stores token in `window.__NEX__.token` — used as `X-nex-Token` header on all RPC calls.
5. On SSE disconnect → browser calls `GET /api/token` (unauthenticated, returns a fresh nonce) → reopens SSE → receives a new token. Recovers from server restarts automatically.

A stolen nonce from `/nex.js` is harmless once the legitimate client opens SSE first (first-use wins). The session token is never exposed in any file, URL, or HTTP body outside the SSE channel.

---

## Requirements

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26.4+ | CGO not required |
| Node | 18+ with npm | Frontend build only |
| garble | auto-installed | `build.go` installs it when missing |

No C compiler, no pkg-config, no platform SDK. `CGO_ENABLED=0` throughout.

```bash
make requirements   # validate all tools
```

---

## Project structure

```
nex-web-framework/
├── .env                    # App identity + backend vars + public VITE_* vars
├── config/
│   └── config.go           # .env loader
├── frontend/
│   ├── package.json        # React 19 · Vite 8 · react-icons
│   ├── vite.config.js      # Proxy /api and /nex.js → Go backend in dev
│   ├── index.html
│   ├── dist/               # Populated by npm run build
│   └── src/
│       ├── components/
│       │   ├── nexweblogo.jsx # React framework logo component
│       │   └── nexweblogo.css # Dedicated logo animation/layout
│       ├── main.jsx
│       ├── App.jsx         # Showcase template — all APIs demonstrated + docs modal
│       ├── index.css
│       └── lib/
│           ├── nex.js       # JS client bridge (RPC, SSE, all namespaces, apiRiskLevel)
│           ├── markdown.js  # Markdown renderer (sanitized HTML, slugify, anchor nav)
│           └── theme.js     # Theme preference helpers (system/light/dark)
├── internal/
│   ├── app/
│   │   └── app.go          # HTTP server, RPC dispatch, SSE, proxy, nonce manager
│   ├── core/
│   │   └── core.go         # Context, HandlerFunc, Host, Registrar, RPCError
│   ├── daemon/
│   │   ├── daemon.go           # MaybeDetach() — public entry point
│   │   ├── trigger.go          # Enabled() → true  (//go:build !dev)
│   │   ├── trigger_dev.go      # Enabled() → false (//go:build dev)
│   │   ├── detach_unix.go      # setsid (//go:build !windows)
│   │   └── detach_windows.go   # CREATE_NEW_PROCESS_GROUP
│   ├── meta/
│   │   └── meta.go         # Framework identity — Name, Version, Build, Updated, Author
│   ├── session/
│   │   └── session.go      # Per-launch token, sliding expiry, constant-time compare
│   └── system/
│       └── system.go       # Built-in sys.* RPC handlers
├── nex/
│   └── nex.go              # Public API facade — type aliases, factory, meta re-exports
├── res/
│   ├── CenturyGothic.ttf
│   ├── nexcube.svg
│   ├── nexweb.svg          # README/DOCS logo matching the React component
│   └── nexicon.svg         # Default favicon (replace with your own in res/)
├── build.go                # //go:build ignore — build / release / clean / requirements
├── dev.go                  # //go:build ignore — Vite HMR + Go backend, no daemon
├── main.go                 # Entry point — daemon.MaybeDetach + Config + embed
├── DOCS.md                 # This file — embedded in the app and rendered in the docs modal
├── Makefile
└── go.mod
```

Dependency graph:

```
config  ──┐
session ──┼──► core ──► system ──┐
meta    ──┤                      ├──► app ──► nex ──► main
          └──────────────────────┘                 ↑
daemon ────────────────────────────────────────────┘
```

No external runtime dependencies beyond the Go standard library.

---

## Desktop comparison notes

The desktop framework sibling (`projetcs/goreact/nex-framework`) is used as a
read-only reference. The web variant has adopted the compatible logo pattern:
`nexweblogo.jsx` imports `res/CenturyGothic.ttf` with Vite `?url`, injects the
`@font-face`, renders the cube/wordmark as a component, and keeps the `web`
suffix aligned through the component CSS. The README/DOCS header uses a separate
SVG asset for documentation rendering; the desktop-style lateral entrance
animation stays in `nexweblogo.css`.

The topbar color-mode switch also mirrors the desktop framework: `system`,
`light`, and `dark` are icon+label segmented buttons using the same interaction
model and CSS structure.

Desktop features adopted with web-specific semantics:

| Desktop feature | Web adaptation |
|---|---|
| `SecurityPolicy` before RPC dispatch | `SecurityPolicy` + focused hooks before sensitive `sys.*` handlers |
| build metadata helpers | `build.go` reads `.env` and resolves `envDefault(...)` in `main.go` |
| capability checks | `sys.security.capabilities` / `security.capabilities()` |
| single-instance handling | `PublicAddr` acts as web instance lock; occupied public port aborts startup |

Remaining desktop ideas that need more product design: second-instance browser
notification and opinionated default policies for multi-user/public deployments.

---

## Using the framework

### Go side

```go
package main

import (
    "embed"
    "encoding/json"
    "log"

    "nex-web/internal/daemon"
    nexweb "nex-web/nex"
)

//go:embed frontend/dist
var distFS embed.FS

func main() {
    daemon.MaybeDetach(nexweb.FrameworkVersion(), ":3000")

    a := nexweb.New(nexweb.Config{
        Name:       envDefault("NEXWEB_APP_NAME",    "my-app"),
        Version:    envDefault("NEXWEB_APP_VERSION", "1.0.0"),
        Build:      envDefault("NEXWEB_APP_BUILD",   ""),
        Author:     envDefault("NEXWEB_APP_AUTHOR",  ""),
        Dist:       distFS,
        DistDir:    "frontend/dist",
        PublicAddr: ":3000",
    })

    a.Handle("greet", func(c *nexweb.Context, params json.RawMessage) (any, error) {
        var p struct{ Name string `json:"name"` }
        if err := c.Bind(params, &p); err != nil {
            return nil, nexweb.Errorf("bad_request", "%v", err)
        }
        return map[string]string{"message": "Hello, " + p.Name + "!"}, nil
    })

    if err := a.Run(); err != nil {
        log.Fatal(err)
    }
}
```

### JS side

```js
import { call, on, os, shell, fs, kv, netutil, http, env, proc, app, framework, logger, security } from "./lib/nex.js";

const info  = await app.info();       // { name, version, build, author, public }
const fw    = await framework.info(); // { name, version, build, updated, author, stack }
const host  = await os.host();        // includes osVersion field
const result = await shell.exec("uname -a");
const list  = await fs.list("/tmp");
const { open } = await netutil.port({ host: "localhost", port: 5432 });

await kv.set("last-run", Date.now());
const off = on("tick", ({ n }) => console.log(n));
off(); // unsubscribe
```

---

## Development

```bash
make dev
# or: go run dev.go
```

Starts Vite HMR on `127.0.0.1:5181` and the Go backend on `127.0.0.1:34116`.
Vite proxies `/api` and `/nex.js` to the Go backend. No daemonisation, no port 3000.
The `-tags dev` build tag sets `daemon.Enabled() = false`.

Hot reload for the frontend is handled by Vite. Restart `make dev` for Go changes.

---

## Build system

Both `make` and `go run build.go` are available.

| `make` target | Action |
|---|---|
| `make build` | Frontend build + obfuscated binaries → `release/<version>-<build>/` |
| `make release` | Same + `-s -w` strip |
| `make obfuscate` | Alias for release with garble |
| `make dev` | Vite HMR + Go backend (dev mode, no daemon) |
| `make requirements` / `make doctor` | Validate Go, npm, garble |
| `make clean` | Remove `release/` and `frontend/dist` |
| `make tidy` | Run `go mod tidy` |

Cross-compilation targets (all from any host, `CGO_ENABLED=0`):

| Target | Binary |
|---|---|
| `darwin/amd64` | `<app>-darwin-amd64` |
| `darwin/arm64` | `<app>-darwin-arm64` |
| `linux/amd64` | `<app>-linux-amd64` |
| `linux/arm64` | `<app>-linux-arm64` |
| `windows/amd64` | `<app>-windows-amd64.exe` |

garble flags: `-literals -seed=random`. If garble is missing, `build.go` installs it automatically via `go install mvdan.cc/garble@latest`.

To skip garble (diagnostics only):

```bash
go run build.go build --plain
```

To set framework metadata at build time via ldflags:

```bash
go run build.go build -ldflags "-X nex-web/internal/meta.Version=0.1.0 -X nex-web/internal/meta.Build=R070726"
```

---

## Environment

nex-web loads `.env` from the project root at startup. Variables without `VITE_` stay
backend-only. Variables with `VITE_` are exposed to the frontend via `app.info().public`.

### App identity

All four app-identity fields are read from `.env` at runtime — no recompile needed.

| Variable | Default | Purpose |
|---|---|---|
| `NEXWEB_APP_NAME` | `"nex-web-template"` | App name returned by `sys.app.info` |
| `NEXWEB_APP_VERSION` | `"dev"` | App version returned by `sys.app.info` |
| `NEXWEB_APP_BUILD` | `""` | App build tag returned by `sys.app.info` |
| `NEXWEB_APP_AUTHOR` | `""` | App author returned by `sys.app.info` |

### Server config

| Variable | Default | Purpose |
|---|---|---|
| `NEXWEB_ADDR` | `:0` | Backend listen address — `:0` = OS-assigned random port |
| `NEXWEB_PUBLIC_ADDR` | `:3000` | Public proxy address; empty string = disabled |
| `NEXWEB_ENV_FILE` | `.env` | Path to the env file |

### Frontend-public variables (`VITE_` prefix)

| Variable | Effect |
|---|---|
| `VITE_APP_TITLE` | Browser tab title (default: `NEXWEB_APP_NAME`) |
| `VITE_APP_ICON` | Favicon URL override (default: `/nexicon.svg` from `res/`) |

Any additional `VITE_*` variable is automatically available in the frontend via `app.info().public`.

---

## Security model

| Layer | Mechanism |
|---|---|
| Session token | 32-byte random hex, generated per launch, never written to disk |
| Bootstrap | `/nex.js` embeds a **one-time nonce** (16-byte, 60 s TTL) — not the token |
| Token delivery | Real token delivered only inside the first SSE `connected` event payload |
| Nonce | Single-use: atomically consumed and deleted on first `GET /api/events?nonce=` |
| `/api/token` | Unauthenticated; returns a fresh nonce (not the token) for reconnect use |
| Reconnect | Client calls `/api/token` → gets nonce → opens SSE → receives new token |
| RPC auth | Every call requires `X-nex-Token`; validated with constant-time compare |
| SSE auth | Nonce-only: `?nonce=` on every connect — real token never appears in URLs or access logs |
| Expiry | Sliding 12 h session expiry (configurable via `Config.IdleTimeout`) |
| CORS | Optional `AllowedOrigins`; nil = allow all (appropriate for local-first use) |
| Port | Backend on unpredictable random port; only `:3000` is publicly stable |
| Public port lock | Startup fails if configured `PublicAddr` is already bound; avoids orphan hidden backends |
| Security policy | Optional `Config.SecurityPolicy` + focused hooks run before sensitive RPC execution |
| FSRoot jail | `checkPath()` resolves symlinks on full path + parent directory — symlink-escape proof |

### Deployment considerations

nex-web is designed for **local-first, single-user** use. If you expose port 3000 publicly:

- **Shell APIs** (`shell.exec/run/start`) execute server-side as the OS user running the binary. Disable them for multi-user or public deployments by not registering your app with those methods — or gate them at the application level.
- **`http.fetch`** makes outbound HTTP requests from the server's network perspective. In cloud environments this can reach internal metadata services (e.g. `169.254.169.254`). Validate or restrict URLs at the application level if needed.
- **`env.get` / `env.list`** expose process environment variables (including secrets) to authenticated clients. Consider whether your deployment requires further restriction.
- **Security hooks** can deny shell, filesystem, HTTP, env, process, network, KV, and app-control operations before execution.
- For public deployments, add a **Content-Security-Policy** header to prevent XSS from reaching `window.__NEX__.token`.

---

## Config reference

`nexweb.Config` fields passed to `nexweb.New()`:

| Field | Type | Default | Description |
|---|---|---|---|
| `Name` | `string` | `"nex-web"` | Your app name |
| `Version` | `string` | `"dev"` | Your app version |
| `Build` | `string` | `""` | Your app build tag (optional) |
| `Author` | `string` | `""` | Your app author (optional) |
| `Debug` | `bool` | `false` | Enable debug logging |
| `Dist` | `embed.FS` | — | Embedded frontend dist (required) |
| `DistDir` | `string` | `"frontend/dist"` | Subdirectory inside `Dist` |
| `Addr` | `string` | `":0"` | Backend listen address |
| `PublicAddr` | `string` | `""` | Public proxy address (e.g. `":3000"`) |
| `IdleTimeout` | `time.Duration` | `12h` | Session sliding expiry |
| `EnvFile` | `string` | `".env"` | Path to env file |
| `PublicPrefix` | `string` | `"VITE_"` | Frontend-public env var prefix |
| `AllowedOrigins` | `[]string` | `nil` | CORS allowlist; nil = allow all |
| `FSRoot` | `string` | `""` | Jail all filesystem API calls to this path |
| `SecurityPolicy` | `nexweb.SecurityPolicy` | `nil` | Global authorization hook before sensitive RPCs |
| `OnSecurityDecision` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Global focused callback after `SecurityPolicy` |
| `OnShellCommand` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Shell exec/run/start hook |
| `OnFileAccess` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Filesystem hook |
| `OnHTTPFetch` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Server-side HTTP fetch hook |
| `OnEnvAccess` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Env get/list hook |
| `OnProcessList` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | Process listing hook |
| `OnNetworkAccess` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | DNS/port-check hook |
| `OnKVAccess` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | KV mutation hook |
| `OnAppControl` | `func(*nexweb.Context, nexweb.SecurityDecision) error` | `nil` | App quit/reload hook |

`Name/Version/Build/Author` describe **your application**. Framework identity
(`nex-web`, `0.1.0`, `R070726`) always comes from `internal/meta` — never from Config.

---

## API Reference

All RPC calls use `POST /api/rpc` with the `X-nex-Token` header.
The `sys.*` namespace is reserved for built-in APIs.

### Core

| Helper | Description |
|---|---|
| `call(method, params?)` | Call any registered backend method |
| `on(event, handler)` | Subscribe to a backend SSE event; returns an unsubscribe fn |
| `session()` | Fetch current session `{ id, expiresAt }` |
| `apiRiskLevel(id)` | Returns `"safe"` / `"user"` / `"privileged"` for any `sys.*` method id |

```go
a.Emit("data-ready", map[string]any{"count": 42})
```

```js
const off = on("data-ready", (payload) => console.log(payload));
off(); // unsubscribe
```

---

### framework

| Helper | RPC | Returns |
|---|---|---|
| `framework.info()` | `sys.framework.info` | Framework identity + runtime stack |
| `framework.stack()` | `sys.framework.stack` | Resolved stack: Go version + external module versions |

`framework.info()` response:

```json
{
  "name":    "nex-web",
  "version": "0.1.0",
  "build":   "R070726",
  "updated": "7 Luglio 2026",
  "author":  "© 2026 vlT di Veronesi Lorenzo",
  "id":      "nex-web@0.1.0",
  "stack":   { "nex-web": "nex-web@0.1.0-R070726", "go": "1.26.4" }
}
```

Framework fields always come from `internal/meta` — never from app `Config`.
Override at build time: `-ldflags "-X nex-web/internal/meta.Version=x -X nex-web/internal/meta.Build=R..."`.

---

### app

| Helper | RPC | Returns |
|---|---|---|
| `app.info()` | `sys.app.info` | App-level metadata set via `Config` or `.env` |
| `app.quit()` | `sys.app.quit` | Gracefully stops the server |
| `app.reload()` | `sys.app.reload` | Broadcasts `nex:reload` → all clients reload the page |

`app.info()` response:

```json
{
  "name":    "my-app",
  "version": "1.0.0",
  "build":   "B001",
  "author":  "Mario Rossi",
  "public":  { "VITE_APP_TITLE": "My App" }
}
```

App fields come from `.env` (read at startup) via `Config.Name/Version/Build/Author`.
For framework identity use `framework.info()`.

---

### os and env

Every `os.*` value is gathered with pure Go — the stdlib `syscall` package
(`Uname`, `Sysctl`/`SysctlUint32`/`SysctlUint64`, `Statfs`/`Getfsstat` on
darwin/linux; registry + `kernel32.dll` via `syscall.NewLazyDLL` on windows),
`/proc`, `/etc/os-release`, or `SystemVersion.plist` — never a subprocess.
Implementation: `internal/system/osinfo_linux.go` / `osinfo_darwin.go` /
`osinfo_windows.go`.

Two darwin data points can't be obtained safely without cgo or an unverified
raw-struct decode, and are reported as `{ supported: false }` instead of a
guessed value: `os.host().uptime` (boot time; `kern.boottime` is a binary
`timeval`, not a string-typed sysctl) and the `vmStat` sub-field of
`os.memory()` (page-in/out counters need the Mach `host_statistics64` API).

| Helper | RPC | Returns |
|---|---|---|
| `os.info()` | `sys.os.info` | Full snapshot: host + user + runtime + network + disks + memory + time |
| `os.host()` | `sys.os.host` | OS, arch, hostname, compiler, kernel, platform, CPU model, uptime, **osVersion** |
| `os.user()` | `sys.os.user` | uid, gid, username, display name, home dir, groups |
| `os.runtime()` | `sys.os.runtime` | goroutines, GOMAXPROCS, allocator stats, build info |
| `os.process()` | `sys.os.process` | PID, PPID, executable, cwd, args, page size |
| `os.network()` | `sys.os.network` | Network interfaces with addresses, MTU, flags |
| `os.disks()` | `sys.os.disks` | Mounted volumes with size / used / available |
| `os.memory()` | `sys.os.memory` | Go heap stats + system memory |
| `os.time()` | `sys.os.time` | Local/UTC time, unix timestamp, timezone, offset |
| `env.paths()` | `sys.env.paths` | `{ home, config, cache, temp, exe, cwd }` |
| `env.get(key)` | `sys.env.get` | `{ key, value, found }` |
| `env.list(prefix?)` | `sys.env.list` | `{ vars: [{key,value}], count }` — optional prefix filter |

---

### http

| Helper | RPC | Returns |
|---|---|---|
| `http.fetch(url, opts?)` | `sys.http.fetch` | `{ ok, status, statusText, headers, body, encoding, url, redirected }` |

Params: `{ url, method?, headers?, body?, timeoutMs?, encoding?, skipVerify? }`.
Default encoding: `"utf8"`. Use `"base64"` for binary responses. Default timeout: 30 s.

```js
const res = await http.fetch("https://api.example.com/data", {
  method: "POST",
  headers: { "Authorization": "Bearer secret" },
  body: JSON.stringify({ id: 1 }),
});
console.log(res.status, JSON.parse(res.body));
```

---

### log

| Helper | RPC | Effect |
|---|---|---|
| `logger.print(msg)` | `sys.log.print` | Write to backend log (no prefix) |
| `logger.trace(msg)` | `sys.log.trace` | Write `[trace]` entry |
| `logger.debug(msg)` | `sys.log.debug` | Write `[debug]` entry |
| `logger.info(msg)` | `sys.log.info` | Write `[info]` entry |
| `logger.warning(msg)` | `sys.log.warning` | Write `[warning]` entry |
| `logger.error(msg)` | `sys.log.error` | Write `[error]` entry |

```js
window.addEventListener("error", (e) => {
  logger.error(`${e.message} @ ${e.filename}:${e.lineno}`);
});
```

---

### shell

| Helper | Params | Returns |
|---|---|---|
| `shell.exec(cmd, opts?)` | `{ command, cwd?, env?, timeoutMs? }` | `{ ok, exitCode, stdout, stderr, timedOut, durationMs }` |
| `shell.run(cmd, opts?)` | same | same |
| `shell.start(cmd, opts?)` | `{ command, cwd?, env? }` | `{ ok, pid }` |

Shell: `/bin/sh -c` on Unix, `%COMSPEC% /C` on Windows. Default timeout: 30 s. Max: 10 min.

---

### fs

| Helper | RPC | Returns |
|---|---|---|
| `fs.read(path, enc?)` | `sys.fs.read` | `{ data, encoding }` — enc: `utf8` or `base64` |
| `fs.write(path, data, enc?)` | `sys.fs.write` | `{ ok, bytes }` |
| `fs.list(path)` | `sys.fs.list` | `{ entries: [{name, isDir, size}] }` |
| `fs.exists(path)` | `sys.fs.exists` | `{ exists, isDir? }` |
| `fs.stat(path)` | `sys.fs.stat` | `{ name, size, isDir, modTime }` |
| `fs.mkdir(path)` | `sys.fs.mkdir` | `{ ok }` — creates recursively |
| `fs.remove(path, rec?)` | `sys.fs.remove` | `{ ok }` |
| `fs.rename(from, to)` | `sys.fs.rename` | `{ ok }` |
| `fs.copy(src, dst, ow?)` | `sys.fs.copy` | `{ ok, src, dst }` — overwrite: false by default |
| `fs.watch(path, opts?)` | `sys.fs.watch` | `{ ok, id, path, intervalMs }` — emits `nex:fs.changed` |
| `fs.unwatch(id)` | `sys.fs.unwatch` | `{ ok, stopped, id }` |
| `fs.glob(pattern)` | `sys.fs.glob` | `{ matches: string[], count }` — shell glob via `filepath.Glob` |
| `fs.abs(path)` | `sys.fs.abs` | `{ path }` — absolute form, server CWD-relative |
| `fs.temp(opts?)` | `sys.fs.temp` | `{ path, isDir }` — opts: `{ dir?, prefix?, isDir? }` |

`nex:fs.changed` payload: `{ id, path, event, size?, modTime? }` — event is `"created"`, `"modified"`, or `"deleted"`.

If `Config.FSRoot` is set, all path arguments are validated against it using
`filepath.EvalSymlinks` + prefix check. Paths outside the root return an error.

```js
const { matches } = await fs.glob("/etc/*.conf");
const { path }   = await fs.abs("./data");
const tmp        = await fs.temp({ prefix: "report-", isDir: true });
await fs.write(tmp.path + "/output.txt", "done");
```

---

### kv

Persistent JSON key-value store backed by `.nex-kv.json` in the working directory.
Thread-safe. Values can be any JSON type.

| Helper | RPC | Returns |
|---|---|---|
| `kv.get(key)` | `sys.kv.get` | `{ key, value, found }` |
| `kv.set(key, value)` | `sys.kv.set` | `{ ok, key }` |
| `kv.delete(key)` | `sys.kv.delete` | `{ ok, key, deleted }` |
| `kv.list()` | `sys.kv.list` | `{ entries: [{key, value}], count }` |
| `kv.clear()` | `sys.kv.clear` | `{ ok, cleared }` — removes **all** entries |

```js
await kv.set("app.theme", "dark");
const { value }   = await kv.get("app.theme"); // "dark"
const { entries } = await kv.list();
await kv.clear(); // { ok: true, cleared: N }
```

---

### net and proc

| Helper | RPC | Returns |
|---|---|---|
| `netutil.resolve(host)` | `sys.net.resolve` | `{ host, addresses: string[], cname }` |
| `netutil.port({host,port,timeoutMs?})` | `sys.net.port` | `{ open, host, port, addr }` |
| `proc.list()` | `sys.proc.list` | `{ processes: [{pid, ppid, user, name}], count, supported }` — pure Go via `/proc` (linux) / `CreateToolhelp32Snapshot` (windows). `supported: false` and an empty list on darwin: enumerating the process table without a subprocess requires decoding the undocumented, version-sensitive `kinfo_proc` sysctl struct, not attempted without cgo or a way to verify it. |

```js
const { addresses } = await netutil.resolve("google.com");
const { open }      = await netutil.port({ host: "localhost", port: 5432 });
const { processes } = await proc.list();
```

---

### Custom RPC

```go
a.Handle("data.fetch", func(c *nexweb.Context, params json.RawMessage) (any, error) {
    var p struct {
        ID int `json:"id"`
    }
    if err := c.Bind(params, &p); err != nil {
        return nil, nexweb.Errorf("bad_request", "%v", err)
    }
    return map[string]any{"id": p.ID, "data": "..."}, nil
})
```

```js
const row = await call("data.fetch", { id: 42 });
```

Context fields inside handlers:

| Field / Method | Purpose |
|---|---|
| `c.Bind(params, &v)` | Decode JSON params into a struct |
| `c.Authorize(decision)` | Run configured security policy/hooks for custom sensitive handlers |
| `c.Emit(event, payload)` | Broadcast SSE event to all connected clients |
| `c.OnMain(fn)` | Calls `fn()` directly (no UI thread constraint in web variant) |
| `c.Quit()` | Gracefully shut down the server |
| `c.Session` | Current session `{ ID, ExpiresAt }` |
| `c.Request` | Raw `*http.Request` |
| `c.Ctx` | Request `context.Context` |

Custom handler authorization example:

```go
a.Handle("admin.reindex", func(c *nexweb.Context, params json.RawMessage) (any, error) {
    if err := c.Authorize(nexweb.SecurityDecision{
        Category: "app",
        Operation: "reindex",
        Params: params,
    }); err != nil {
        return nil, err
    }
    return map[string]any{"ok": true}, nil
})
```

---

### security

| Helper | RPC | Returns |
|---|---|---|
| `security.capabilities()` | `sys.security.capabilities` | Available sensitive API surfaces, shell/process capability, and FS jail status |

---

## Differences from nex desktop

| Feature | nex (desktop) | nex-web |
|---|---|---|
| CGO | Required (webview) | Not required |
| Native webview | Yes — WebView2 / WebKit / WebKitGTK | No — browser |
| Window APIs | setTitle, setSize | Not available |
| Native dialogs | open / save / message | Not available |
| OS notifications | toast | Not available |
| shell.openURL / openPath | Yes | Not available (use `window.open()`) |
| Theme | system / light / dark | system / light / dark |
| Backend→frontend | webview JS eval | SSE EventSource |
| Daemon mode | No | Yes — `internal/daemon` |
| Public proxy port | No (random loopback) | Yes — `:3000` |
| Cross-compile | Host-native only (CGO) | Any host → any target |
| KV store | Not available | `kv.get/set/delete/list/clear` |
| DNS resolve | Not available | `netutil.resolve(host)` |
| Port check | Not available | `netutil.port({host,port})` |
| Process list | Not available | `proc.list()` |
| Env list | Not available | `env.list(prefix?)` |
| Browser reload | Not available | `app.reload()` |
| HTTP proxy | Not available | `http.fetch(url, opts?)` — server-side, bypasses CORS |
| Filesystem | read, write, list, exists, stat, mkdir, remove, rename | Same + copy, watch/unwatch, glob, abs, temp |
| OS info APIs | host, user, runtime, process, network, disks, memory, time | Same |
| Shell APIs | exec, run, start | Same |
| Frontend logging | sys.log.* | Same |

`os.*`, `shell.*`, and basic `fs.*` calls are portable between both variants.

---

## License

nex-web is distributed under the **nex Framework Source-Available License** —
Copyright © 2026 Veronesi Lorenzo (vlT).

The framework may be read, studied, forked, modified, and redistributed as a
standalone framework under the terms in [LICENSE](LICENSE). Incorporating,
embedding, vendoring, bundling, or otherwise using nex-web inside another
project, product, service, template, SDK, framework, or distributed software
requires prior express written consent from Veronesi Lorenzo (vlT).

For licensing inquiries: veronesilorenzo@outlook.com
