const STORAGE_KEY = "nex-theme";
const THEMES = new Set(["system", "light", "dark"]);

export function getSystemTheme() {
  if (typeof window === "undefined" || !window.matchMedia) return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function getThemePreference() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (THEMES.has(saved)) return saved;
  } catch {
    // Storage can fail in restricted contexts; fall back to the OS.
  }
  return "system";
}

export function resolveTheme(preference = getThemePreference()) {
  return preference === "dark" || preference === "light" ? preference : getSystemTheme();
}

export function applyTheme(preference = getThemePreference()) {
  const normalized = THEMES.has(preference) ? preference : "system";
  const resolved = resolveTheme(normalized);
  const root = document.documentElement;
  root.dataset.theme = resolved;
  root.dataset.themePreference = normalized;
  root.style.colorScheme = resolved;
  return { preference: normalized, theme: resolved };
}

export function setThemePreference(preference) {
  const normalized = THEMES.has(preference) ? preference : "system";
  try {
    localStorage.setItem(STORAGE_KEY, normalized);
  } catch {
    // The applied document theme is still updated below.
  }
  return applyTheme(normalized);
}

export function toggleTheme(current = getThemePreference()) {
  const resolved = resolveTheme(current);
  return setThemePreference(resolved === "dark" ? "light" : "dark");
}

export function watchSystemTheme(callback) {
  if (typeof window === "undefined" || !window.matchMedia) return () => {};
  const query = window.matchMedia("(prefers-color-scheme: dark)");
  const listener = () => callback(getSystemTheme());
  if (query.addEventListener) {
    query.addEventListener("change", listener);
  } else {
    query.addListener?.(listener);
  }
  return () => {
    if (query.removeEventListener) {
      query.removeEventListener("change", listener);
    } else {
      query.removeListener?.(listener);
    }
  };
}
