/**
 * nex-web client bridge — pure JavaScript, no TypeScript, no external deps.
 *
 * The Go backend serves /nex.js which opens an SSE stream using a one-time
 * nonce. The server delivers the real session token inside the first "connected"
 * SSE event payload and stores it in window.__NEX__.token — the token never
 * appears in any file or URL. This library reads window.__NEX__ and provides:
 *
 *   call(method, params?)         — RPC call to the Go backend
 *   on(event, handler)            — subscribe to backend events; returns unsubscribe fn
 *   session()                     — fetch current session info
 *
 *   os.info()                     — full OS snapshot (host + user + runtime + ...)
 *   os.host()                     — hostname, OS, arch, CPU, kernel, platform, uptime
 *   os.user()                     — current user (uid, gid, username, groups)
 *   os.runtime()                  — Go runtime stats, mem, build info
 *   os.process()                  — pid, ppid, exe, cwd, args
 *   os.network()                  — network interfaces
 *   os.disks()                    — disk volumes
 *   os.memory()                   — Go heap + system memory
 *   os.time()                     — local/UTC time, timezone
 *   env.paths()                   — home, config, cache, temp, exe, cwd
 *   shell.exec(command, opts?)    — run shell command, capture output
 *   shell.run(command, opts?)     — alias for shell.exec
 *   shell.start(command, opts?)   — start background process; returns PID
 *   fs.read(path, encoding?)      — read file (utf8 | base64)
 *   fs.write(path, data, enc?)    — write file
 *   fs.list(path)                 — list directory
 *   fs.exists(path)               — check existence
 *   fs.stat(path)                 — file metadata
 *   fs.mkdir(path)                — create directory (recursive)
 *   fs.remove(path, recursive?)   — remove file/directory
 *   fs.rename(from, to)           — rename/move
 *   app.info()                    — app metadata (name, version, build, author, public env)
 *   app.quit()                    — gracefully stop the web server
 *   app.reload()                  — reload all connected browser clients via SSE
 *   framework.info()              — nex-web framework identity (name, version, build, stack)
 *   framework.stack()             — resolved runtime stack (Go version, build settings, deps)
 *   log.print(message)            — write to backend log (no level prefix)
 *   log.trace/debug/info/warning/error(message) — structured frontend→backend logging
 *   http.fetch(url, opts?)        — server-side HTTP/HTTPS request (bypasses CORS)
 *   env.get(key)                  — read a specific backend environment variable
 *   fs.copy(src, dst, overwrite?) — copy file or directory
 *   fs.watch(path, opts?)         — poll a path; emits nex:fs.changed SSE events
 *   fs.unwatch(id)                — stop a watcher by id
 *   fs.glob(pattern)              — filepath.Glob — files matching a shell pattern
 *   fs.abs(path)                  — resolve path to absolute form (server CWD-relative)
 *   fs.temp(opts?)                — create a temp file or dir; returns its path
 *   kv.get(key)                   — read from persistent JSON KV store
 *   kv.set(key, value)            — write any JSON value to persistent KV store
 *   kv.delete(key)                — remove a key from KV store
 *   kv.list()                     — list all KV entries
 *   kv.clear()                    — delete all KV entries at once
 *   netutil.resolve(host)         — DNS lookup: IP addresses + CNAME
 *   netutil.port({host,port})     — TCP dial check: returns { open, host, port, addr }
 *   proc.list()                   — list running processes (pid, ppid, user, name);
 *                                    { supported: false } on platforms without a safe
 *                                    pure-Go implementation (currently: darwin)
 *   env.list(prefix?)             — list backend env vars, optional prefix filter
 *   security.capabilities()       — inspect available sensitive API surfaces
 */

const NEX = (typeof window !== "undefined" && window.__NEX__) || { token: "", base: "" };
const API_BASE = import.meta.env.VITE_API_BASE ?? "/api";

// ── Core RPC ─────────────────────────────────────────────────────────────────

