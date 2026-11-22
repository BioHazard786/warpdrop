import { execFileSync } from "node:child_process"
import { createHash } from "node:crypto"
import { createReadStream, statSync } from "node:fs"
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { expect, test } from "@playwright/test"
import {
  BASE, CODE_RE, acceptAndRead, acceptButton, file, openReceiver, progress, runCli, sha, startSend, unzip,
} from "./helpers"

test.describe("web to web", () => {
  test("three receivers get a zip with zero-byte and duplicate-named files, hashes intact @firefox", async ({ browser }) => {
    const files = [file("big.bin", 3_000_000), file("empty.txt", 0), file("same.txt", 20, 1), file("same.txt", 21, 2)]
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, files)

    const contexts = await Promise.all([browser.newContext(), browser.newContext(), browser.newContext()])
    const receivers = await Promise.all(contexts.map((c) => openReceiver(c, share.link)))
    const results = await Promise.all(receivers.map((p) => acceptAndRead(p)))

    for (const { bytes } of results) {
      const z = unzip(bytes)
      expect(Object.keys(z).sort()).toEqual(["big.bin", "empty.txt", "same (1).txt", "same.txt"])
      expect(sha(z["big.bin"]!)).toBe(sha(files[0]!.buffer))
      expect(z["empty.txt"]!.length).toBe(0)
      expect(sha(z["same.txt"]!)).toBe(sha(files[2]!.buffer))
      expect(sha(z["same (1).txt"]!)).toBe(sha(files[3]!.buffer))
    }
    // the sender sees every receiver finish (this used to stay "sending" forever)
    await expect(sender.getByText("Delivered", { exact: true })).toHaveCount(3, { timeout: 20_000 })
  })

  test("a single file arrives as itself, not zipped", async ({ browser }) => {
    const f = file("one.bin", 1_500_000)
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [f])
    const rx = await openReceiver(await browser.newContext(), share.link)
    const { bytes, download } = await acceptAndRead(rx)
    expect(download.suggestedFilename()).toBe("one.bin")
    expect(sha(bytes)).toBe(sha(f.buffer))
  })

  test("forced relay: encrypted WebSocket path delivers the file", async ({ browser }) => {
    const f = file("relay.bin", 800_000)
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [f])
    const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${share.roomId}?transport=relay#${share.secret}`)
    await expect(rx.getByText("RELAY", { exact: true })).toBeVisible()
    const { bytes } = await acceptAndRead(rx)
    expect(sha(bytes)).toBe(sha(f.buffer))
  })
})

test.describe("secret handling", () => {
  test("a wrong secret is rejected, nothing is offered, and the sender notices", async ({ browser }) => {
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [file("secret.bin", 1000)])
    const rx = await (await browser.newContext()).newPage()
    await rx.goto(`${BASE}/r/${share.roomId}#abacus-abdomen-abdominal-abide`)
    await expect(rx.getByText("Transfer failed")).toBeVisible({ timeout: 30_000 })
    await expect(rx.getByText(/Wrong words/)).toBeVisible()
    await expect(acceptButton(rx)).toHaveCount(0)
    await expect(sender.getByText("Wrong secret")).toBeVisible()
  })

  test("a link without the fragment asks for the words; wrong words fail, right words work", async ({ browser }) => {
    const f = file("frag.bin", 5000)
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [f])
    const rx = await (await browser.newContext()).newPage()
    await rx.goto(`${BASE}/r/${share.roomId}`)

    const input = rx.getByLabel("Four secret words")
    await input.fill("not real words")
    await expect(rx.getByRole("button", { name: "Connect" })).toBeDisabled() // not four wordlist words

    await input.fill("abacus abdomen abdominal abide")
    await rx.getByRole("button", { name: "Connect" }).click()
    await expect(rx.getByText(/Wrong words/)).toBeVisible({ timeout: 30_000 })

    await rx.getByRole("button", { name: "Try different words" }).click()
    await rx.getByLabel("Four secret words").fill(share.secret.replaceAll("-", " "))
    await rx.getByRole("button", { name: "Connect" }).click()
    await expect(acceptButton(rx)).toBeVisible({ timeout: 30_000 })
    const { bytes } = await acceptAndRead(rx)
    expect(sha(bytes)).toBe(sha(f.buffer))
  })
})

