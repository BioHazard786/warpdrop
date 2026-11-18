// Drivers that run the pure machines over a Transport: handshake, hashing, flow control, resume.
import { createXXHash64 } from "hash-wasm"
import {
  deriveKeys,
  mac,
  newEphemeral,
  toHex,
  fromHex,
  transcript,
  verifyMac,
  type Ephemeral,
} from "./handshake"
import {
  type REffect,
  type REvent,
  type RState,
  type SEffect,
  type SEvent,
  type SState,
  receiverInit,
  receiverStep,
  senderInit,
  senderStep,
} from "./machine"
import {
  ACK_EVERY,
  type CancelReason,
  type FileEntry,
  type Frame,
  HANDSHAKE_TIMEOUT_MS,
  HIGH_WATER,
  MAX_UNACKED,
  PROTOCOL_V,
  encodeFrame,
  parseFrame,
  sanitizeName,
} from "./protocol"
import { type Transport, type Wire, chunkSizeFor } from "./transport"

/** No frame for this long while a transfer is in flight means the link is dead (NAT timeout, sleeping laptop). */
export const STALL_MS = 30_000

/** A sink (the browser's download) that accepts nothing for this long is treated as broken, not slow. */
export const SINK_STALL_MS = 60_000

/** Rejects if `p` has not settled within `ms`, so a stuck download surfaces as an error instead of a silent hang. */
function bounded<T>(p: Promise<T>, ms: number, what: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout>
  const timeout = new Promise<never>((_, rej) => {
    timer = setTimeout(
      () => rej(new Error(`${what} stalled: the browser stopped accepting data`)),
      ms,
    )
  })
  return Promise.race([p, timeout]).finally(() => clearTimeout(timer))
}

export type Result =
  | { status: "done" }
  | { status: "failed"; reason: CancelReason; byPeer: boolean; error?: string } // error: local cause, when there is one
  | { status: "closed" } // transport lost; the caller may resume on a new one

// ------------------------------------------------------------------ inbox
/** Buffers incoming frames so the handshake can await them, then hands the rest to a handler in order. */
class Inbox {
  private q: (Wire | null)[] = []
  private waiter: ((w: Wire | null) => void) | null = null
  private handler: ((w: Wire | null) => void) | null = null
  constructor(t: Transport) {
    t.onframe = (f) => this.push(f)
    t.onclose = () => this.push(null)
  }
  private push(w: Wire | null) {
    if (this.handler) return this.handler(w)
    if (this.waiter) {
      const w2 = this.waiter
      this.waiter = null
      return w2(w)
    }
    this.q.push(w)
  }
  /** Next frame, `null` on close, `undefined` on timeout. */
  next(ms: number): Promise<Wire | null | undefined> {
    if (this.q.length) return Promise.resolve(this.q.shift()!)
    return new Promise((res) => {
      const t = setTimeout(() => ((this.waiter = null), res(undefined)), ms)
      this.waiter = (w) => (clearTimeout(t), res(w))
    })
  }
  setHandler(h: (w: Wire | null) => void) {
    this.handler = h
    for (const w of this.q.splice(0)) h(w)
  }
}

type HandshakeOutcome = { ok: true } | { ok: false; result: Result }
const authFailed: HandshakeOutcome = {
  ok: false,
  result: { status: "failed", reason: "auth_failed", byPeer: false },
}
const closedOut: HandshakeOutcome = { ok: false, result: { status: "closed" } }
const protocolFailed: HandshakeOutcome = {
  ok: false,
  result: { status: "failed", reason: "protocol_error", byPeer: false },
}

async function handshake(
  role: "sender" | "receiver",
  t: Transport,
  inbox: Inbox,
  roomId: string,
  secret: string,
  device: string,
): Promise<HandshakeOutcome> {
  const deadline = Date.now() + HANDSHAKE_TIMEOUT_MS
  const wait = async () => {
    const w = await inbox.next(Math.max(1, deadline - Date.now()))
    if (w === undefined || w === null) return null
    if (typeof w !== "string") return "bad" as const
    return parseFrame(w) ?? ("bad" as const)
  }
  const eph: Ephemeral = await newEphemeral()
  t.send(encodeFrame({ t: "hello", v: PROTOCOL_V, client: "web/2", device, pub: toHex(eph.pub) }))

  const hello = await wait()
  if (hello === null) return closedOut
  if (hello === "bad") return protocolFailed
  if (hello.t === "cancel")
    return { ok: false, result: { status: "failed", reason: hello.reason, byPeer: true } }
  if (hello.t !== "hello" || hello.v !== PROTOCOL_V) return protocolFailed
  const peerPub = fromHex(hello.pub)
  if (peerPub.length !== 32) return protocolFailed

  const keys = await deriveKeys(secret, roomId, eph, peerPub)
  const [senderPub, receiverPub] = role === "sender" ? [eph.pub, peerPub] : [peerPub, eph.pub]
  const [senderFp, receiverFp] =
    role === "sender"
      ? [t.localFingerprint(), t.remoteFingerprint()]
      : [t.remoteFingerprint(), t.localFingerprint()]
  const tr = transcript(senderPub, receiverPub, senderFp, receiverFp)

  if (role === "receiver")
    t.send(encodeFrame({ t: "auth", mac: await mac(keys.authKey, "recv", tr) }))
  const a = await wait()
  if (a === null) return closedOut
  if (a === "bad") return protocolFailed
  if (a.t === "cancel")
    return { ok: false, result: { status: "failed", reason: a.reason, byPeer: true } }
  if (a.t !== "auth") return protocolFailed
  const ok = await verifyMac(keys.authKey, role === "sender" ? "recv" : "send", tr, a.mac)
  if (!ok) {
    t.send(encodeFrame({ t: "cancel", reason: "auth_failed" }))
    return authFailed
  }
  if (role === "sender")
    t.send(encodeFrame({ t: "auth", mac: await mac(keys.authKey, "send", tr) }))
  await t.enableCrypto(keys.sessionKey, role)
  return { ok: true }
}