export class nexError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
    this.name = "nexError";
  }
}

/**
 * Call a backend RPC method.
 * @param {string} method
 * @param {unknown} [params]
 * @returns {Promise<unknown>}
 */
export async function call(method, params = {}) {
  const res = await fetch(`${API_BASE}/rpc`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-nex-Token": NEX.token,
    },
    body: JSON.stringify({ method, params }),
  });
  if (!res.ok) throw new nexError(`http_${res.status}`, `HTTP ${res.status}`);
  const data = await res.json();
  if (data.error) throw new nexError(data.error.code, data.error.message);
  return data.result;
}

/**
 * Subscribe to a backend event emitted via nex.Emit() on the Go side.
 * Events arrive via the SSE stream set up by /nex.js.
 * @param {string} event
 * @param {(payload: unknown) => void} handler
 * @returns {() => void} unsubscribe
 */
export function on(event, handler) {
  const listener = (e) => handler(e.detail);
  window.addEventListener(`nex:${event}`, listener);
  return () => window.removeEventListener(`nex:${event}`, listener);
}

/** Fetch current session metadata. */
export async function session() {
  const res = await fetch(`${API_BASE}/session`, {
    headers: { "X-nex-Token": NEX.token },
  });
  if (!res.ok) throw new nexError(`http_${res.status}`, `HTTP ${res.status}`);
  return res.json();
}

// ── Namespaced helpers ────────────────────────────────────────────────────────

export const os = {
  /** Full OS snapshot: host + user + runtime + network + disks + memory + time. */
  info: () => call("sys.os.info"),
  /** Hostname, OS, arch, CPU, kernel, platform, uptime. */
  host: () => call("sys.os.host"),
  /** Current user: uid, gid, username, name, groups. */
  user: () => call("sys.os.user"),
  /** Go runtime stats, goroutines, mem, build info. */
  runtime: () => call("sys.os.runtime"),
  /** Process info: pid, ppid, exe, cwd, args. */
  process: () => call("sys.os.process"),
  /** Network interfaces with addresses. */
  network: () => call("sys.os.network"),
  /** Disk volumes with usage. */
  disks: () => call("sys.os.disks"),
  /** Go heap + system memory stats. */
  memory: () => call("sys.os.memory"),
  /** Current local/UTC time, timezone, unix timestamp. */
  time: () => call("sys.os.time"),
  /** Alias kept for compatibility. */
  paths: () => call("sys.env.paths"),
};

export const env = {
  /** Standard paths: home, config, cache, temp, exe, cwd. */
  paths: () => call("sys.env.paths"),
  /** Read a specific backend environment variable by name. */
  get: (key) => call("sys.env.get", { key }),
  /** List all backend env vars. Optional prefix filters by key prefix. */
  list: (prefix = "") => call("sys.env.list", { prefix }),
};

export const http = {
  /**
   * Server-side HTTP/HTTPS request. Bypasses browser CORS restrictions.
   * @param {string} url
   * @param {{ method?, headers?, body?, timeoutMs?, encoding?, skipVerify? }} [opts]
   *   skipVerify: true disables TLS certificate validation (HTTPS self-signed certs)
   */
  fetch: (url, opts = {}) => call("sys.http.fetch", { url, ...opts }),
};

export const shell = {
  /** @param {{cwd?:string, env?:Record<string,string>, timeoutMs?:number}} [opts] */
  exec: (command, opts = {}) => call("sys.shell.exec", { command, ...opts }),
  /** @param {{cwd?:string, env?:Record<string,string>, timeoutMs?:number}} [opts] */
  run: (command, opts = {}) => call("sys.shell.run", { command, ...opts }),
  /** @param {{cwd?:string, env?:Record<string,string>}} [opts] */
  start: (command, opts = {}) => call("sys.shell.start", { command, ...opts }),
};