test.describe("failures and recovery", () => {
  test("closing the sender mid-file shows the receiver an error within seconds", async ({ browser }) => {
    const senderCtx = await browser.newContext()
    const sender = await senderCtx.newPage()
    const share = await startSend(sender, [file("long.bin", 45_000_000)])
    const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${share.roomId}?transport=relay#${share.secret}`)
    await acceptButton(rx).click()
    await expect.poll(() => progress(rx), { timeout: 20_000 }).toBeGreaterThan(2)
    const t0 = Date.now()
    await senderCtx.close()
    await expect(rx.getByText("Transfer failed")).toBeVisible({ timeout: 30_000 })
    expect(Date.now() - t0).toBeLessThan(30_000)
  })

  test("a dropped connection at random offsets resumes and the file still matches its hash", async ({ browser }) => {
    const seed = Math.floor(Math.random() * 1e6)
    console.log(`fault-injection seed ${seed}`) // reproduce a failure by pinning this
    const rnd = (() => { let s = seed; return () => ((s = (s * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff) })()
    const f = file("flaky.bin", 24_000_000, seed)
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [f])
    const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${share.roomId}?transport=relay#${share.secret}`)

    const download = rx.waitForEvent("download", { timeout: 30_000 })
    await acceptButton(rx).click()
    const dl = await download
    for (const at of [0.15 + rnd() * 0.15, 0.45 + rnd() * 0.15, 0.75 + rnd() * 0.1]) {
      await expect.poll(() => progress(rx), { timeout: 60_000 }).toBeGreaterThan(at * 100)
      await rx.evaluate(() => (window as unknown as { __warpdropTest: { drop(): void } }).__warpdropTest.drop())
    }
    const bytes = await readFile(await dl.path())
    // the faults really fired: each drop closed a live transport (otherwise this test would pass vacuously)
    expect(await rx.evaluate(() => (window as unknown as { __warpdropTest: { dropped: number } }).__warpdropTest.dropped)).toBeGreaterThanOrEqual(3)
    expect(bytes.length).toBe(f.buffer.length)
    expect(sha(bytes)).toBe(sha(f.buffer))
  })

  test("the sender's signaling socket can drop and the room code survives", async ({ browser }) => {
    const f = file("blip.bin", 400_000)
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [f])
    await sender.evaluate(() => (window as unknown as { __warpdropTest: { dropSignaling(): void } }).__warpdropTest.dropSignaling())
    await sender.waitForTimeout(2500) // backoff reconnect + resume_room
    await expect(sender.locator('[aria-label="Share code"]')).toHaveText(share.code)
    await expect(sender.getByText("Waiting for someone to open the link")).toBeVisible()

    const rx = await openReceiver(await browser.newContext(), share.link)
    const { bytes } = await acceptAndRead(rx)
    expect(sha(bytes)).toBe(sha(f.buffer))
  })

  test("declining stops the sender's transfer with a reason", async ({ browser }) => {
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [file("no.bin", 4000)])
    const rx = await openReceiver(await browser.newContext(), share.link)
    await rx.getByRole("button", { name: "Decline" }).click()
    await expect(rx.getByText("You declined the files")).toBeVisible()
    await expect(sender.getByText("Receiver declined")).toBeVisible()
  })
})