/** Tell the peer why (protocol errors only; auth failures were already announced) and close, so it never waits out the timeout. */
function failHandshake(t: Transport, result: Result): Result {
  if (result.status === "failed" && !result.byPeer && result.reason === "protocol_error") {
    t.send(encodeFrame({ t: "cancel", reason: "protocol_error" }))
  }
  if (result.status !== "closed") t.close()
  return result
}

// ------------------------------------------------------------------ sender
export interface FileSource extends FileEntry {
  /** Bytes [start, end) as a stream. */
  stream(start: number, end?: number): ReadableStream<Uint8Array>
}

export interface SenderOptions {
  transport: Transport
  roomId: string
  secret: string
  device: string
  files: FileSource[]
  onProgress?: (idx: number, bytes: number) => void
  onFileDone?: (idx: number) => void
}

export class SenderSession {
  private abort = new AbortController()
  private acked = 0
  private ackWaiters: (() => void)[] = []
  private resolve!: (r: Result) => void
  private state: SState
  private o: SenderOptions
  private t: Transport
  private result = new Promise<Result>((r) => (this.resolve = r))

  constructor(o: SenderOptions) {
    this.o = o
    this.t = o.transport
    this.state = senderInit(o.files)
  }

  /** Stop sending and tell the receiver. */
  cancel() {
    this.dispatch({ e: "cancel" })
  }

  async run(): Promise<Result> {
    const inbox = new Inbox(this.t)
    const hs = await handshake("sender", this.t, inbox, this.o.roomId, this.o.secret, this.o.device)
    if (!hs.ok) {
      this.finish(failHandshake(this.t, hs.result))
      return this.result
    }
    const manifest = encodeFrame({
      t: "manifest",
      files: this.o.files.map(({ id, name, size, type }) => ({ id, name, size, type })),
    })
    if (manifest.length > this.t.maxMessage) {
      // the peer's SCTP/relay limit: it could never receive this frame, so say so instead of wedging the link
      this.finish(
        failHandshake(this.t, { status: "failed", reason: "protocol_error", byPeer: false }),
      )
      return this.result
    }
    this.t.send(manifest)
    inbox.setHandler((w) => {
      if (w === null) return this.dispatch({ e: "closed" })
      if (typeof w !== "string") return this.dispatch({ e: "bad" })
      const f = parseFrame(w)
      this.dispatch(f ? { e: "frame", f } : { e: "bad" })
    })
    return this.result
  }

  private finish(r: Result) {
    this.abort.abort()
    for (const w of this.ackWaiters.splice(0)) w()
    this.resolve(r)
  }

  private dispatch(ev: SEvent) {
    const r = senderStep(this.state, ev)
    this.state = r.state
    for (const fx of r.fx) this.apply(fx)
  }

  private apply(fx: SEffect) {
    switch (fx.k) {
      case "send":
        this.t.send(encodeFrame(fx.f))
        break
      case "stream_file":
        void this.streamFile(fx.idx, fx.offset)
        break
      case "ack":
        this.acked = Math.max(this.acked, fx.bytes)
        for (const w of this.ackWaiters.splice(0)) w()
        break
      case "file_acked":
        this.o.onFileDone?.(fx.idx)
        break
      case "complete":
        this.finish({ status: "done" })
        this.t.close() // the receiver confirmed the last file and waits for this close before leaving, so nothing is lost
        break
      case "abort":
        if (fx.reason === "closed") this.finish({ status: "closed" })
        else {
          this.finish({ status: "failed", reason: fx.reason, byPeer: fx.byPeer })
          this.t.close()
        }
        break
    }
  }

