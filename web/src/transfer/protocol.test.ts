import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import { xxhash64 } from "hash-wasm"
import { type Frame, parseFrame, sanitizeName } from "./protocol"
import {
  receiverInit,
  receiverStep,
  senderInit,
  senderStep,
  type REffect,
  type REvent,
  type RState,
} from "./machine"
import { generateSecret, isValidSecret, parseShare } from "./words"

const fx = (n: string) =>
  JSON.parse(readFileSync(new URL(`../../../protocol/fixtures/${n}.json`, import.meta.url), "utf8"))
const transcripts = fx("transcripts") as Record<string, any>

describe("sanitizeName", () => {
  it("matches the shared hostile-names fixture", () => {
    const t = transcripts["hostile-names"]
    const files = t.steps[0].frame.files
    for (const e of t.expect.files)
      expect(sanitizeName(files.find((f: any) => f.id === e.id).name)).toBe(e.name)
  })
  it("never returns anything unsafe", () => {
    for (const bad of [
      "",
      ".",
      "..",
      "...",
      "a/../b",
      "C:\\x\\y",
      "NUL.txt",
      "aux",
      "x".repeat(400),
      "\u0000",
      "é".repeat(200),
    ]) {
      const n = sanitizeName(bad)
      expect(n).not.toMatch(/[/\\]|^\.+$/)
      expect([...n].every((c) => c.charCodeAt(0) > 31)).toBe(true)
      expect(new TextEncoder().encode(n).length).toBeLessThanOrEqual(255)
      expect(n.length).toBeGreaterThan(0)
    }
  })
})

describe("parseFrame", () => {
  it("accepts valid frames and rejects hostile ones", () => {
    expect(parseFrame('{"t":"request","id":0,"offset":0}')).toEqual({
      t: "request",
      id: 0,
      offset: 0,
    })
    for (const bad of [
      "not json",
      "{}",
      '{"t":"nope"}',
      '{"t":"request","id":-1,"offset":0}',
      '{"t":"request","id":0,"offset":1.5}',
      '{"t":"manifest","files":[]}',
      '{"t":"manifest","files":[{"id":0,"name":"a","size":-1,"type":""}]}',
      '{"t":"manifest","files":[{"id":0,"name":"a","size":1e400,"type":""}]}',
      '{"t":"manifest","files":[{"id":0,"name":"a","size":1,"type":""},{"id":0,"name":"b","size":1,"type":""}]}',
      '{"t":"file_end","id":0,"hash":"xyz"}',
      '{"t":"cancel","reason":"because"}',
    ])
      expect(parseFrame(bad), bad).toBeNull()
  })
})

describe("share codes", () => {
  it("generates valid secrets and parses codes and links", () => {
    const s = generateSecret()
    expect(isValidSecret(s)).toBe(true)
    expect(parseShare(`4821-${s}`)).toEqual({ roomId: "4821", secret: s })
    expect(parseShare(`https://x.example/r/4821#${s}`)).toEqual({ roomId: "4821", secret: s })
    expect(parseShare("https://x.example/r/4821")).toEqual({ roomId: "4821", secret: null })
    expect(parseShare("4821-notwords-here-at-all")).toBeNull()
    expect(parseShare("hello")).toBeNull()
  })
})

