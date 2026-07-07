import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { readFileSync } from "node:fs";

const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf8"));

// Read .env from the repo root so Go and Vite share the same file.
const envDir = fileURLToPath(new URL("..", import.meta.url));
const devHost = process.env.NEX_VITE_HOST ?? "127.0.0.1";
const devPort = Number(process.env.NEX_VITE_PORT ?? 5181);

export default defineConfig({
  plugins: [react()],
  base: "./",          // relative paths so assets work when served from Go
  envDir,
  publicDir: "../res",
  server: {
    host: devHost,
    port: devPort,
    strictPort: true,
    proxy: {
      // In dev mode Vite proxies /api and /nex.js to the Go backend.
      "/api": "http://127.0.0.1:34116",
      "/nex.js": "http://127.0.0.1:34116",
    },
    fs: {
      // Allow ?raw imports from the repo root (e.g. DOCS.md) in dev mode.
      allow: [envDir, "."],
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  define: {
    __PKG_REACT__:        JSON.stringify(pkg.dependencies?.react        ?? ""),
    __PKG_REACT_ICONS__:  JSON.stringify(pkg.dependencies?.["react-icons"] ?? ""),
    __PKG_VITE__:         JSON.stringify(pkg.devDependencies?.vite      ?? ""),
  },
});
