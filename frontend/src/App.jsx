import { useEffect, useMemo, useRef, useState } from "react";
import * as nex from "./lib/nex.js";
import docsRaw from "../../DOCS.md?raw";
import licenseRaw from "../../LICENSE?raw";
import { renderMarkdown } from "./lib/markdown.js";
import { NexWebLogo } from "./components/nexweblogo.jsx";
import {
  applyTheme,
  getThemePreference,
  resolveTheme,
  setThemePreference,
  watchSystemTheme,
} from "./lib/theme.js";
import {
  LuBookOpen, LuCode, LuCpu, LuDatabase, LuExternalLink, LuFileText,
  LuFolder, LuGlobe, LuInfo, LuLayers, LuList, LuMonitor,
  LuPackage, LuPlay, LuServer, LuSettings, LuShield, LuTerminal,
  LuTrash2, LuWifi, LuX, LuZap,
} from "react-icons/lu";
import { FiMoon, FiMonitor, FiSun } from "react-icons/fi";

// ── stack badges (framework intro) ───────────────────────────────────────────

// Versions injected by Vite at build time from package.json — never stale.
/* global __PKG_REACT__, __PKG_REACT_ICONS__, __PKG_VITE__ */
const stackBadges = [
  { label: "nex-web",     tone: "blue",   resolve: (_, __, fw) => fw ? `${fw.version}${fw.build ? `-${fw.build}` : ""}` : "…" },
  { label: "go",          tone: "cyan",   resolve: (_, osd)    => osd?.runtime?.goVersion?.replace(/^go/, "") ?? "…" },
  { label: "react",       tone: "sky",    value: __PKG_REACT__ },
  { label: "react-icons", tone: "sky",    value: __PKG_REACT_ICONS__ },
  { label: "vite",        tone: "violet", value: __PKG_VITE__ },
];

// ── FileModal (docs + license) ───────────────────────────────────────────────

const docsFile = {
  title: "DOCS.md",
  description: "Complete nex-web framework manual.",
  content: docsRaw,
};

const licenseFile = {
  title: "LICENSE",
  description: "nex Framework Source-Available License.",
  content: licenseRaw,
};

function FileModal({ open, onClose, onLog, file, kicker, format = "markdown" }) {
  const renderedContent = useMemo(
    () => (format === "markdown" ? renderMarkdown(file.content) : null),
    [file.content, format]
  );
  const contentRef = useRef(null);
  const titleId = `${file.title}-modal-title`;

  useEffect(() => {
    if (!open) return undefined;
    const onKeyDown = (e) => { if (e.key === "Escape") onClose(); };
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open, onClose]);

  function openAsFile() {
    const mime = format === "markdown" ? "text/markdown;charset=utf-8" : "text/plain;charset=utf-8";
    const blob = new Blob([file.content], { type: mime });
    const url = URL.createObjectURL(blob);
    const opened = window.open(url, "_blank");
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    if (opened) {
      onLog?.(`opened ${file.title}`, "ok");
    } else {
      onLog?.(`popup blocked for ${file.title}`, "err");
    }
  }

  function onContentClick(e) {
    if (format !== "markdown") return;
    const link = e.target.closest?.("a[href]");
    if (!link || !contentRef.current?.contains(link)) return;
    const href = link.getAttribute("href") || "";
    if (href.startsWith("#")) {
      e.preventDefault();
      const target = Array.from(contentRef.current.querySelectorAll("[id]"))
        .find((node) => node.id === href.slice(1));
      target?.scrollIntoView({ block: "start" });
      return;
    }
    if (/^(https?:|mailto:)/.test(href)) {
      e.preventDefault();
      window.open(href, "_blank");
    }
  }

  if (!open) return null;

  return (
    <div aria-labelledby={titleId} aria-modal="true" className="modal-layer" role="dialog">
      <button aria-label={`close ${file.title}`} className="modal-backdrop" onClick={onClose} type="button" />
      <div className="docs-modal">
        <div className="docs-head">
          <div>
            <span className="docs-kicker">{kicker}</span>
            <h2 id={titleId}>{file.title}</h2>
            <p>{file.description}</p>
          </div>
          <button className="modal-close" onClick={onClose} type="button" aria-label="close">
            <LuX size={16} aria-hidden="true" />
          </button>
        </div>
        <div className="docs-actions">
          <span className="docs-file">
            <LuFileText size={12} aria-hidden="true" />
            {file.title}
          </span>
          <button className="docs-open" onClick={openAsFile} type="button">
            <LuExternalLink size={12} aria-hidden="true" />
            <span>open file</span>
          </button>
        </div>
        {format === "markdown" ? (
          <div
            className="docs-content"
            onClick={onContentClick}
            ref={contentRef}
            dangerouslySetInnerHTML={{ __html: renderedContent }}
          />
        ) : (
          <div className="docs-content" ref={contentRef}>
            <pre><code>{file.content}</code></pre>
          </div>
        )}
      </div>
    </div>
  );
}

