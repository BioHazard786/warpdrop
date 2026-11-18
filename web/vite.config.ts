import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import { tanstackRouter } from "@tanstack/router-plugin/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

const backend = process.env.WARPDROP_BACKEND ?? "http://localhost:8080"

export default defineConfig({
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: { alias: { "@": resolve(import.meta.dirname, "./src") } },
  server: {
    proxy: {
      "/ws": { target: backend, ws: true },
      "/relay": { target: backend, ws: true },
      "/api": backend,
    },
  },
  build: { outDir: "../backend/web/dist", emptyOutDir: true },
  test: { include: ["src/**/*.test.ts"] },
})