  private async streamFile(idx: number, offset: number) {
    const file = this.o.files[idx]!
    const signal = this.abort.signal
    const hasher = await createXXHash64()
    hasher.init()
    this.acked = offset
    try {
      if (offset > 0) await this.rehash(file, offset, hasher, signal)
      this.t.send(encodeFrame({ t: "file_start", id: file.id, offset }))
      const size = chunkSizeFor(this.t)
      let sent = offset
      const reader = file.stream(offset).getReader()
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (signal.aborted) return
          if (done) break
          for (let i = 0; i < value.length; i += size) {
            const part = value.subarray(i, i + size)
            // flow control: receiver-confirmed window first, then the socket's own buffer
            while (sent - this.acked >= MAX_UNACKED && !signal.aborted) {
              const before = this.acked
              let timer: ReturnType<typeof setTimeout> | undefined
              await Promise.race([
                new Promise<void>((r) => this.ackWaiters.push(r)),
                new Promise<void>((r) => (timer = setTimeout(r, STALL_MS))),
              ])
              clearTimeout(timer)
              if (this.acked === before && !signal.aborted) return void this.t.close() // receiver went silent: end as "closed" so it can resume
            }
            if (this.t.buffered > HIGH_WATER) await this.t.drain()
            if (signal.aborted) return
            this.t.send(part)
            hasher.update(part)
            sent += part.length
            this.o.onProgress?.(idx, sent)
          }
        }
      } finally {
        void reader.cancel().catch(() => {})
      }
      if (sent !== file.size) throw new Error("file changed while sending")
      this.t.send(encodeFrame({ t: "file_end", id: file.id, hash: hasher.digest("hex") }))
      this.dispatch({ e: "streamed", idx })
    } catch {
      if (!signal.aborted) this.dispatch({ e: "cancel" }) // unreadable file: tell the receiver instead of hanging it
    }
  }

  private async rehash(
    file: FileSource,
    upTo: number,
    hasher: { update(b: Uint8Array): unknown },
    signal: AbortSignal,
  ) {
    const reader = file.stream(0, upTo).getReader()
    for (;;) {
      const { done, value } = await reader.read()
      if (done || signal.aborted) return
      hasher.update(value)
    }
  }
}

// ------------------------------------------------------------------ receiver
export interface FileWriter {
  write(chunk: Uint8Array): Promise<void>
  close(): Promise<void>
  abort(): Promise<void>
}

export interface Sink {
  /** Called once per file, in order. `offset` is always 0 unless the writer is being kept across a resume. */
  open(file: FileEntry & { safeName: string }, idx: number): Promise<FileWriter>
}

/** Receiver state that survives transport changes so a transfer can resume. */
export class ReceiveState {
  manifest: string | null = null
  files: (FileEntry & { safeName: string })[] = []
  accepted = false
  nextIdx = 0
  cur: {
    idx: number
    bytes: number
    writer: FileWriter
    hasher: Awaited<ReturnType<typeof createXXHash64>>
  } | null = null
  sinceAck = 0

  /** Give up for good: abort the partial download (resuming keeps it; abandoning must not). */
  discard() {
    void this.cur?.writer.abort().catch(() => {})
    this.cur = null
  }
}

export interface ReceiverOptions {
  transport: Transport
  roomId: string
  secret: string
  device: string
  sink: Sink
  state: ReceiveState
  /** Ask the user. Called once per transfer, not again on resume. */
  onOffer: (files: (FileEntry & { safeName: string })[]) => Promise<boolean>
  onProgress?: (idx: number, bytes: number) => void
  onFileDone?: (idx: number) => void
  /** Overrides SINK_STALL_MS (tests). */
  sinkTimeoutMs?: number
}

export class ReceiverSession {
  private rs: RState = receiverInit
  private resolve!: (r: Result) => void
  private result = new Promise<Result>((r) => (this.resolve = r))
  private chain: Promise<void> = Promise.resolve()
  private onPeerClosed!: () => void // declared first: a later field declaration would reset the assignment below
  private peerClosed = new Promise<void>((r) => (this.onPeerClosed = r))
  private o: ReceiverOptions
  private t: Transport
  private st: ReceiveState

  constructor(o: ReceiverOptions) {
    this.o = o
    this.t = o.transport
    this.st = o.state
  }

  /** After finishing, wait (briefly) for the sender to close so our last frame is flushed before we hang up. */
  waitPeerClose(ms: number): Promise<void> {
    return Promise.race([this.peerClosed, new Promise<void>((r) => setTimeout(r, ms))])
  }

  cancel() {
    this.enqueue({ e: "cancel" })
  }

