import { describe, expect, it } from "vitest"
import { xxhash64 } from "hash-wasm"
import {
  type FileSource,
  type FileWriter,
  ReceiveState,
  ReceiverSession,
  type Result,
  SenderSession,
  type Sink,
} from "./session"
import { MiB } from "./protocol"
import { type MemoryTransport, memoryPair } from "./transport"

const SECRET = "amber-otter-kite-moth"
const ROOM = "4821"

function fileFrom(id: number, name: string, bytes: Uint8Array): FileSource {
  return {
    id,
    name,
    size: bytes.length,
    type: "",
    stream(start, end = bytes.length) {
      let o = start
      return new ReadableStream({
        pull(c) {
          if (o >= end) return c.close()
          const n = Math.min(100_000, end - o) // odd size so slicing is exercised
          c.enqueue(bytes.subarray(o, o + n))
          o += n
        },
      })
    },
  }
}

const pattern = (n: number, seed = 1) => {
  const b = new Uint8Array(n)
  for (let i = 0; i < n; i++) b[i] = (i * 31 + seed + (i >> 8)) & 255
  return b
}

class MemSink implements Sink {
  out = new Map<number, Uint8Array[]>()
  names: string[] = []
  aborted: number[] = []
  closed: number[] = []
  delay = 0
  async open(file: { safeName: string }, idx: number): Promise<FileWriter> {
    this.names[idx] = file.safeName
    const parts: Uint8Array[] = this.out.get(idx) ?? []
    this.out.set(idx, parts)
    return {
      write: async (c) => {
        if (this.delay) await new Promise((r) => setTimeout(r, this.delay))
        parts.push(c.slice())
      },
      close: async () => void this.closed.push(idx),
      abort: async () => void this.aborted.push(idx),
    }
  }
  bytes(idx: number) {
    const p = this.out.get(idx) ?? []
    const out = new Uint8Array(p.reduce((n, x) => n + x.length, 0))
    let o = 0
    for (const x of p) {
      out.set(x, o)
      o += x.length
    }
    return out
  }
}

interface Rig {
  s: SenderSession
  r: ReceiverSession
  sRes: Promise<Result>
  rRes: Promise<Result>
  st: MemoryTransport
  rt: MemoryTransport
  sink: MemSink
  state: ReceiveState
}

function rig(
  files: FileSource[],
  o: {
    secret?: string
    accept?: boolean
    sink?: MemSink
    state?: ReceiveState
    pair?: [MemoryTransport, MemoryTransport]
  } = {},
): Rig {
  const [st, rt] = o.pair ?? memoryPair()
  const sink = o.sink ?? new MemSink()
  const state = o.state ?? new ReceiveState()
  const s = new SenderSession({ transport: st, roomId: ROOM, secret: SECRET, device: "s", files })
  const r = new ReceiverSession({
    transport: rt,
    roomId: ROOM,
    secret: o.secret ?? SECRET,
    device: "r",
    sink,
    state,
    onOffer: async () => o.accept ?? true,
  })
  return { s, r, sRes: s.run(), rRes: r.run(), st, rt, sink, state }
}

