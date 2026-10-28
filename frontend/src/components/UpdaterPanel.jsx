import { useEffect, useState } from "react";
import { FiDownload, FiRefreshCw, FiRotateCw } from "react-icons/fi";
import * as nex from "../lib/nex.js";

const emptyState = {
  configured: false,
  source: "",
  currentVersion: "",
  currentBuild: "",
  status: "idle",
  update: null,
  progress: { received: 0, total: 0 },
  settings: { autoCheck: true, autoDownload: false, includePrerelease: false },
  lastCheck: "",
  error: "",
};

const settingLabels = [
  { key: "autoCheck", label: "Automatic check" },
  { key: "autoDownload", label: "Automatic download" },
  { key: "includePrerelease", label: "Include pre-releases" },
];

const statusLabels = {
  idle: "up to date or not checked",
  checking: "checking…",
  available: "update available",
  downloading: "downloading…",
  downloaded: "ready to install",
  installing: "installing…",
  error: "error",
};

function mergeState(prev, next) {
  return {
    ...prev,
    ...next,
    progress: { ...prev.progress, ...(next?.progress ?? {}) },
    settings: { ...prev.settings, ...(next?.settings ?? {}) },
  };
}

function formatBytes(value) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function describeResult(result, state) {
  const status = result.status ?? "";
  const reason = result.reason ? `: ${result.reason}` : ".";
  if (status === "updated") {
    return { ok: true, text: `Updated to ${state.currentVersion} · ${state.currentBuild}.` };
  }
  if (status.startsWith("rolledBack")) {
    return { ok: false, text: `Update was rolled back${reason}` };
  }
  return { ok: false, text: `Update failed${reason}` };
}

export function UpdaterPanel({ className = "" }) {
  const [state, setState] = useState(emptyState);
  const [lastResult, setLastResult] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!nex.isReady()) return undefined;
    let cancelled = false;
    let timers = [];
    const pollLastResult = (attempt) => {
      nex.updater
        .getLastResult()
        .then((result) => {
          if (cancelled) return;
          if (result) setLastResult(result);
          else if (attempt < 5) timers.push(setTimeout(() => pollLastResult(attempt + 1), 1500));
        })
        .catch(() => {});
    };
    const refresh = () => {
      timers.forEach(clearTimeout);
      timers = [];
      nex.updater.getState().then((next) => !cancelled && setState((prev) => mergeState(prev, next))).catch((e) => setError(e.message));
      pollLastResult(0);
    };
    refresh();
    const offConnected = nex.on("connected", refresh);
    const offState = nex.updater.onState((next) => setState((prev) => mergeState(prev, next)));
    const offProgress = nex.updater.onProgress((progress) =>
      setState((prev) => ({ ...prev, progress: { ...prev.progress, ...progress } }))
    );
    return () => {
      cancelled = true;
      timers.forEach(clearTimeout);
      offConnected();
      offState();
      offProgress();
    };
  }, []);

  async function run(action) {
    setBusy(true);
    setError("");
    try {
      const next = await action();
      if (next && typeof next === "object") setState((prev) => mergeState(prev, next));
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function installAndRestart() {
    try {
      return await nex.updater.install();
    } catch (e) {
      if (e instanceof TypeError) return { status: "installing" };
      throw e;
    }
  }

  async function changeSetting(key, value) {
    await run(async () => {
      await nex.updater.setSettings({ ...state.settings, [key]: value });
      return nex.updater.getState();
    });
  }

  if (!nex.isReady()) {
    return (
      <div className={`panel updater-panel ${className}`.trim()}>
        <div className="note updater-note">Updates are available only when the nex bridge is loaded.</div>
      </div>
    );
  }

  const update = state.update;
  const installable = Boolean(update?.installable);
  const downloading = state.status === "downloading";
  const total = state.progress.total || 0;
  const percent = total > 0 ? Math.min(100, Math.round((state.progress.received / total) * 100)) : 0;
  const errorText = error || state.error;
  const resultView = lastResult ? describeResult(lastResult, state) : null;

  return (
    <div className={`panel updater-panel ${className}`.trim()}>
      <div className="kv">
        <div className="k">current</div>
        <div>
          {state.currentVersion || "…"}
          {state.currentBuild ? ` · ${state.currentBuild}` : ""}
        </div>
        <div className="k">status</div>
        <div>{state.configured ? statusLabels[state.status] ?? state.status : "updates not configured"}</div>
        {state.source ? (
          <>
            <div className="k">source</div>
            <div>{state.source}</div>
          </>
        ) : null}
        {state.lastCheck ? (
          <>
            <div className="k">last check</div>
            <div>{new Date(state.lastCheck).toLocaleString()}</div>
          </>
        ) : null}
      </div>

      {resultView ? (
        <div className={`updater-banner ${resultView.ok ? "ok" : "err"}`}>{resultView.text}</div>
      ) : null}

      {update ? (
        <div className="updater-update">
          <div className="updater-update-head">
            <span className="updater-version">
              {update.version} · {update.build}
            </span>
            {update.date ? <span className="updater-date">{update.date}</span> : null}
            {update.mandatory ? <span className="updater-tag">mandatory</span> : null}
            {update.rolledBack ? <span className="updater-tag">previously rolled back</span> : null}
          </div>
          {update.notes ? <pre className="updater-notes">{update.notes}</pre> : null}
          {!installable && update.reason ? <div className="updater-reason">{update.reason}</div> : null}
          {downloading ? (
            <div className="updater-progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}>
              <div className="updater-progress-bar" style={{ width: `${percent}%` }} />
              <span className="updater-progress-text">
                {formatBytes(state.progress.received)} / {total > 0 ? formatBytes(total) : "?"}
              </span>
            </div>
          ) : null}
        </div>
      ) : null}

      {errorText ? <div className="updater-banner err">{errorText}</div> : null}

      <div className="updater-actions">
        <button
          className="btn"
          type="button"
          disabled={busy || !state.configured || downloading || state.status === "installing"}
          onClick={() => run(() => nex.updater.check({ manual: true }))}
        >
          <FiRefreshCw aria-hidden="true" className="ui-icon" />
          Check for updates
        </button>
        {update && installable && state.status === "available" ? (
          <button className="btn" type="button" disabled={busy} onClick={() => run(() => nex.updater.download())}>
            <FiDownload aria-hidden="true" className="ui-icon" />
            Download
          </button>
        ) : null}
        {update && installable && state.status === "downloaded" ? (
          <button className="btn" type="button" disabled={busy} onClick={() => run(installAndRestart)}>
            <FiRotateCw aria-hidden="true" className="ui-icon" />
            Install and restart
          </button>
        ) : null}
      </div>

      <div className="updater-settings">
        {settingLabels.map(({ key, label }) => (
          <label className="updater-toggle" key={key}>
            <input
              type="checkbox"
              checked={Boolean(state.settings[key])}
              disabled={busy || !state.configured}
              onChange={(event) => changeSetting(key, event.target.checked)}
            />
            <span>{label}</span>
          </label>
        ))}
      </div>
    </div>
  );
}