export const fs = {
  /** @param {"utf8"|"base64"} [encoding] */
  read: (path, encoding = "utf8") => call("sys.fs.read", { path, encoding }),
  /** @param {"utf8"|"base64"} [encoding] */
  write: (path, data, encoding = "utf8") =>
    call("sys.fs.write", { path, data, encoding }),
  list: (path) => call("sys.fs.list", { path }),
  exists: (path) => call("sys.fs.exists", { path }),
  stat: (path) => call("sys.fs.stat", { path }),
  mkdir: (path) => call("sys.fs.mkdir", { path }),
  remove: (path, recursive = false) => call("sys.fs.remove", { path, recursive }),
  rename: (from, to) => call("sys.fs.rename", { from, to }),
  /** @param {boolean} [overwrite] */
  copy: (src, dst, overwrite = false) => call("sys.fs.copy", { src, dst, overwrite }),
  /**
   * Poll a path and emit nex:fs.changed events on creation/modification/deletion.
   * @param {string} path
   * @param {{ id?, intervalMs? }} [opts]
   */
  watch: (path, opts = {}) => call("sys.fs.watch", { path, ...opts }),
  /** Stop a watcher started with fs.watch(). */
  unwatch: (id) => call("sys.fs.unwatch", { id }),
  /** Shell-style glob pattern. Returns { matches: string[], count }. */
  glob: (pattern) => call("sys.fs.glob", { pattern }),
  /** Resolve a path to its absolute form relative to the server's CWD. */
  abs: (path) => call("sys.fs.abs", { path }),
  /**
   * Create a temporary file or directory. Returns { path, isDir }.
   * @param {{ dir?, prefix?, isDir? }} [opts]
   */
  temp: (opts = {}) => call("sys.fs.temp", opts),
};

export const app = {
  info: () => call("sys.app.info"),
  quit: () => call("sys.app.quit"),
  reload: () => call("sys.app.reload"),
};

export const framework = {
  info: () => call("sys.framework.info"),
  stack: () => call("sys.framework.stack"),
};

export const kv = {
  get: (key) => call("sys.kv.get", { key }),
  set: (key, value) => call("sys.kv.set", { key, value }),
  delete: (key) => call("sys.kv.delete", { key }),
  list: () => call("sys.kv.list"),
  /** Delete all entries from the persistent KV store. Returns { ok, cleared }. */
  clear: () => call("sys.kv.clear"),
};

export const netutil = {
  resolve: (host) => call("sys.net.resolve", { host }),
  /**
   * TCP dial check: returns { open, host, port, addr }.
   * @param {{ host?: string, port: number, timeoutMs?: number }} opts
   */
  port: ({ host = "localhost", port, timeoutMs } = {}) =>
    call("sys.net.port", { host, port, timeoutMs }),
};

/** Running processes on the server (pid, ppid, user, name). */
export const proc = {
  list: () => call("sys.proc.list"),
};

export const security = {
  capabilities: () => call("sys.security.capabilities"),
};

export const logger = {
  print:   (message) => call("sys.log.print",   { message }),
  trace:   (message) => call("sys.log.trace",   { message }),
  debug:   (message) => call("sys.log.debug",   { message }),
  info:    (message) => call("sys.log.info",    { message }),
  warning: (message) => call("sys.log.warning", { message }),
  error:   (message) => call("sys.log.error",   { message }),
};

// ── API risk levels ───────────────────────────────────────────────────────────

const privilegedApiIds = new Set([
  // Arbitrary code / shell execution
  "sys.shell.exec", "sys.shell.run", "sys.shell.start",
  // Filesystem access (read, write, delete, move)
  "sys.fs.read", "sys.fs.write", "sys.fs.list", "sys.fs.exists",
  "sys.fs.stat", "sys.fs.mkdir", "sys.fs.copy", "sys.fs.temp",
  "sys.fs.watch", "sys.fs.unwatch", "sys.fs.abs", "sys.fs.glob",
  "sys.fs.remove", "sys.fs.rename",
  // Service disruption
  "sys.app.reload", "sys.app.quit",
]);

