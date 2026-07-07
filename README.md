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

nex-web builds a single self-contained binary that embeds a React/Vite frontend and exposes a typed Go RPC backend accessible from any browser. No CGO, no native webview, no platform toolchain. Any `go build` produces a binary that runs on macOS, Linux, and Windows.

In release mode the binary daemonizes itself and exposes the app on **port 3000**. The backend runs on a random internal port for unpredictability. The public port is also the web variant's instance lock: if it is already bound, startup fails instead of leaving an unreachable backend process running.

```
Browser → http://localhost:3000      (stable public proxy)
                 ↓
Go backend   http://127.0.0.1:<rand> (embedded Vite SPA + RPC + SSE)
```

→ Full architecture, API reference and internals in **[DOCS.md](DOCS.md)**

---

## Project rules

Every framework change must update **README.md**, **DOCS.md**, and **handoff.md**
in the same pass, so the public entry point, full manual, and working handoff stay aligned.

---

## Requirements

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26.4+ | CGO not required |
| Node | 18+ with npm | Frontend build only |
| garble | auto-installed | `build.go` installs it when missing |

```bash
make requirements   # validate all tools
```

---

## Quick start

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
        Name:       "my-app",   // your app name
        Version:    "1.0.0",    // your app version
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

Frontend:

```js
import { call, on, os, shell, fs, kv, netutil, http, env, proc, app, framework, logger, security } from "./lib/nex.js";

const info    = await app.info();          // { name, version, build, author, public }
const fw      = await framework.info();    // { name, version, build, updated, author, stack }
const host    = await os.host();
const result  = await shell.exec("uname -a");
const list    = await fs.list("/tmp");
const { open} = await netutil.port({ host: "localhost", port: 5432 });

await kv.set("last-run", Date.now());
const caps = await security.capabilities();
const off = on("tick", ({ n }) => console.log(n));
```

---

## Frontend logo

The showcase UI uses `frontend/src/components/nexweblogo.jsx`, a dedicated React
component adapted from the desktop framework logo component. It imports
`res/CenturyGothic.ttf` via Vite (`?url`), injects the `@font-face`, renders the
cube/wordmark/suffix with the `web` suffix centered by the component CSS. The
README/DOCS header uses a separate SVG asset for documentation rendering; the
desktop-style entrance animation lives in `nexweblogo.css`.

The color-mode switch mirrors the desktop framework: `system`, `light`, and
`dark` are rendered as icon+label segmented buttons.

---

## Template docs & license modal

The frontend template includes `docs` and `license` buttons in the top bar,
both backed by a single generic `FileModal` component (`frontend/src/App.jsx`).
`docs` renders `DOCS.md` as sanitized markdown; `license` renders the repo-root
`LICENSE` as plain text. Both files are pulled in at build time via Vite's
`?raw` import (`import licenseText from "../../LICENSE?raw"`) — never
hardcoded — so the bundled text always matches the actual file on disk.

---

## Security hooks

Apps can add a `SecurityPolicy` or focused hooks (`OnShellCommand`,
`OnFileAccess`, `OnHTTPFetch`, `OnEnvAccess`, `OnProcessList`, `OnNetworkAccess`,
`OnKVAccess`, `OnAppControl`) to approve or deny sensitive RPC operations before
execution. The frontend can inspect base surfaces with `security.capabilities()`.

---

## Development

```bash
make dev          # Vite HMR + Go backend, no daemon, no port 3000
```

Hot reload for the frontend via Vite. Restart `make dev` for Go changes.

## Build

```bash
make build        # obfuscated binaries → release/<version>-<build>/
make release      # same + -s -w strip
make clean
```

Targets: `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64` — all from any host, `CGO_ENABLED=0`.

---

## License

nex-web is distributed under the **nex Framework Source-Available License** —
Copyright © 2026 Veronesi Lorenzo (vlT).

The framework may be read, studied, forked, modified, and redistributed as a
standalone framework under the terms in [LICENSE](LICENSE). Incorporating,
embedding, vendoring, bundling, or otherwise using nex-web inside another
project, product, service, template, SDK, framework, or distributed software
requires prior express written consent from Veronesi Lorenzo (vlT).

For licensing inquiries: [veronesilorenzo@outlook.com](mailto:veronesilorenzo@outlook.com)