test.describe("CLI and web interoperate", () => {
  for (const relay of [false, true]) {
    const via = relay ? "relay" : "direct"

    test(`CLI sends, browser receives (${via})`, async ({ browser }) => {
      const dir = await mkdtemp(join(tmpdir(), "wd-e2e-"))
      const files = [file("c1.bin", 2_000_000, 5), file("empty.dat", 0), file("note.txt", 30, 6)]
      for (const f of files) await writeFile(join(dir, f.name), f.buffer)
      const cli = runCli(["send", ...(relay ? ["--relay"] : []), ...files.map((f) => join(dir, f.name))])
      const [, room, secret] = await cli.waitFor(CODE_RE)
      const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${room}${relay ? "?transport=relay" : ""}#${secret}`)
      const { bytes } = await acceptAndRead(rx)
      const z = unzip(bytes)
      expect(sha(z["c1.bin"]!)).toBe(sha(files[0]!.buffer))
      expect(z["empty.dat"]!.length).toBe(0)
      expect(sha(z["note.txt"]!)).toBe(sha(files[2]!.buffer))
      expect(await cli.exited).toBe(0) // the CLI learns the delivery completed and exits cleanly
    })

    test(`browser sends, CLI receives and sanitizes a hostile name (${via})`, async ({ browser }) => {
      const files = [file("w1.bin", 1_500_000, 7), file("..\\evil.txt", 6, 8), file("zero.txt", 0)]
      const sender = await (await browser.newContext()).newPage()
      const share = await startSend(sender, files)
      const out = await mkdtemp(join(tmpdir(), "wd-e2e-out-"))
      const cli = runCli(["receive", ...(relay ? ["--relay"] : []), share.code, "--yes", "--dir", join(out, "sub")])
      expect(await cli.exited, cli.output()).toBe(0)
      expect((await readdir(out)).sort()).toEqual(["sub"]) // nothing escaped the output directory
      const got = (await readdir(join(out, "sub"))).sort()
      expect(got).toEqual(["evil.txt", "w1.bin", "zero.txt"])
      expect(sha(await readFile(join(out, "sub", "w1.bin")))).toBe(sha(files[0]!.buffer))
      expect(sha(await readFile(join(out, "sub", "evil.txt")))).toBe(sha(files[1]!.buffer))
      await expect(sender.getByText("Delivered", { exact: true })).toBeVisible({ timeout: 20_000 })
    })
  }

  test("pressing Ctrl-C in the CLI receiver cancels cleanly: exit 130, no partial files, the browser sees it", async ({ browser }) => {
    const sender = await (await browser.newContext()).newPage()
    const share = await startSend(sender, [file("slow.bin", 45_000_000)])
    const out = await mkdtemp(join(tmpdir(), "wd-e2e-int-"))
    const cli = runCli(["receive", "--relay", share.code, "--yes", "--dir", out]) // relay is rate limited, so it stays mid-transfer
    await expect.poll(async () => (await readdir(out)).some((n) => n.endsWith(".part")), { timeout: 20_000 }).toBe(true)
    cli.proc.kill("SIGINT")
    expect(await cli.exited).toBe(130)
    expect(await readdir(out)).toEqual([])
    await expect(sender.getByText("Failed", { exact: true })).toBeVisible({ timeout: 20_000 })
  })
})

test.describe("long downloads", () => {
  test("a download that outlives Firefox's service-worker idle timeout still completes @firefox", async ({ browser, browserName }) => {
    // Regression: stock StreamSaver never started its service-worker heartbeat for transferable streams, so Firefox
    // terminated the idle worker 30-60 s into a download and the transfer froze. The server's relay is throttled to
    // ~5 MB/s in this suite, so 400 MB takes ~80 s, comfortably past that window.
    test.skip(browserName !== "firefox", "Chromium does not reap the worker; this guards the Firefox-only failure")
    test.setTimeout(6 * 60_000)
    const SIZE = 400 * 1024 * 1024
    const dir = await mkdtemp(join(tmpdir(), "wd-e2e-long-"))
    const src = join(dir, "long.bin")
    execFileSync("sh", ["-c", `head -c ${SIZE} /dev/urandom > "${src}"`])
    const hash = (p: string) =>
      new Promise<string>((res, rej) => {
        const h = createHash("sha256")
        createReadStream(p).on("data", (d) => h.update(d)).on("end", () => res(h.digest("hex").slice(0, 16))).on("error", rej)
      })

    try {
      const cli = runCli(["send", src])
      const [, room, secret] = await cli.waitFor(CODE_RE)
      const rx = await openReceiver(await browser.newContext(), `${BASE}/r/${room}?transport=relay#${secret}`)
      const [download] = await Promise.all([rx.waitForEvent("download", { timeout: 30_000 }), acceptButton(rx).click()])
      const t0 = Date.now()
      // the app's own completion message is the signal: download.path() can return early for a streamed Firefox download
      await expect(rx.getByText(/Received 1 file/)).toBeVisible({ timeout: 5 * 60_000 })
      expect(Date.now() - t0, "must have run past Firefox's ~30-60 s service-worker window to prove anything").toBeGreaterThan(65_000)
      const path = await download.path()
      for (let i = 0; i < 120 && statSync(path).size < SIZE; i++) await new Promise((r) => setTimeout(r, 500))
      expect(statSync(path).size).toBe(SIZE)
      expect(await hash(path)).toBe(await hash(src))
      expect(await cli.exited).toBe(0)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  })
})
