import { type ChildProcess, execFileSync, spawn } from "node:child_process"
import { mkdirSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { PORT } from "./playwright.config"

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, "..")
export const BIN = join(here, ".bin")

let server: ChildProcess | undefined

/** Builds the web app (with the E2E hooks compiled in), the server and the CLI, then starts a real server. */
export default async function globalSetup() {
  mkdirSync(BIN, { recursive: true })
  const run = (cmd: string, args: string[], cwd: string, env: NodeJS.ProcessEnv = {}) =>
    execFileSync(cmd, args, { cwd, stdio: "inherit", env: { ...process.env, ...env } })
  run("bun", ["install", "--frozen-lockfile"], join(root, "web"))
  run("bun", ["run", "build"], join(root, "web"), { VITE_E2E: "1" }) // writes backend/web/dist, which the server embeds
  run("go", ["build", "-o", join(BIN, "server"), "./cmd/server"], join(root, "backend"))
  run("go", ["build", "-o", join(BIN, "warpdrop"), "."], join(root, "cli"))

  server = spawn(join(BIN, "server"), [], {
    stdio: "inherit",
    env: {
      ...process.env,
      PORT: String(PORT),
      // tests create many rooms from one IP; throttle the relay so transfers last long enough to interrupt
      RATE_LIMIT_SCALE: "100",
      RELAY_MAX_MBPS: "40",
    },
  })
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`http://127.0.0.1:${PORT}/healthz`)).ok) return () => void server?.kill()
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 100))
  }
  server.kill()
  throw new Error(`server did not start on port ${PORT} (is it already in use? set E2E_PORT)`)
}
