import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { readFileSync } from "node:fs";

const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf8"));

const envDir = fileURLToPath(new URL("..", import.meta.url));
const devHost = process.env.NEX_VITE_HOST ?? "127.0.0.1";
const devPort = Number(process.env.NEX_VITE_PORT ?? 5181);

export default defineConfig({
  plugins: [react()],
  base: "./",
  envDir,
  publicDir: "../res",
  server: {
    host: devHost,
    port: devPort,
    strictPort: true,
    proxy: {
      "/api": "http://127.0.0.1:34116",
      "/nex.js": "http://127.0.0.1:34116",
    },
    fs: {
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
