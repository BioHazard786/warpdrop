// Spawns the real Go server and runs full transfers over the encrypted WebSocket relay (no browser needed).
import { type ChildProcess, execFileSync, spawn } from "node:child_process"
import { mkdtempSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { fileURLToPath } from "node:url"
import { afterAll, beforeAll, describe, expect, it } from "vitest"
import { MiB } from "./protocol"
import { RelayTransport } from "./relay-transport"
import {
  type FileSource,
  type FileWriter,
  ReceiveState,
  ReceiverSession,
  SenderSession,
  type Sink,
} from "./session"

const BACKEND = fileURLToPath(new URL("../../../backend/", import.meta.url))
const SECRET = "amber-otter-kite-moth"
let proc: ChildProcess
let base = ""
let ready = false

beforeAll(async () => {
  try {
    const dir = mkdtempSync(join(tmpdir(), "wd-"))
    const bin = join(dir, "server")
    execFileSync("go", ["build", "-o", bin, "./cmd/server"], { cwd: BACKEND, stdio: "pipe" })
    const port = 20000 + Math.floor(Math.random() * 20000)
    proc = spawn(bin, [], { env: { ...process.env, PORT: String(port) }, stdio: "ignore" })
    base = `127.0.0.1:${port}`
    for (let i = 0; i < 50 && !ready; i++) {
      ready = await fetch(`http://${base}/healthz`).then(
        (r) => r.ok,
        () => false,
      )
      if (!ready) await new Promise((r) => setTimeout(r, 100))
    }
  } catch {
    ready = false
  }
}, 60_000)

afterAll(() => proc?.kill())

class Ws {
  ws!: WebSocket
  q: Record<string, unknown>[] = []
  waiters: ((m: Record<string, unknown>) => void)[] = []
  static async open(path: string) {
    const c = new Ws()
    c.ws = new WebSocket(`ws://${base}${path}`)
    c.ws.onmessage = (e) => {
      const m = JSON.parse(e.data as string)
      const w = c.waiters.shift()
      if (w) w(m)
      else c.q.push(m)
    }
    await new Promise<void>(
      (res, rej) => ((c.ws.onopen = () => res()), (c.ws.onerror = () => rej(new Error("ws")))),
    )
    return c
  }
  send(m: object) {
    this.ws.send(JSON.stringify(m))
  }
  next(): Promise<Record<string, unknown>> {
    return this.q.length
      ? Promise.resolve(this.q.shift()!)
      : new Promise((r) => this.waiters.push(r))
  }
  async want(t: string) {
    const m = await this.next()
    if (m.t !== t) throw new Error(`wanted ${t}, got ${JSON.stringify(m)}`)
    return m
  }
}

async function pair() {
  const s = await Ws.open("/ws")
  s.send({ t: "create_room", v: 2, client: {} })
  const roomId = (await s.want("room_created")).room_id as string
  const r = await Ws.open("/ws")
  r.send({ t: "join_room", v: 2, room_id: roomId, client: {} })
  await r.want("joined")
  const peer = (await s.want("peer_joined")).peer_id as string
  return { s, r, roomId, peer }
}

async function relayPair(p: Awaited<ReturnType<typeof pair>>) {
  p.r.send({ t: "relay_request", to: "sender" })
  const ts = await p.s.want("relay_ticket")
  const tr = await p.r.want("relay_ticket")
  expect(ts.ticket).toBe(tr.ticket)
  const [st, rt] = await Promise.all([
    RelayTransport.open(`ws://${base}/relay/${ts.ticket}`),
    RelayTransport.open(`ws://${base}/relay/${tr.ticket}`),
  ])
  return [st, rt] as const
}

const pattern = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 7 + (i >> 9)) & 255)
const src = (id: number, name: string, b: Uint8Array): FileSource => ({
  id,
  name,
  size: b.length,
  type: "",
  stream: (start, end = b.length) =>
    new ReadableStream({
      start(c) {
        c.enqueue(b.subarray(start, end))
        c.close()
      },
    }),
})
class Mem implements Sink {
  parts: Uint8Array[][] = []
  async open(_f: unknown, idx: number): Promise<FileWriter> {
    const p = (this.parts[idx] ??= [])
    return {
      write: async (c) => void p.push(c.slice()),
      close: async () => {},
      abort: async () => {},
    }
  }
  bytes(i: number) {
    return Uint8Array.from((this.parts[i] ?? []).flatMap((c) => [...c]))
  }
}

