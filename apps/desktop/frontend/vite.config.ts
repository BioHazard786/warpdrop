import { resolve } from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import wails from "@wailsio/runtime/plugins/vite"
import { defineConfig } from "vite"

// The UI building blocks (buttons, items, progress...) are the web app's own files, shared through "@": the same
// look with no copy to keep in sync. "~" is this app. Both apps need one React, hence dedupe.
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    fs: { allow: [resolve(import.meta.dirname, "../../..")] },
  },
  resolve: {
    alias: { "@": resolve(import.meta.dirname, "../../../web/src"), "~": resolve(import.meta.dirname, "src") },
    dedupe: ["react", "react-dom", "@base-ui/react", "@tabler/icons-react", "class-variance-authority", "cn"],
  },
  plugins: [react(), tailwindcss(), wails("./bindings")],
})
