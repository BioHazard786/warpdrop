import { execFileSync } from "node:child_process"
import { createHash } from "node:crypto"
import { createReadStream, existsSync, mkdirSync, rmSync, statSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"
import { expect, test } from "@playwright/test"
import { BASE, CODE_RE, acceptButton, openReceiver, runCli } from "./helpers"

// Opt-in (E2E_BIG=1): writes ~10 GB. For a Firefox memory check use manual/firefox_real.py (see manual/README.md). Run with TMPDIR on a real disk, NOT a RAM-backed /tmp, or the memory
// measurement is meaningless:  TMPDIR=~/.cache/warpdrop-e2e-big E2E_BIG=1 bun x playwright test --project=firefox -g "5 GB"
const GB = 1024 ** 3
const SIZE = Math.round(Number(process.env.E2E_BIG_GB ?? 5) * GB)
// E2E_BIG_RELAY=1 forces the (rate-limited) relay, which makes the transfer slow enough to tell time-based from size-based failures
const RELAY = process.env.E2E_BIG_RELAY ? "?transport=relay" : ""
const dir = process.env.E2E_BIG_DIR ?? join(homedir(), ".cache", "warpdrop-e2e-big")

const hashFile = (p: string) =>
  new Promise<string>((res, rej) => {
    const h = createHash("sha256")
    createReadStream(p).on("data", (d) => h.update(d)).on("end", () => res(h.digest("hex").slice(0, 16))).on("error", rej)
  })

/** Sum of resident memory (MB) of every Playwright browser process. */
function firefoxRssMB(): number {
  const out = execFileSync("ps", ["-eo", "rss=,args="], { encoding: "utf8" })
  return out
    .split("\n")
    .filter((l) => l.includes("ms-playwright/"))
    .reduce((n, l) => n + Number(l.trim().split(/\s+/)[0]) / 1024, 0)
}

test.skip(!process.env.E2E_BIG, "set E2E_BIG=1 to run the multi-GB streaming test")
test.setTimeout(30 * 60_000)

test("a 5 GB file streams to disk with an exact hash, and flat memory on Chromium @firefox", async ({ browser, browserName }) => {
  mkdirSync(dir, { recursive: true })
  const src = join(dir, "big.bin")
  if (!existsSync(src) || statSync(src).size !== SIZE) {
    console.log(`creating ${SIZE / GB} GB source file in ${dir} ...`)
    execFileSync("sh", ["-c", `head -c ${SIZE} /dev/urandom > "${src}"`])
  }
  const want = await hashFile(src)

  const cli = runCli(["send", src])
  const [, room, secret] = await cli.waitFor(CODE_RE)
  const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${room}${RELAY}#${secret}`)
  const baseline = firefoxRssMB()

  const samples: number[] = []
  // Kill switch: if the browser balloons, stop before it takes the machine down with it.
  const LIMIT_MB = Number(process.env.E2E_BIG_RSS_LIMIT_MB ?? 6000)
  const timer = setInterval(() => {
    const rss = firefoxRssMB()
    samples.push(rss)
    if (rss > LIMIT_MB) {
      clearInterval(timer)
      void rx.context().browser()?.close()
      cli.kill()
      console.log(`ABORTED: browser memory ${rss.toFixed(0)} MB passed the ${LIMIT_MB} MB safety limit`)
    }
  }, 1000)
  const t0 = Date.now()
  await rx.evaluate(() => {
    const w = window as unknown as { __sw: string[] }
    w.__sw = []
    new BroadcastChannel("wd-dbg").onmessage = (e) => w.__sw.push(e.data)
  })
  const [download] = await Promise.all([rx.waitForEvent("download", { timeout: 60_000 }), acceptButton(rx).click()])
  // Playwright's download.path() can return while Firefox is still streaming a service-worker download, so the
  // app's own completion message is the signal, and only then do we look at the file.
  // Fail fast with diagnostics if progress freezes, instead of waiting out a 25 minute timeout.
  const logs: string[] = []
  rx.on("console", (m) => logs.push(`[${m.type()}] ${m.text()}`))
  rx.on("pageerror", (e) => logs.push(`[pageerror] ${e.message}`))
  let last = "", since = Date.now()
  for (;;) {
    if (await rx.getByText(/Received 1 file/).isVisible()) break
    const txt = (await rx.locator("main").innerText()).replace(/\s+/g, " ")
    const m = /([\d.]+ [KMG]B) \/ [\d.]+ [KMG]B/.exec(txt)?.[1] ?? txt.slice(-80)
    if (m !== last) {
      last = m
      since = Date.now()
      console.log(`  +${((Date.now() - t0) / 1000).toFixed(0)}s  ${m}`)
    }
    if (await rx.getByText("Transfer failed").isVisible()) since = 0 // the app reported it: stop waiting
    if (Date.now() - since > 60_000) {
      const sw = await rx.evaluate(async () =>
        (await navigator.serviceWorker.getRegistrations()).map((r) => ({ scope: r.scope, active: r.active?.state, waiting: !!r.waiting })),
      )
      const failed = await rx.getByText("Transfer failed").isVisible()
      const swlog = await rx.evaluate(() => (window as unknown as { __sw: string[] }).__sw.slice(-25))
      throw new Error(
        `transfer froze at ${last} (failed banner: ${failed})\nservice workers: ${JSON.stringify(sw)}\nFirefox RSS ${firefoxRssMB().toFixed(0)} MB\npage text: ${txt.slice(0, 300)}\nSW log:\n${swlog.join("\n")}\nconsole:\n${logs.slice(-15).join("\n")}`,
      )
    }
    await new Promise((r) => setTimeout(r, 2000))
  }
  clearInterval(timer)
  const secs = (Date.now() - t0) / 1000
  const path = await download.path()
  for (let i = 0; i < 120 && statSync(path).size < SIZE; i++) await new Promise((r) => setTimeout(r, 500)) // browser finalizing

  expect(statSync(path).size).toBe(SIZE)
  expect(await hashFile(path)).toBe(want)
  expect(await cli.exited).toBe(0)

  const peak = Math.max(...samples)
  console.log(`5 GB in ${secs.toFixed(0)}s (${(SIZE / 1e6 / secs).toFixed(0)} MB/s); Firefox RSS baseline ${baseline.toFixed(0)} MB, peak ${peak.toFixed(0)} MB, samples ${samples.length}`)
  // Flat means nowhere near the file size. Only asserted on Chromium: Playwright's patched Firefox buffers downloads in
  // memory (no file appears on disk until the end, and RSS grows with the file), which stock Firefox does not do.
  // Real Firefox was measured separately with manual/firefox_real.py: 5 GB streamed with +613 MB peak memory.
  if (browserName === "chromium") expect(peak - baseline).toBeLessThan(1024)
  rmSync(path, { force: true })
})