  async run(): Promise<Result> {
    const inbox = new Inbox(this.t)
    const hs = await handshake(
      "receiver",
      this.t,
      inbox,
      this.o.roomId,
      this.o.secret,
      this.o.device,
    )
    if (!hs.ok) {
      this.resolve(failHandshake(this.t, hs.result))
      return this.result
    }
    let last = Date.now()
    const watchdog = setInterval(() => {
      const busy =
        this.rs.s === "streaming" || this.rs.s === "await_start" || this.rs.s === "verifying"
      if (busy && Date.now() - last > STALL_MS) this.t.close() // not while the user is still deciding
    }, 5000)
    void this.result.finally(() => clearInterval(watchdog))
    inbox.setHandler((w) => {
      last = Date.now()
      if (w === null) {
        this.onPeerClosed()
        return this.enqueue({ e: "closed" })
      }
      if (typeof w !== "string") return this.enqueue({ e: "chunk", n: w.length }, w)
      const f = parseFrame(w)
      this.enqueue(f ? { e: "frame", f } : { e: "bad" })
    })
    return this.result
  }

  /** Frames are processed strictly in order, each fully awaited, so a slow sink delays acks and therefore the sender. */
  private enqueue(ev: REvent, chunk?: Uint8Array) {
    this.chain = this.chain
      .then(() => this.step(ev, chunk))
      .catch((e) => this.fail("protocol_error", e instanceof Error ? e.message : String(e)))
  }

  private async step(ev: REvent, chunk?: Uint8Array) {
    const before = this.rs
    const r = receiverStep(this.rs, ev)
    this.rs = r.state
    if (chunk && r.state.s === "streaming" && before.s === "streaming") await this.writeChunk(chunk)
    for (const fx of r.fx) await this.apply(fx)
  }

  private async writeChunk(chunk: Uint8Array) {
    const cur = this.st.cur!
    await bounded(
      cur.writer.write(chunk),
      this.o.sinkTimeoutMs ?? SINK_STALL_MS,
      "writing the download",
    )
    cur.hasher.update(chunk)
    cur.bytes += chunk.length
    this.st.sinceAck += chunk.length
    this.o.onProgress?.(cur.idx, cur.bytes)
    if (this.st.sinceAck >= ACK_EVERY) {
      this.st.sinceAck = 0
      this.t.send(encodeFrame({ t: "ack", id: this.st.files[cur.idx]!.id, bytes: cur.bytes }))
    }
  }

  private async apply(fx: REffect) {
    const st = this.st
    switch (fx.k) {
      case "send":
        this.t.send(encodeFrame(fx.f))
        break
      case "offer": {
        const key = JSON.stringify(fx.files.map(({ id, name, size }) => [id, name, size]))
        if (st.manifest !== null && st.manifest !== key) return this.fail("protocol_error") // manifest changed mid-transfer
        const first = st.manifest === null
        st.manifest = key
        st.files = fx.files
        const ok = st.accepted || (first && (await this.o.onOffer(fx.files)))
        if (!ok) return void this.enqueue({ e: "decline" })
        st.accepted = true
        const resume = st.cur
          ? { idx: st.cur.idx, offset: st.cur.bytes }
          : { idx: st.nextIdx, offset: 0 }
        this.enqueue({ e: "accept", resume })
        break
      }
      case "begin_file": {
        if (!(st.cur && st.cur.idx === fx.idx && st.cur.bytes === fx.offset)) {
          if (fx.offset !== 0) return this.fail("protocol_error")
          const writer = await this.o.sink.open(st.files[fx.idx]!, fx.idx)
          const hasher = await createXXHash64()
          hasher.init()
          st.cur = { idx: fx.idx, bytes: 0, writer, hasher }
          st.sinceAck = 0
        }
        break
      }
      case "verify": {
        const cur = st.cur!
        this.enqueue({ e: "verified", ok: cur.hasher.digest("hex") === fx.hash })
        break
      }
      case "finish_file": {
        await bounded(
          st.cur!.writer.close(),
          this.o.sinkTimeoutMs ?? SINK_STALL_MS,
          "finishing the download",
        )
        st.cur = null
        st.nextIdx = fx.idx + 1
        this.o.onFileDone?.(fx.idx)
        break
      }
      case "complete":
        this.resolve({ status: "done" })
        break
      case "abort":
        if (fx.reason === "closed") return void this.resolve({ status: "closed" }) // keep writers for a resume
        await st.cur?.writer.abort().catch(() => {})
        st.cur = null
        this.resolve({ status: "failed", reason: fx.reason, byPeer: fx.byPeer })
        this.t.close()
        break
    }
  }

  private fail(reason: CancelReason, error?: string) {
    this.t.send(encodeFrame({ t: "cancel", reason }))
    void this.st.cur?.writer.abort().catch(() => {})
    this.st.cur = null
    this.resolve({ status: "failed", reason, byPeer: false, error })
    this.t.close()
  }
}

export { sanitizeName }
export type { Frame }
