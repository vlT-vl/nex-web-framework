const NEX = (typeof window !== "undefined" && window.__NEX__) || { token: "", base: "" };
const API_BASE = import.meta.env.VITE_API_BASE ?? "/api";

export class nexError extends Error {
  constructor(code, message) {
    super(message);
    this.code = code;
    this.name = "nexError";
  }
}

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

export function on(event, handler) {
  const listener = (e) => handler(e.detail);
  window.addEventListener(`nex:${event}`, listener);
  return () => window.removeEventListener(`nex:${event}`, listener);
}

export async function session() {
  const res = await fetch(`${API_BASE}/session`, {
    headers: { "X-nex-Token": NEX.token },
  });
  if (!res.ok) throw new nexError(`http_${res.status}`, `HTTP ${res.status}`);
  return res.json();
}

export const os = {
  info: () => call("sys.os.info"),
  host: () => call("sys.os.host"),
  user: () => call("sys.os.user"),
  runtime: () => call("sys.os.runtime"),
  process: () => call("sys.os.process"),
  network: () => call("sys.os.network"),
  disks: () => call("sys.os.disks"),
  memory: () => call("sys.os.memory"),
  time: () => call("sys.os.time"),
  paths: () => call("sys.env.paths"),
};

export const env = {
  paths: () => call("sys.env.paths"),
  get: (key) => call("sys.env.get", { key }),
  list: (prefix = "") => call("sys.env.list", { prefix }),
};

export const http = {
  fetch: (url, opts = {}) => call("sys.http.fetch", { url, ...opts }),
};

export const shell = {
  exec: (command, opts = {}) => call("sys.shell.exec", { command, ...opts }),
  run: (command, opts = {}) => call("sys.shell.run", { command, ...opts }),
  start: (command, opts = {}) => call("sys.shell.start", { command, ...opts }),
};

export const fs = {
  read: (path, encoding = "utf8") => call("sys.fs.read", { path, encoding }),
  write: (path, data, encoding = "utf8") =>
    call("sys.fs.write", { path, data, encoding }),
  list: (path) => call("sys.fs.list", { path }),
  exists: (path) => call("sys.fs.exists", { path }),
  stat: (path) => call("sys.fs.stat", { path }),
  mkdir: (path) => call("sys.fs.mkdir", { path }),
  remove: (path, recursive = false) => call("sys.fs.remove", { path, recursive }),
  rename: (from, to) => call("sys.fs.rename", { from, to }),
  copy: (src, dst, overwrite = false) => call("sys.fs.copy", { src, dst, overwrite }),
  watch: (path, opts = {}) => call("sys.fs.watch", { path, ...opts }),
  unwatch: (id) => call("sys.fs.unwatch", { id }),
  glob: (pattern) => call("sys.fs.glob", { pattern }),
  abs: (path) => call("sys.fs.abs", { path }),
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
  clear: () => call("sys.kv.clear"),
};

export const netutil = {
  resolve: (host) => call("sys.net.resolve", { host }),
  port: ({ host = "localhost", port, timeoutMs } = {}) =>
    call("sys.net.port", { host, port, timeoutMs }),
};

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

const privilegedApiIds = new Set([
  "sys.shell.exec", "sys.shell.run", "sys.shell.start",
  "sys.fs.read", "sys.fs.write", "sys.fs.list", "sys.fs.exists",
  "sys.fs.stat", "sys.fs.mkdir", "sys.fs.copy", "sys.fs.temp",
  "sys.fs.watch", "sys.fs.unwatch", "sys.fs.abs", "sys.fs.glob",
  "sys.fs.remove", "sys.fs.rename",
  "sys.app.reload", "sys.app.quit",
]);

const userMediatedApiIds = new Set([
  "sys.env.get", "sys.env.list", "sys.env.paths",
  "sys.os.user", "sys.os.host", "sys.os.process", "sys.os.info",
  "sys.proc.list",
  "sys.kv.set", "sys.kv.delete", "sys.kv.clear",
  "sys.http.fetch", "sys.net.resolve", "sys.net.port",
]);

export function apiRiskLevel(id) {
  if (privilegedApiIds.has(id)) return "privileged";
  if (userMediatedApiIds.has(id)) return "user";
  return "safe";
}

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
