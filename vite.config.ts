import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import solid from "vite-plugin-solid"

export default defineConfig({
  plugins: [solid()],
  // The Go server embeds dist/ via webcontent and serves index.html for
  // any unknown path (SPA fallback in internal/api/router.go), so the
  // bundler output directory must stay "dist" and the app must work
  // from any route. Absolute path: outDir is resolved against the Vite
  // root (./client), so a bare "dist" would land in client/dist.
  build: {
    outDir: fileURLToPath(new URL("./dist", import.meta.url)),
  },
  // Dev only: forward API calls to a user-run Go server so the
  // same-origin HttpOnly session cookie flow works under `vite dev`.
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  appType: "spa",
})