describe("session over an in-memory transport", () => {
  it("transfers zero-byte, duplicate-named and multi-chunk files with matching bytes", async () => {
    const a = pattern(3 * MiB + 17, 1),
      b = pattern(1000, 2)
    const files = [
      fileFrom(0, "same.txt", a),
      fileFrom(1, "same.txt", b),
      fileFrom(2, "empty", new Uint8Array()),
    ]
    const x = rig(files)
    expect(await x.sRes).toEqual({ status: "done" })
    expect(await x.rRes).toEqual({ status: "done" })
    expect(await xxhash64(x.sink.bytes(0))).toBe(await xxhash64(a))
    expect(x.sink.bytes(0)).toEqual(a)
    expect(x.sink.bytes(1)).toEqual(b)
    expect(x.sink.bytes(2).length).toBe(0)
    expect(x.sink.closed).toEqual([0, 1, 2])
  })

  it("moves a file larger than the 32 MiB window (needs receiver acks) with a slow sink", async () => {
    const big = pattern(40 * MiB)
    const x = rig([fileFrom(0, "big.bin", big)])
    expect(await x.sRes).toEqual({ status: "done" })
    expect(await x.rRes).toEqual({ status: "done" })
    expect(x.sink.bytes(0).length).toBe(big.length)
    expect(await xxhash64(x.sink.bytes(0))).toBe(await xxhash64(big))
  }, 30_000)

  it("sanitizes hostile names before they reach the sink", async () => {
    const x = rig([fileFrom(0, "../../etc/passwd", pattern(5))])
    await x.sRes
    expect(x.sink.names[0]).toBe("passwd")
  })

  it("fails both sides with auth_failed on a wrong secret and sends no manifest", async () => {
    const x = rig([fileFrom(0, "x", pattern(10))], { secret: "wrong-wrong-wrong-wrong" })
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "auth_failed" })
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "auth_failed" })
    expect(x.sink.out.size).toBe(0)
  })

  it("binds DTLS fingerprints: a middle-man with different fingerprints fails auth", async () => {
    const [st, rt] = memoryPair({ fingerprints: true })
    // receiver sees a different sender fingerprint than the sender actually has
    const lie = new Uint8Array(32).fill(0xcc)
    rt.remoteFingerprint = () => lie
    const x = rig([fileFrom(0, "x", pattern(10))], { pair: [st, rt] })
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "auth_failed" })
    expect(await x.rRes).toMatchObject({ status: "failed" })
  })

  it("decline cancels the sender", async () => {
    const x = rig([fileFrom(0, "x", pattern(10))], { accept: false })
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "declined", byPeer: false })
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "declined", byPeer: true })
  })

  it("detects a corrupted chunk by hash, aborts the writer and cancels the sender", async () => {
    const data = pattern(500_000)
    const [st, rt] = memoryPair()
    let flipped = false
    st.tamper = (f) => {
      if (typeof f !== "string" && !flipped) {
        flipped = true
        const c = f.slice()
        c[0]! ^= 0xff
        return c
      }
      return f
    }
    const x = rig([fileFrom(0, "x", data)], { pair: [st, rt] })
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "hash_mismatch" })
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "hash_mismatch", byPeer: true })
    expect(x.sink.aborted).toEqual([0])
    expect(x.sink.closed).toEqual([])
  })

  it("rejects a sender that sends more bytes than declared", async () => {
    const [st, rt] = memoryPair()
    const x = rig([fileFrom(0, "x", pattern(100))], { pair: [st, rt] })
    // after the real transfer starts, inject an extra frame as a rogue sender would
    const orig = st.send.bind(st)
    st.send = (f) => {
      orig(f)
      if (typeof f !== "string") orig(new Uint8Array(50))
    }
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "protocol_error" })
    expect(x.sink.aborted).toEqual([0])
  })

  it("sender cancel stops streaming and the receiver sees it", async () => {
    const x = rig([fileFrom(0, "big", pattern(20 * MiB))])
    x.sink.delay = 1
    const start = Date.now()
    while (x.sink.bytes(0).length < MiB && Date.now() - start < 5000)
      await new Promise((r) => setTimeout(r, 5))
    x.s.cancel()
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "user", byPeer: false })
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "user", byPeer: true })
    const sent = x.st.sentBytes
    await new Promise((r) => setTimeout(r, 100))
    expect(x.st.sentBytes).toBe(sent) // really stopped
    expect(x.sink.aborted).toEqual([0])
  })

  it("receiver cancel stops the sender", async () => {
    const x = rig([fileFrom(0, "big", pattern(20 * MiB))])
    x.sink.delay = 1
    while (x.sink.bytes(0).length < MiB) await new Promise((r) => setTimeout(r, 5))
    x.r.cancel()
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "user", byPeer: false })
    expect(await x.sRes).toMatchObject({ status: "failed", reason: "user", byPeer: true })
  })

  it("a dropped transport ends both sessions as 'closed' instead of hanging", async () => {
    const x = rig([fileFrom(0, "big", pattern(20 * MiB))])
    x.sink.delay = 1
    while (x.sink.bytes(0).length < MiB) await new Promise((r) => setTimeout(r, 5))
    x.st.close()
    expect(await x.sRes).toEqual({ status: "closed" })
    expect(await x.rRes).toEqual({ status: "closed" })
    expect(x.sink.aborted).toEqual([]) // writer kept for resume
  })

  it("resumes mid-file on a new transport from the last written byte and still verifies the whole file", async () => {
    const data = pattern(6 * MiB + 123)
    const files = [fileFrom(0, "a.bin", data), fileFrom(1, "b.bin", pattern(2000, 9))]
    const sink = new MemSink()
    const state = new ReceiveState()
    let offers = 0
    const pair = memoryPair()
    pair[0].tamper = (f) => {
      if (pair[0].sentBytes >= 2 * MiB) {
        pair[0].close() // the link dies after ~2 MiB, mid-file
        return null
      }
      return f
    }
    const first = rig(files, { sink, state, pair })
    expect(await first.rRes).toEqual({ status: "closed" })
    await first.sRes
    const have = state.cur!.bytes
    expect(have).toBeGreaterThanOrEqual(2 * MiB)
    expect(have).toBeLessThan(data.length)

    sink.delay = 0
    const [st2, rt2] = memoryPair()
    const s2 = new SenderSession({
      transport: st2,
      roomId: ROOM,
      secret: SECRET,
      device: "s",
      files,
    })
    const r2 = new ReceiverSession({
      transport: rt2,
      roomId: ROOM,
      secret: SECRET,
      device: "r",
      sink,
      state,
      onOffer: async () => (offers++, true),
    })
    const [a, b] = await Promise.all([s2.run(), r2.run()])
    expect(a).toEqual({ status: "done" })
    expect(b).toEqual({ status: "done" })
    expect(offers).toBe(0) // no second prompt
    expect(sink.bytes(0)).toEqual(data)
    expect(sink.bytes(1)).toEqual(pattern(2000, 9))
    expect(st2.sentBytes).toBeLessThan(data.length - have + 4000) // only the remainder (plus control frames) was re-sent
  }, 30_000)

  it("a peer that sends a binary frame before the handshake completes is rejected", async () => {
    const [st, rt] = memoryPair()
    const x = rig([fileFrom(0, "x", pattern(10))], { pair: [st, rt] })
    st.send(new Uint8Array(10))
    const [a, b] = await Promise.all([x.sRes, x.rRes])
    expect(a.status === "failed" || b.status === "failed").toBe(true)
  })
})

