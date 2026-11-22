import { describe, expect, it } from "vitest"
import { toHex } from "./handshake"
import { parseFrame } from "./protocol"
import { type FileSource, ReceiveState, ReceiverSession, SenderSession, type Sink } from "./session"
import { type MemoryTransport, memoryPair } from "./transport"

const ROOM = "lobby"

const data = new Uint8Array(300_000).map((_, i) => (i * 31) & 255)
const file: FileSource = {
  id: 0,
  name: "n.bin",
  size: data.length,
  type: "",
  stream: (start, end = data.length) =>
    new ReadableStream({
      start(c) {
        c.enqueue(data.subarray(start, end))
        c.close()
      },
    }),
}

function sinkOf(into: Uint8Array[]): Sink {
  return {
    open: async () => ({
      write: async (c) => void into.push(c.slice()),
      close: async () => {},
      abort: async () => {},
    }),
  }
}

/** A nearby sender and receiver (empty secret) over the given transports. */
function nearby(st: MemoryTransport, rt: MemoryTransport, out: Uint8Array[] = []) {
  const state = new ReceiveState()
  let senderSas = ""
  const s = new SenderSession({
    transport: st,
    roomId: ROOM,
    secret: "",
    device: "Laptop",
    files: [file],
    onSas: (x) => (senderSas = x),
  })
  const r = new ReceiverSession({
    transport: rt,
    roomId: ROOM,
    secret: "",
    device: "Phone",
    sink: sinkOf(out),
    state,
    onOffer: async () => true,
  })
  return { sRes: s.run(), rRes: r.run(), state, sas: () => senderSas }
}

describe("nearby handshake (no secret words)", () => {
  it("transfers, and both sides show the same 6-digit code", async () => {
    const [st, rt] = memoryPair({ fingerprints: true })
    const out: Uint8Array[] = []
    const x = nearby(st, rt, out)
    expect((await x.sRes).status).toBe("done")
    expect((await x.rRes).status).toBe("done")
    expect(x.sas()).toMatch(/^\d{6}$/)
    expect(x.state.sas).toBe(x.sas())
    expect(out.reduce((n, c) => n + c.length, 0)).toBe(data.length)
  })

  it("two separate sessions get independent codes (what a man-in-the-middle would face)", async () => {
    // an attacker in the middle holds two separate sessions, one per side; the strong MITM check is the Go test
    const [s1, m1] = memoryPair()
    const [m2, r2] = memoryPair()
    const a = nearby(s1, m1) // sender <-> mallory (mallory acts as a receiver)
    const b = nearby(m2, r2) // mallory (acts as a sender) <-> receiver
    // each pair is a complete, honest nearby session: their codes are independent, so they do not match
    await Promise.all([a.sRes, a.rRes, b.sRes, b.rRes])
    expect(a.sas()).not.toBe(b.sas()) // 1-in-a-million flake is acceptable
  })

  it("rejects a reveal that does not match the commitment", async () => {
    const [st, rt] = memoryPair()
    const other = toHex(crypto.getRandomValues(new Uint8Array(32)))
    st.tamper = (f) =>
      typeof f === "string" && parseFrame(f)?.t === "reveal"
        ? JSON.stringify({ t: "reveal", pub: other })
        : f
    const x = nearby(st, rt)
    const r = await x.rRes
    expect(r).toMatchObject({ status: "failed", reason: "protocol_error" })
    await x.sRes
  })

  it("a code-less receiver refuses a sender that skips the commitment", async () => {
    const [st, rt] = memoryPair()
    const s = new SenderSession({
      transport: st,
      roomId: ROOM,
      secret: "amber-otter-kite-moth",
      device: "s",
      files: [file],
    })
    const r = new ReceiverSession({
      transport: rt,
      roomId: ROOM,
      secret: "",
      device: "r",
      sink: sinkOf([]),
      state: new ReceiveState(),
      onOffer: async () => true,
    })
    const [, rr] = await Promise.all([s.run(), r.run()])
    expect(rr).toMatchObject({ status: "failed", reason: "protocol_error" })
  })
})

describe("hello frames", () => {
  const base = { t: "hello", v: 2, client: "c", device: "d" }
  const h64 = "a".repeat(64)
  it.each([
    ["key only", { ...base, pub: h64 }, true],
    ["commit only", { ...base, pub: "", commit: h64 }, true],
    ["both", { ...base, pub: h64, commit: h64 }, false],
    ["neither", { ...base, pub: "" }, false],
  ])("%s", (_n, frame, ok) => {
    expect(parseFrame(JSON.stringify(frame)) !== null).toBe(ok)
  })
})