describe.skipIf(false)("relay end-to-end against the Go server", () => {
  it("transfers files through the relay and the wire carries only ciphertext", async () => {
    expect(ready, "Go server did not start (is `go` installed?)").toBe(true)
    const sent: (string | ArrayBufferLike | ArrayBufferView)[] = []
    const orig = WebSocket.prototype.send
    WebSocket.prototype.send = function (d: never) {
      sent.push(d)
      return orig.call(this, d)
    }
    try {
      const p = await pair()
      const [st, rt] = await relayPair(p)
      const secretName = "tax-return-2026.pdf"
      const data = pattern(2 * MiB + 5)
      const files = [src(0, secretName, data), src(1, "empty", new Uint8Array())]
      const sink = new Mem()
      const sres = new SenderSession({
        transport: st,
        roomId: p.roomId,
        secret: SECRET,
        device: "s",
        files,
      }).run()
      const rres = new ReceiverSession({
        transport: rt,
        roomId: p.roomId,
        secret: SECRET,
        device: "r",
        sink,
        state: new ReceiveState(),
        onOffer: async () => true,
      }).run()
      expect(await sres).toEqual({ status: "done" })
      expect(await rres).toEqual({ status: "done" })
      expect(sink.bytes(0)).toEqual(data)

      // after the handshake, nothing on the wire may contain the file name or file bytes
      const probe = data.subarray(1000, 1064)
      const needle = (hay: Uint8Array, n: Uint8Array) =>
        hay.some((_, i) => i + n.length <= hay.length && n.every((b, j) => hay[i + j] === b))
      const nameBytes = new TextEncoder().encode(secretName)
      // positive control: the detector must actually find plaintext when it is there
      expect(needle(new TextEncoder().encode(`xx${secretName}yy`), nameBytes)).toBe(true)
      let frames = 0
      for (const d of sent) {
        const u8 =
          typeof d === "string"
            ? new TextEncoder().encode(d)
            : d instanceof ArrayBuffer
              ? new Uint8Array(d)
              : new Uint8Array(
                  (d as ArrayBufferView).buffer,
                  (d as ArrayBufferView).byteOffset,
                  (d as ArrayBufferView).byteLength,
                )
        frames++
        expect(needle(u8, nameBytes)).toBe(false)
        if (u8.length > 200) expect(needle(u8, probe)).toBe(false)
      }
      expect(frames).toBeGreaterThan(8)
    } finally {
      WebSocket.prototype.send = orig
    }
  }, 30_000)

  it("a wrong secret fails authentication on both sides through the relay", async () => {
    expect(ready).toBe(true)
    const p = await pair()
    const [st, rt] = await relayPair(p)
    const sink = new Mem()
    const sres = new SenderSession({
      transport: st,
      roomId: p.roomId,
      secret: SECRET,
      device: "s",
      files: [src(0, "x", pattern(10))],
    }).run()
    const rres = new ReceiverSession({
      transport: rt,
      roomId: p.roomId,
      secret: "wrong-wrong-wrong-wrong",
      device: "r",
      sink,
      state: new ReceiveState(),
      onOffer: async () => true,
    }).run()
    expect(await sres).toMatchObject({ status: "failed", reason: "auth_failed" })
    expect(await rres).toMatchObject({ status: "failed", reason: "auth_failed" })
    expect(sink.parts.length).toBe(0)
  }, 30_000)

  it("resumes on a second relay connection after the first is cut mid-file", async () => {
    expect(ready).toBe(true)
    const p = await pair()
    const data = pattern(6 * MiB)
    const files = [src(0, "big", data)]
    const sink = new Mem()
    const state = new ReceiveState()
    const [st, rt] = await relayPair(p)
    const s1 = new SenderSession({
      transport: st,
      roomId: p.roomId,
      secret: SECRET,
      device: "s",
      files,
    }).run()
    let cut = false
    const r1 = new ReceiverSession({
      transport: rt,
      roomId: p.roomId,
      secret: SECRET,
      device: "r",
      sink,
      state,
      onOffer: async () => true,
      onProgress: (_i, b) => {
        if (b > MiB && !cut) {
          cut = true
          rt.close()
        }
      },
    }).run()
    expect(await r1).toEqual({ status: "closed" })
    await s1
    expect(state.cur!.bytes).toBeGreaterThan(0)

    const [st2, rt2] = await relayPair(p)
    const s2 = new SenderSession({
      transport: st2,
      roomId: p.roomId,
      secret: SECRET,
      device: "s",
      files,
    }).run()
    const r2 = new ReceiverSession({
      transport: rt2,
      roomId: p.roomId,
      secret: SECRET,
      device: "r",
      sink,
      state,
      onOffer: async () => {
        throw new Error("must not prompt again")
      },
    }).run()
    expect(await s2).toEqual({ status: "done" })
    expect(await r2).toEqual({ status: "done" })
    expect(sink.bytes(0)).toEqual(data)
  }, 30_000)
})