// Replays each shared transcript through the receiver machine, doing what a driver does for the effects.
async function replayReceiver(name: string) {
  const t = transcripts[name]
  let st: RState = receiverInit
  const sent: Frame[] = []
  const bytes: Record<number, number> = { ...t.receiverHasBytes }
  const hashes: Record<number, string> = {}
  const done: number[] = []
  let error: string | null = null
  let offered: any[] = []
  const hashers: Record<number, Uint8Array[]> = {}
  const run = async (ev: REvent) => {
    const q: REvent[] = [ev]
    while (q.length) {
      const r = receiverStep(st, q.shift()!)
      st = r.state
      for (const e of r.fx as REffect[]) {
        if (e.k === "send") sent.push(e.f)
        else if (e.k === "offer") offered = e.files
        else if (e.k === "begin_file") hashers[e.idx] = []
        else if (e.k === "verify") q.push({ e: "verified", ok: e.hash === hashes[e.idx] })
        else if (e.k === "finish_file") done.push(e.idx)
        else if (e.k === "abort") error = e.reason
      }
    }
  }
  const chunks: Record<number, Uint8Array[]> = {}
  // events that need data are computed up front from the transcript
  let current = -1
  for (const step of t.steps) {
    if (step.from !== "sender") continue
    if (step.frame) {
      const f = step.frame as Frame
      if (f.t === "file_start") {
        current = f.id
        chunks[current] = []
      }
      await run({ e: "frame", f })
      if (f.t === "manifest") {
        if (t.expect.error === "declined") await run({ e: "decline" })
        else
          await run({
            e: "accept",
            resume: t.receiverHasBytes ? { idx: 0, offset: t.receiverHasBytes[0] } : undefined,
          })
      }
    } else {
      const b = Buffer.from(step.bin, "hex")
      chunks[current]!.push(b)
      await run({ e: "chunk", n: b.length })
    }
    // compute the real hash lazily right before file_end is processed
    if (step.frame?.t === "file_start") hashes[step.frame.id] = ""
    const nxt = t.steps[t.steps.indexOf(step) + 1]
    if (nxt?.frame?.t === "file_end")
      hashes[nxt.frame.id] = await xxhash64(await fullBytes(t, nxt.frame.id, bytes))
  }
  void hashers
  return { st, sent, done, error, offered }
}

// reconstruct the file's full bytes from the transcript (resume transcripts only carry the tail)
async function fullBytes(t: any, id: number, has: Record<number, number>) {
  const parts: Buffer[] = []
  let cur = -1
  for (const s of t.steps) {
    if (s.frame?.t === "file_start") cur = s.frame.id
    if (s.bin && cur === id) parts.push(Buffer.from(s.bin, "hex"))
  }
  const tail = Buffer.concat(parts)
  // resume fixture: first 5 bytes were already received ("hello"), tail is " world"
  return has[id] ? Buffer.concat([Buffer.from("hello world").subarray(0, has[id]), tail]) : tail
}

describe("receiver machine replays shared transcripts", () => {
  it("happy path", async () => {
    const r = await replayReceiver("happy-path")
    expect(r.st.s).toBe("done")
    expect(r.done).toEqual([0, 1])
    expect(r.sent.map((f) => f.t)).toEqual(["request", "file_done", "request", "file_done"])
  })
  it("duplicate names keep two separate files", async () => {
    const r = await replayReceiver("duplicate-names")
    expect(r.st.s).toBe("done")
    expect(r.done).toEqual([0, 1])
  })
  it("decline sends cancel", async () => {
    const r = await replayReceiver("cancel-declined")
    expect(r.st).toMatchObject({ s: "failed", reason: "declined" })
    expect(r.sent).toEqual([{ t: "cancel", reason: "declined" }])
  })
  it("hash mismatch aborts and cancels", async () => {
    const r = await replayReceiver("hash-mismatch")
    expect(r.st).toMatchObject({ s: "failed", reason: "hash_mismatch" })
    expect(r.done).toEqual([])
    expect(r.sent.at(-1)).toEqual({ t: "cancel", reason: "hash_mismatch" })
  })
  it("resume requests from the held offset", async () => {
    const r = await replayReceiver("resume")
    expect(r.sent[0]).toEqual({ t: "request", id: 0, offset: 5 })
    expect(r.st.s).toBe("done")
  })
  it("overrun is a protocol error", async () => {
    const r = await replayReceiver("overrun")
    expect(r.st).toMatchObject({ s: "failed", reason: "protocol_error" })
  })
  it("offers sanitized names", async () => {
    const r = await replayReceiver("hostile-names")
    expect(r.offered.map((f) => f.safeName)).toEqual(
      transcripts["hostile-names"].expect.files.map((f: any) => f.name),
    )
  })
})