// ── sub-components ────────────────────────────────────────────────────────────

function ThemeSwitch({ preference, theme, onChange }) {
  const modes = [
    { id: "system", label: "system", icon: FiMonitor },
    { id: "light", label: "light", icon: FiSun },
    { id: "dark", label: "dark", icon: FiMoon },
  ];
  return (
    <div className="theme-switch" role="group" aria-label="color mode">
      {modes.map((mode) => {
        const Icon = mode.icon;
        return (
          <button
            className={`theme-option${preference === mode.id ? " active" : ""}`}
            key={mode.id}
            onClick={() => onChange(mode.id)}
            title={mode.id === "system" ? `system (${theme})` : mode.label}
            type="button"
          >
            <Icon aria-hidden="true" className="ui-icon" />
            <span>{mode.label}</span>
          </button>
        );
      })}
    </div>
  );
}

function StatusBar({ info, sess, beat, theme, themePreference, onThemeChange, onOpenDocs, onOpenLicense }) {
  return (
    <header className="topbar">
      <div className="brand">
        <span className="brand-name">{info?.name ?? "nex-web"}</span>
        <span className="brand-ver">v{info?.version ?? "—"}</span>
      </div>
      <div className="top-actions">
        <ThemeSwitch preference={themePreference} theme={theme} onChange={onThemeChange} />
        <button className="docs-trigger" onClick={onOpenDocs}><LuBookOpen size={12} /> docs</button>
        <button className="docs-trigger" onClick={onOpenLicense}><LuShield size={12} /> license</button>
        <div className="bridge">
          <span>{sess ? `session ${sess.id.slice(0, 8)}` : "connecting…"}</span>
          {/* key={beat} restarts CSS animation on every tick */}
          <span
            key={beat}
            className={`dot${sess ? " online" : ""}${beat > 0 ? " beat" : ""}`}
            title="backend ⇄ frontend bridge (SSE)"
          />
        </div>
      </div>
    </header>
  );
}

function HttpFetchRow({ onRun }) {
  const [url, setUrl] = useState("https://httpbin.org/json");
  const [skipVerify, setSkipVerify] = useState(false);
  return (
    <div className="api-row api-row-fetch">
      <span className="api-sig">http.fetch(url, opts?)</span>
      <span className="api-desc">Server-side HTTP/HTTPS request — bypasses CORS. skipVerify per cert self-signed.</span>
      <input
        className="fetch-url-input"
        type="text"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://..."
        spellCheck={false}
      />
      <label className="fetch-skip-verify">
        <input
          type="checkbox"
          checked={skipVerify}
          onChange={(e) => setSkipVerify(e.target.checked)}
        />
        skipVerify
      </label>
      <button className="btn" onClick={() => onRun("sys.http.fetch", () => nex.http.fetch(url, { skipVerify }))}>
        <LuPlay size={11} /> Run
      </button>
    </div>
  );
}

const NS_ICONS = {
  framework: LuLayers,
  app:    LuInfo,
  os:     LuMonitor,
  http:   LuGlobe,
  env:    LuSettings,
  log:    LuList,
  shell:  LuTerminal,
  fs:     LuFolder,
  kv:     LuDatabase,
  net:    LuWifi,
  proc:   LuCpu,
  security: LuShield,
};