describe("handshake failure never leaves the peer waiting", () => {
  it("a sender-side protocol error during the handshake closes the link for the receiver too", async () => {
    const [st, rt] = memoryPair()
    const x = rig([fileFrom(0, "x", pattern(10))], { pair: [st, rt] })
    rt.send(new Uint8Array(10)) // receiver injects a binary frame into the sender's handshake
    const [a, b] = await Promise.all([x.sRes, x.rRes])
    expect(a.status).toBe("failed")
    expect(b.status === "failed" || b.status === "closed").toBe(true)
  })
})

describe("driver faults fail loudly", () => {
  it("a sink that throws on open ends both sides instead of hanging", async () => {
    const sink = new MemSink()
    sink.open = async () => {
      throw new Error("disk full")
    }
    const x = rig([fileFrom(0, "x", pattern(10))], { sink })
    expect(await x.rRes).toMatchObject({ status: "failed", reason: "protocol_error" })
    expect(await x.sRes).toMatchObject({ status: "failed", byPeer: true })
  })
})

describe("graceful shutdown", () => {
  it("the sender closes after the last confirmation and the receiver sees it, so the final frame is never cut off", async () => {
    const x = rig([fileFrom(0, "x", pattern(1000))])
    expect(await x.sRes).toEqual({ status: "done" })
    expect(await x.rRes).toEqual({ status: "done" })
    const t0 = Date.now()
    await x.r.waitPeerClose(2000)
    expect(Date.now() - t0).toBeLessThan(500) // resolved by the sender's close, not by the timeout
    expect(x.st.closed).toBe(true)
  })
})

describe("large manifests", () => {
  it("sends 2,000 files (about 170 kB of manifest) and refuses a list that cannot fit one message", async () => {
    const many = Array.from({ length: 2000 }, (_, i) => fileFrom(i, `f${i}.bin`, pattern(1, i)))
    const x = rig(many)
    expect(await x.sRes).toEqual({ status: "done" })
    expect(await x.rRes).toEqual({ status: "done" })
    expect(x.sink.names.length).toBe(2000)

    const tooMany = Array.from({ length: 6000 }, (_, i) => fileFrom(i, `f${i}.bin`, pattern(1, i)))
    const y = rig(tooMany)
    expect(await y.sRes).toMatchObject({ status: "failed", reason: "protocol_error" })
    expect((await y.rRes).status).toBe("failed")
  }, 60_000)
})

describe("giving up", () => {
  it("discard() aborts the partial download that a resume would have kept", async () => {
    const s = new MemSink()
    s.delay = 1
    const pair = memoryPair()
    pair[0].tamper = (f) => {
      if (pair[0].sentBytes >= MiB) {
        pair[0].close()
        return null
      }
      return f
    }
    const x = rig([fileFrom(0, "big", pattern(8 * MiB))], { sink: s, pair })
    expect(await x.rRes).toEqual({ status: "closed" })
    expect(s.aborted).toEqual([]) // kept for a resume
    x.state.discard()
    expect(s.aborted).toEqual([0]) // abandoned: the browser's download now fails instead of hanging
    expect(x.state.cur).toBeNull()
  })
})
