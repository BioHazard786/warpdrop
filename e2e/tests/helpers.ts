import { type ChildProcess, spawn } from "node:child_process"
import { createHash, randomBytes } from "node:crypto"
import { readFile } from "node:fs/promises"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { type BrowserContext, type Download, type Page, expect } from "@playwright/test"
import { unzipSync } from "fflate"
import { PORT } from "../playwright.config"

export const BASE = `http://127.0.0.1:${PORT}`
const here = dirname(fileURLToPath(import.meta.url))
export const CLI = join(here, "..", ".bin", "warpdrop")

export interface TestFile {
  name: string
  mimeType: string
  buffer: Buffer
}

export const sha = (b: Uint8Array) => createHash("sha256").update(b).digest("hex").slice(0, 16)
export const file = (name: string, size: number, seed?: number): TestFile => ({
  name,
  mimeType: "application/octet-stream",
  // deterministic when seeded so a failing run can be reproduced
  buffer: seed === undefined ? randomBytes(size) : Buffer.from(Uint8Array.from({ length: size }, (_, i) => (i * 31 + seed + (i >> 8)) & 255)),
})

export interface Share {
  code: string
  roomId: string
  secret: string
  link: string
}

/** Open the send page, choose files, and wait for the share code. */
export async function startSend(page: Page, files: TestFile[]): Promise<Share> {
  await page.goto("/")
  await page.locator('input[type="file"]').setInputFiles(files)
  const code = (await page.locator('[aria-label="Share code"]').textContent({ timeout: 20_000 }))!.trim()
  const [roomId, ...words] = code.split("-")
  const secret = words.join("-")
  return { code, roomId: roomId!, secret, link: `${BASE}/r/${roomId}#${secret}` }
}

export const acceptButton = (p: Page) => p.getByRole("button", { name: "Accept and download" })

/** Open a receiver in its own context, wait for the offer. */
export async function openReceiver(context: BrowserContext, url: string): Promise<Page> {
  const page = await context.newPage()
  await page.goto(url)
  await expect(acceptButton(page)).toBeVisible({ timeout: 30_000 })
  return page
}

/** Accept and return the finished download's bytes. */
export async function acceptAndRead(page: Page): Promise<{ bytes: Buffer; download: Download }> {
  const [download] = await Promise.all([page.waitForEvent("download", { timeout: 30_000 }), acceptButton(page).click()])
  const path = await download.path() // resolves when the download has finished, throws if it failed
  return { bytes: await readFile(path), download }
}

export const unzip = (b: Buffer) => unzipSync(new Uint8Array(b))

/** Current download progress of a receiver page, 0..100. */
export const progress = async (p: Page) => Number((await p.getByRole("progressbar").first().getAttribute("aria-valuenow")) ?? 0)

export const stripAnsi = (s: string) => s.replace(/\x1b\[[0-9;?]*[a-zA-Z]/g, "")

export interface Cli {
  proc: ChildProcess
  output(): string
  exited: Promise<number>
  waitFor(re: RegExp, ms?: number): Promise<RegExpMatchArray>
  kill(): void
}

export function runCli(args: string[], cwd?: string): Cli {
  const proc = spawn(CLI, args, { cwd, env: { ...process.env, WARPDROP_SERVER: BASE, NO_COLOR: "1" } })
  let out = ""
  proc.stdout!.on("data", (d) => (out += d))
  proc.stderr!.on("data", (d) => (out += d))
  const exited = new Promise<number>((res) => proc.on("exit", (c) => res(c ?? -1)))
  return {
    proc,
    output: () => stripAnsi(out),
    exited,
    async waitFor(re, ms = 20_000) {
      const t0 = Date.now()
      while (Date.now() - t0 < ms) {
        const m = stripAnsi(out).match(re)
        if (m) return m
        await new Promise((r) => setTimeout(r, 50))
      }
      throw new Error(`timed out waiting for ${re}\n--- CLI output ---\n${stripAnsi(out)}`)
    },
    kill: () => void proc.kill("SIGKILL"),
  }
}

export const CODE_RE = /(\d{4,6})-([a-z]+-[a-z]+-[a-z]+-[a-z]+)/