function ApiExplorer({ demos, onRun }) {
  return (
    <div className="panel">
      {nex.apiCatalog.map((group) => {
        const Icon = NS_ICONS[group.namespace];
        return (
          <div className="group" key={group.namespace}>
            <div className="group-head">{Icon && <Icon size={12} />}{group.namespace}</div>
            {group.methods.map((m) => {
              const level = nex.apiRiskLevel(m.id);
              return m.id === "sys.http.fetch" ? (
                <HttpFetchRow key={m.id} onRun={onRun} />
              ) : (
                <div className="api-row" key={m.id}>
                  <span className="api-sig">{m.sig}</span>
                  <span className={`api-risk ${level}`}>{level}</span>
                  <span className="api-desc">{m.desc}</span>
                  {demos[m.id] ? (
                    <button className="btn" onClick={() => onRun(m.id, demos[m.id])}>
                      <LuPlay size={11} /> Run
                    </button>
                  ) : (
                    <button className="btn" disabled>—</button>
                  )}
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

const CONSOLE_HEIGHT_KEY = "nex-console-height";
const CONSOLE_MIN_HEIGHT = 118;
const CONSOLE_DEFAULT_HEIGHT = 156;

function clampConsoleHeight(value) {
  const viewportHeight = typeof window === "undefined" ? 760 : window.innerHeight;
  const max = Math.max(CONSOLE_MIN_HEIGHT, Math.floor(viewportHeight * 0.72));
  return Math.min(max, Math.max(CONSOLE_MIN_HEIGHT, value));
}

function initialConsoleHeight() {
  try {
    const saved = Number(localStorage.getItem(CONSOLE_HEIGHT_KEY));
    if (Number.isFinite(saved) && saved > 0) return clampConsoleHeight(saved);
  } catch {
    // Keep default height when storage is unavailable.
  }
  return clampConsoleHeight(CONSOLE_DEFAULT_HEIGHT);
}

function Console({ lines, onClear }) {
  const consoleRef = useRef(null);
  const dragRef = useRef(null);
  const [height, setHeight] = useState(initialConsoleHeight);

  useEffect(() => {
    if (lines.length === 0 || !consoleRef.current) return;
    consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
  }, [lines]);

  useEffect(() => {
    try {
      localStorage.setItem(CONSOLE_HEIGHT_KEY, String(height));
    } catch {
      // Height persistence is optional.
    }
  }, [height]);

  useEffect(() => {
    function onResize() {
      setHeight((current) => clampConsoleHeight(current));
    }
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  function startResize(e) {
    e.preventDefault();
    const startY = e.clientY;
    const startHeight = height;
    dragRef.current?.setPointerCapture?.(e.pointerId);

    function onMove(moveEvent) {
      const nextHeight = startHeight + (startY - moveEvent.clientY);
      setHeight(clampConsoleHeight(nextHeight));
    }

    function onUp() {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    }

    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
  }

  function setPreset(nextHeight) {
    setHeight(clampConsoleHeight(nextHeight));
  }

  return (
    <div className="terminal">
      <button
        className="terminal-resize"
        onPointerDown={startResize}
        ref={dragRef}
        title="resize console"
        type="button"
      />
      <div className="terminal-bar">
        <span className="terminal-dots" aria-hidden="true">
          <span className="terminal-dot red" />
          <span className="terminal-dot yellow" />
          <span className="terminal-dot green" />
        </span>
        <span className="terminal-label"><LuTerminal size={12} /> nex-web console</span>
        <div className="terminal-size-actions" aria-label="console size">
          <button type="button" onClick={() => setPreset(156)}>fit</button>
          <button type="button" onClick={() => setPreset(280)}>tall</button>
          <button type="button" onClick={() => setPreset(window.innerHeight * 0.72)}>max</button>
        </div>
        <button className="terminal-clear" onClick={onClear} disabled={lines.length === 0}>
          <LuTrash2 size={11} /> clear
        </button>
      </div>
      <div className="console terminal-pre" ref={consoleRef} style={{ height }}>
        {lines.length === 0 ? (
          <div className="empty">(nessun output)</div>
        ) : (
          lines.map((l, i) => (
            <div className="log-line" key={i}>
              <span className="ts">{l.time}  </span>
              <span className={l.kind === "err" ? "err-text" : l.kind === "ok" ? "ok-text" : ""}>
                {l.text}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── main app ──────────────────────────────────────────────────────────────────

export default function App() {
  const [info, setInfo] = useState(null);
  const [frameworkInfo, setFrameworkInfo] = useState(null);
  const [osData, setOsData] = useState(null);
  const [sess, setSess] = useState(null);
  const [beat, setBeat] = useState(0);
  const [lines, setLines] = useState([]);
  const [themePreference, setThemePreferenceState] = useState(getThemePreference);
  const [theme, setTheme] = useState(() => resolveTheme(getThemePreference()));
  const [docsOpen, setDocsOpen] = useState(false);
  const [licenseOpen, setLicenseOpen] = useState(false);

  function log(text, kind = "info") {
    setLines((prev) =>
      [...prev, { time: new Date().toLocaleTimeString(), text, kind }].slice(-200)
    );
  }

  useEffect(() => {
    nex.app.info().then(setInfo).catch((e) => log(`app.info: ${e.message}`, "err"));
    nex.framework.info().then(setFrameworkInfo).catch(() => {});
    nex.os.info().then(setOsData).catch(() => {});
    nex.session().then(setSess).catch(() => {});

    const offTick = nex.on("tick", () => setBeat((b) => b + 1));

    // SSE disconnect/reconnect — survives dev-server restarts
    const offDisconnected = nex.on("disconnected", () => setSess(null));
    const offConnected = nex.on("connected", () => {
      nex.session().then(setSess).catch(() => {});
      nex.app.info().then(setInfo).catch(() => {});
      nex.framework.info().then(setFrameworkInfo).catch(() => {});
    });

    // sys.app.reload → full page reload triggered from backend
    const offReload = nex.on("reload", () => window.location.reload());

    return () => { offTick(); offDisconnected(); offConnected(); offReload(); };
  }, []);

  useEffect(() => {
    const applied = applyTheme(themePreference);
    setTheme(applied.theme);
    if (themePreference !== "system") return undefined;
    return watchSystemTheme(() => {
      const next = applyTheme("system");
      setTheme(next.theme);
    });
  }, [themePreference]);

  // Apply title and favicon from .env (VITE_APP_TITLE, VITE_APP_ICON).
  // Both are read at runtime from app.info().public so no rebuild is needed.
  useEffect(() => {
    if (!info) return;
    const pub = info.public ?? {};
    document.title = pub.VITE_APP_TITLE ?? info.name ?? "nex-web";
    const iconHref = pub.VITE_APP_ICON;
    if (iconHref) {
      let link = document.querySelector("link[rel='icon']");
      if (!link) {
        link = document.createElement("link");
        link.rel = "icon";
        document.head.appendChild(link);
      }
      link.href = iconHref;
    }
  }, [info]);

  function changeThemePreference(nextPreference) {
    const applied = setThemePreference(nextPreference);
    setThemePreferenceState(applied.preference);
    setTheme(applied.theme);
  }

  // Interactive demos — covers all APIs; destructive ones use temp paths or reversible ops
  const demos = useMemo(
    () => ({
      // framework
      "sys.framework.info":  () => nex.framework.info(),
      "sys.framework.stack": () => nex.framework.stack(),

      // app
      "sys.app.info":   () => nex.app.info(),
      "sys.app.reload": () => nex.app.reload(),

      // os
      "sys.os.info":    () => nex.os.info(),
      "sys.os.host":    () => nex.os.host(),
      "sys.os.user":    () => nex.os.user(),
      "sys.os.runtime": () => nex.os.runtime(),
      "sys.os.process": () => nex.os.process(),
      "sys.os.network": () => nex.os.network(),
      "sys.os.disks":   () => nex.os.disks(),
      "sys.os.memory":  () => nex.os.memory(),
      "sys.os.time":    () => nex.os.time(),

      // env
      "sys.env.paths": () => nex.env.paths(),
      "sys.env.get":   () => nex.env.get(osData?.os === "windows" ? "USERPROFILE" : "HOME"),
      "sys.env.list":  () => nex.env.list("VITE_"),

      // log — all levels, each writes to backend terminal
      "sys.log.print":   () => nex.logger.print("nex-web demo: plain log"),
      "sys.log.trace":   () => nex.logger.trace("nex-web demo: trace"),
      "sys.log.debug":   () => nex.logger.debug("nex-web demo: debug"),
      "sys.log.info":    () => nex.logger.info("nex-web demo: info"),
      "sys.log.warning": () => nex.logger.warning("nex-web demo: warning"),
      "sys.log.error":   () => nex.logger.error("nex-web demo: error"),

      // shell
      "sys.shell.exec":  () =>
        nex.shell.exec(osData?.os === "windows" ? "ver" : "uname -a", { timeoutMs: 5000 }),
      "sys.shell.run":   () =>
        nex.shell.run(osData?.os === "windows" ? "echo %USERNAME%" : "whoami"),
      "sys.shell.start": () =>
        nex.shell.start(osData?.os === "windows" ? "timeout /T 2 /NOBREAK" : "sleep 2"),

      // fs — read-only
      "sys.fs.list":   () => nex.fs.list(osData?.home ?? "/tmp"),
      "sys.fs.exists": () => nex.fs.exists(osData?.home ?? "/tmp"),
      "sys.fs.stat":   () => nex.fs.stat(osData?.home ?? "/tmp"),
      "sys.fs.read":   () => nex.fs.read(
        osData?.os === "windows"
          ? "C:\\Windows\\System32\\drivers\\etc\\hosts"
          : "/etc/hosts"
      ),
      "sys.fs.abs":    () => nex.fs.abs("."),
      "sys.fs.glob":   () => nex.fs.glob((osData?.home ?? "/tmp") + "/*"),

      // fs — write to temp (safe, isolated)
      "sys.fs.temp":   () => nex.fs.temp({ prefix: "nex-demo-", isDir: false }),
      "sys.fs.mkdir":  () => nex.fs.mkdir((osData?.paths?.temp ?? "/tmp") + "/nex-demo"),
      "sys.fs.write":  () => nex.fs.write(
        (osData?.paths?.temp ?? "/tmp") + "/nex-demo.txt",
        "hello from nex-web\n"
      ),
      "sys.fs.copy":   () => nex.fs.copy(
        (osData?.paths?.temp ?? "/tmp") + "/nex-demo.txt",
        (osData?.paths?.temp ?? "/tmp") + "/nex-demo-copy.txt",
        true
      ),
      "sys.fs.watch":  () => nex.fs.watch(osData?.home ?? "/tmp", { intervalMs: 2000 }),

      // kv
      "sys.kv.set":    () => nex.kv.set("nex-web.demo", { ts: Date.now(), msg: "hello from UI" }),
      "sys.kv.get":    () => nex.kv.get("nex-web.demo"),
      "sys.kv.list":   () => nex.kv.list(),
      "sys.kv.delete": () => nex.kv.delete("nex-web.demo"),
      "sys.kv.clear":  () => nex.kv.clear(),

      // http — HttpFetchRow handles sys.http.fetch inline

      // net
      "sys.net.resolve": () => nex.netutil.resolve("google.com"),
      "sys.net.port":    () => nex.netutil.port({ host: "google.com", port: 443 }),

      // proc
      "sys.proc.list": () => nex.proc.list(),

      // security
      "sys.security.capabilities": () => nex.security.capabilities(),
    }),
    [osData]
  );

  async function onRun(id, fn) {
    const risk = nex.apiRiskLevel(id);
    if (risk === "privileged" && !window.confirm(`${id} is a privileged server-side API. Continue?`)) {
      log(`cancelled ${id}`);
      return;
    }
    log(`→ ${id}`);
    try {
      const r = await fn();
      const s = JSON.stringify(r, null, 2);
      log(s, "ok");
    } catch (e) {
      log(`✗ ${e.message}`, "err");
    }
  }

  const pub = info?.public ?? {};

  return (
    <>
      <StatusBar
        info={info}
        sess={sess}
        beat={beat}
        theme={theme}
        themePreference={themePreference}
        onThemeChange={changeThemePreference}
        onOpenDocs={() => setDocsOpen(true)}
        onOpenLicense={() => setLicenseOpen(true)}
      />
      <FileModal
        open={docsOpen}
        onClose={() => setDocsOpen(false)}
        onLog={log}
        file={docsFile}
        kicker="documentation"
        format="markdown"
      />
      <FileModal
        open={licenseOpen}
        onClose={() => setLicenseOpen(false)}
        onLog={log}
        file={licenseFile}
        kicker="license"
        format="text"
      />
      <main className="wrap">
        <NexWebLogo />

        {/* Framework intro */}
        <section className="section">
          <div className="framework-note">
            <div>
              <span className="framework-eyebrow">framework</span>
              <h2>nex-web turns React, Vite, and Go into a single self-contained web server.</h2>
              <div className="framework-badges" aria-label="framework stack versions">
                {stackBadges.map((badge) => {
                  const value = badge.value ?? badge.resolve?.(info, osData, frameworkInfo) ?? "…";
                  return (
                    <span className={`stack-badge ${badge.tone}`} key={badge.label}>
                      <span className="stack-badge-label">{badge.label}</span>
                      <span className="stack-badge-value">{value}</span>
                    </span>
                  );
                })}
              </div>
            </div>
            <div className="framework-points">
              <span><LuPackage size={12} /> Embedded frontend assets</span>
              <span><LuShield size={12} /> Secure RPC bridge</span>
              <span><LuZap size={12} /> Server-Sent Events (SSE)</span>
              <span><LuServer size={12} /> Detached daemon · port 3000</span>
              <span><LuCode size={12} /> Zero CGO — pure Go</span>
            </div>
          </div>
        </section>

        {/* App + framework info */}
        <section className="section">
          <span className="label"><LuInfo size={13} /> App</span>
          <div className="panel">
            <div className="kv">
              <div className="k">name</div>
              <div>{info?.name ?? "…"}</div>
              <div className="k">version</div>
              <div>{info?.version ?? "…"}</div>
              <div className="k">build</div>
              <div>{info?.build || "—"}</div>
              <div className="k">author</div>
              <div>{info?.author || "—"}</div>
              <div className="k">platform</div>
              <div>{osData ? `${osData.os}/${osData.arch} · ${osData.cpus} cpu` : "…"}</div>
            </div>
          </div>
        </section>

        <section className="section">
          <span className="label"><LuLayers size={13} /> Framework</span>
          <div className="panel">
            <div className="kv">
              <div className="k">framework</div>
              <div>{frameworkInfo ? `${frameworkInfo.name}@${frameworkInfo.version}` : "…"}</div>
              <div className="k">build · updated</div>
              <div>{frameworkInfo ? `${frameworkInfo.build} · ${frameworkInfo.updated}` : "…"}</div>
              <div className="k">author</div>
              <div>{frameworkInfo?.author ?? "…"}</div>
            </div>
          </div>
        </section>

        {/* Public env vars */}
        <section className="section">
          <span className="label"><LuSettings size={13} /> Public env (VITE_*)</span>
          <div className="panel">
            <div className="env-grid">
              {Object.keys(pub).length === 0 ? (
                <div className="env-row">
                  <span className="env-val">No public variables loaded.</span>
                </div>
              ) : (
                Object.entries(pub).map(([k, v]) => (
                  <div className="env-row" key={k}>
                    <span className="env-key">{k}</span>
                    <span className="env-val">{v}</span>
                  </div>
                ))
              )}
            </div>
            <div className="note">
              Variables without the VITE_ prefix stay backend-only and never reach the frontend.
            </div>
          </div>
        </section>

        {/* API explorer */}
        <section className="section">
          <span className="label"><LuCode size={13} /> Available APIs</span>
          <ApiExplorer demos={demos} onRun={onRun} />
        </section>

        {/* Console */}
        <section className="section console-dock">
          <Console lines={lines} onClear={() => setLines([])} />
        </section>

      </main>
    </>
  );
}