const userMediatedApiIds = new Set([
  // Env / sensitive config exposure
  "sys.env.get", "sys.env.list", "sys.env.paths",
  // System identity + process exposure (includes os.Args which may contain secrets)
  "sys.os.user", "sys.os.host", "sys.os.process", "sys.os.info",
  // Process inspection
  "sys.proc.list",
  // KV mutations
  "sys.kv.set", "sys.kv.delete", "sys.kv.clear",
  // Outbound network
  "sys.http.fetch", "sys.net.resolve", "sys.net.port",
]);

export function apiRiskLevel(id) {
  if (privilegedApiIds.has(id)) return "privileged";
  if (userMediatedApiIds.has(id)) return "user";
  return "safe";
}

// ── API catalog (for dashboard/docs) ─────────────────────────────────────────

export const apiCatalog = [
  {
    namespace: "framework",
    methods: [
      { id: "sys.framework.info",  sig: "framework.info()",  desc: "nex-web framework identity: name, version, build, updated, author, stack." },
      { id: "sys.framework.stack", sig: "framework.stack()", desc: "Resolved runtime stack: Go version, build settings (CGO, GOARCH, GOOS), deps." },
    ],
  },
  {
    namespace: "app",
    methods: [
      { id: "sys.app.info",   sig: "app.info()",   desc: "App metadata: name, version, build, author, public env vars." },
      { id: "sys.app.quit",   sig: "app.quit()",   desc: "Gracefully stop the web server." },
      { id: "sys.app.reload", sig: "app.reload()", desc: "Reload all connected browser clients via SSE." },
    ],
  },
  {
    namespace: "os",
    methods: [
      { id: "sys.os.info", sig: "os.info()", desc: "Full OS snapshot: host, user, runtime, network, disks, memory, time." },
      { id: "sys.os.host", sig: "os.host()", desc: "Hostname, OS, arch, CPU model, kernel, platform, uptime." },
      { id: "sys.os.user", sig: "os.user()", desc: "Current user: uid, gid, username, display name, groups." },
      { id: "sys.os.runtime", sig: "os.runtime()", desc: "Go runtime stats, goroutines, memory, build info." },
      { id: "sys.os.process", sig: "os.process()", desc: "PID, PPID, executable path, working directory, args." },
      { id: "sys.os.network", sig: "os.network()", desc: "Network interfaces with IP addresses and flags." },
      { id: "sys.os.disks", sig: "os.disks()", desc: "Disk volumes with size, used, available." },
      { id: "sys.os.memory", sig: "os.memory()", desc: "Go heap stats + system memory info." },
      { id: "sys.os.time", sig: "os.time()", desc: "Current time (local, UTC, unix), timezone, offset." },
    ],
  },
  {
    namespace: "http",
    methods: [
      { id: "sys.http.fetch", sig: "http.fetch(url, opts?)", desc: "Server-side HTTP/HTTPS request — bypasses CORS. skipVerify per cert self-signed." },
    ],
  },
  {
    namespace: "env",
    methods: [
      { id: "sys.env.paths", sig: "env.paths()",         desc: "Standard paths: home, config, cache, temp, exe, cwd." },
      { id: "sys.env.get",   sig: "env.get(key)",        desc: "Read a specific backend environment variable by name." },
      { id: "sys.env.list",  sig: "env.list(prefix?)",   desc: "List backend env vars; optional prefix filter (e.g. 'VITE_')." },
    ],
  },
  {
    namespace: "log",
    methods: [
      { id: "sys.log.print",   sig: "logger.print(message)",   desc: "Write to backend log output (no level prefix)." },
      { id: "sys.log.trace",   sig: "logger.trace(message)",   desc: "Write [trace] entry to backend log." },
      { id: "sys.log.debug",   sig: "logger.debug(message)",   desc: "Write [debug] entry to backend log." },
      { id: "sys.log.info",    sig: "logger.info(message)",    desc: "Write [info] entry to backend log." },
      { id: "sys.log.warning", sig: "logger.warning(message)", desc: "Write [warning] entry to backend log." },
      { id: "sys.log.error",   sig: "logger.error(message)",   desc: "Write [error] entry to backend log." },
    ],
  },
  {
    namespace: "shell",
    methods: [
      { id: "sys.shell.exec", sig: "shell.exec(command, opts?)", desc: "Run a shell command and capture stdout/stderr." },
      { id: "sys.shell.run", sig: "shell.run(command, opts?)", desc: "Alias for shell.exec." },
      { id: "sys.shell.start", sig: "shell.start(command, opts?)", desc: "Start a background process; returns its PID." },
    ],
  },
  {
    namespace: "fs",
    methods: [
      { id: "sys.fs.read",    sig: "fs.read(path, enc?)",      desc: "Read file (utf8 | base64)." },
      { id: "sys.fs.write",   sig: "fs.write(path, data, enc?)", desc: "Write file." },
      { id: "sys.fs.list",    sig: "fs.list(path)",            desc: "List directory." },
      { id: "sys.fs.exists",  sig: "fs.exists(path)",          desc: "Check existence." },
      { id: "sys.fs.stat",    sig: "fs.stat(path)",            desc: "File metadata." },
      { id: "sys.fs.mkdir",   sig: "fs.mkdir(path)",           desc: "Create directory (recursive)." },
      { id: "sys.fs.remove",  sig: "fs.remove(path, rec?)",    desc: "Remove file/directory." },
      { id: "sys.fs.rename",  sig: "fs.rename(from, to)",      desc: "Rename/move." },
      { id: "sys.fs.copy",    sig: "fs.copy(src, dst, ow?)",   desc: "Copy file or directory. overwrite=false by default." },
      { id: "sys.fs.watch",   sig: "fs.watch(path, opts?)",    desc: "Watch path for changes; emits nex:fs.changed events." },
      { id: "sys.fs.unwatch", sig: "fs.unwatch(id)",           desc: "Stop a watcher by id." },
      { id: "sys.fs.glob",   sig: "fs.glob(pattern)",          desc: "Shell-style glob — returns matching paths { matches, count }." },
      { id: "sys.fs.abs",    sig: "fs.abs(path)",              desc: "Resolve path to absolute form (server CWD-relative)." },
      { id: "sys.fs.temp",   sig: "fs.temp(opts?)",            desc: "Create a temp file or dir; returns { path, isDir }. opts: { dir?, prefix?, isDir? }." },
    ],
  },
  {
    namespace: "kv",
    methods: [
      { id: "sys.kv.get",    sig: "kv.get(key)",        desc: "Read a value from the persistent KV store." },
      { id: "sys.kv.set",    sig: "kv.set(key, value)", desc: "Write any JSON value to the persistent KV store." },
      { id: "sys.kv.delete", sig: "kv.delete(key)",     desc: "Remove a key from the persistent KV store." },
      { id: "sys.kv.list",   sig: "kv.list()",          desc: "List all entries in the KV store." },
      { id: "sys.kv.clear",  sig: "kv.clear()",         desc: "Delete ALL entries from the persistent KV store. Returns { cleared }." },
    ],
  },
  {
    namespace: "net",
    methods: [
      { id: "sys.net.resolve", sig: "netutil.resolve(host)",         desc: "DNS lookup: IP addresses + CNAME for a hostname." },
      { id: "sys.net.port",    sig: "netutil.port({host,port})",     desc: "TCP dial check: returns { open, host, port, addr }." },
    ],
  },
  {
    namespace: "proc",
    methods: [
      { id: "sys.proc.list", sig: "proc.list()", desc: "List running processes: pid, ppid, user, name. Includes { supported } — false on platforms without a safe pure-Go implementation (currently: darwin)." },
    ],
  },
  {
    namespace: "security",
    methods: [
      { id: "sys.security.capabilities", sig: "security.capabilities()", desc: "Inspect sensitive API capability surfaces and active filesystem jail status." },
    ],
  },
];