describe("receiver machine rejects out-of-order input", () => {
  const manifest: Frame = { t: "manifest", files: [{ id: 0, name: "a", size: 1, type: "" }] }
  const offered = () => receiverStep(receiverInit, { e: "frame", f: manifest }).state
  it("chunk before file_start, file_end with wrong byte count, wrong id", () => {
    expect(receiverStep(receiverInit, { e: "chunk", n: 1 }).state).toMatchObject({
      s: "failed",
      reason: "protocol_error",
    })
    const a = receiverStep(offered(), { e: "accept" }).state
    expect(receiverStep(a, { e: "chunk", n: 1 }).state).toMatchObject({ s: "failed" })
    const s = receiverStep(a, { e: "frame", f: { t: "file_start", id: 0, offset: 0 } }).state
    expect(
      receiverStep(s, { e: "frame", f: { t: "file_end", id: 0, hash: "0".repeat(16) } }).state,
    ).toMatchObject({ s: "failed" })
    expect(
      receiverStep(a, { e: "frame", f: { t: "file_start", id: 9, offset: 0 } }).state,
    ).toMatchObject({ s: "failed" })
    expect(
      receiverStep(a, { e: "frame", f: { t: "file_start", id: 0, offset: 1 } }).state,
    ).toMatchObject({ s: "failed" })
  })
  it("peer cancel and transport close end the transfer; terminal states are sticky", () => {
    const a = receiverStep(offered(), { e: "accept" }).state
    const c = receiverStep(a, { e: "frame", f: { t: "cancel", reason: "user" } })
    expect(c.state).toMatchObject({ s: "failed", reason: "user", byPeer: true })
    expect(receiverStep(c.state, { e: "chunk", n: 1 }).fx).toEqual([])
    expect(receiverStep(a, { e: "closed" }).state).toMatchObject({ s: "failed", reason: "closed" })
  })
})

describe("sender machine", () => {
  const files = [
    { id: 0, name: "a", size: 4, type: "" },
    { id: 1, name: "b", size: 0, type: "" },
  ]
  it("serves files in order and completes", () => {
    let r = senderStep(senderInit(files), { e: "frame", f: { t: "request", id: 0, offset: 0 } })
    expect(r.fx).toEqual([{ k: "stream_file", idx: 0, offset: 0 }])
    r = senderStep(r.state, { e: "streamed", idx: 0 })
    r = senderStep(r.state, { e: "frame", f: { t: "file_done", id: 0 } })
    expect(r.state.s).toBe("await_request")
    r = senderStep(r.state, { e: "frame", f: { t: "request", id: 1, offset: 0 } })
    r = senderStep(r.state, { e: "streamed", idx: 1 })
    r = senderStep(r.state, { e: "frame", f: { t: "file_done", id: 1 } })
    expect(r.state.s).toBe("done")
    expect(r.fx.at(-1)).toEqual({ k: "complete" })
  })
  it("rejects requests for unknown files or offsets past the end", () => {
    for (const f of [
      { t: "request", id: 9, offset: 0 },
      { t: "request", id: 0, offset: 5 },
    ] as Frame[])
      expect(senderStep(senderInit(files), { e: "frame", f }).state).toMatchObject({
        s: "failed",
        reason: "protocol_error",
      })
  })
  it("forwards acks only for the file in flight and honors resume re-request", () => {
    let r = senderStep(senderInit(files), { e: "frame", f: { t: "request", id: 0, offset: 0 } })
    expect(senderStep(r.state, { e: "frame", f: { t: "ack", id: 0, bytes: 4 } }).fx).toEqual([
      { k: "ack", id: 0, bytes: 4 },
    ])
    expect(senderStep(r.state, { e: "frame", f: { t: "ack", id: 1, bytes: 0 } }).fx).toEqual([])
    expect(senderStep(r.state, { e: "frame", f: { t: "ack", id: 0, bytes: 999 } }).fx).toEqual([])
    r = senderStep(r.state, { e: "streamed", idx: 0 })
    r = senderStep(r.state, { e: "frame", f: { t: "request", id: 0, offset: 2 } })
    expect(r.fx).toEqual([{ k: "stream_file", idx: 0, offset: 2 }])
  })
})
