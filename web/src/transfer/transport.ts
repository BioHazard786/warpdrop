import { CHUNK_SIZE } from "./protocol"

export type Wire = string | Uint8Array

/** An ordered, reliable channel carrying text and binary frames. Implementations: RTC data channel, WS relay, in-memory. */
export interface Transport {
  readonly kind: "rtc" | "relay" | "memory"
  /** Largest binary frame this transport can carry. */
  readonly maxMessage: number
  /** Bytes queued but not yet on the wire. */
  readonly buffered: number
  /** DTLS fingerprints (raw digest bytes) bound into the handshake transcript; null on non-DTLS transports. */
  localFingerprint(): Uint8Array | null
  remoteFingerprint(): Uint8Array | null
  send(frame: Wire): void | Promise<void>
  /** Resolves once `buffered` has fallen to the low-water mark. */
  drain(): Promise<void>
  /** Called after the handshake: relay transports start encrypting here, others ignore it. */
  enableCrypto(sessionKey: Uint8Array, role: "sender" | "receiver"): Promise<void>
  close(): void
  onframe: ((frame: Wire) => void) | null
  onclose: (() => void) | null
}

export const chunkSizeFor = (t: Transport) => Math.min(CHUNK_SIZE, t.maxMessage)

/** Test transport pair: delivers frames in order on a microtask, with a manual `drop()` to simulate a lost link. */
export function memoryPair(
  opts: { fingerprints?: boolean } = {},
): [MemoryTransport, MemoryTransport] {
  const a = new MemoryTransport(opts.fingerprints ? new Uint8Array(32).fill(0xaa) : null)
  const b = new MemoryTransport(opts.fingerprints ? new Uint8Array(32).fill(0xbb) : null)
  a.peer = b
  b.peer = a
  return [a, b]
}

export class MemoryTransport implements Transport {
  readonly kind = "memory" as const
  readonly maxMessage = CHUNK_SIZE
  buffered = 0
  peer!: MemoryTransport
  onframe: ((f: Wire) => void) | null = null
  onclose: (() => void) | null = null
  closed = false
  /** Optional hook to tamper with outgoing frames (tests). Return null to drop the frame. */
  tamper: ((f: Wire) => Wire | null) | null = null
  sentBytes = 0
  private fp: Uint8Array | null
  constructor(fp: Uint8Array | null) {
    this.fp = fp
  }
  localFingerprint() {
    return this.fp
  }
  remoteFingerprint() {
    return this.peer.fp
  }
  send(frame: Wire) {
    if (this.closed) return
    const out = this.tamper ? this.tamper(frame) : frame
    if (out === null) return
    this.sentBytes += typeof out === "string" ? out.length : out.length
    const copy = typeof out === "string" ? out : out.slice()
    queueMicrotask(() => {
      if (!this.peer.closed) this.peer.onframe?.(copy)
    })
  }
  drain() {
    return Promise.resolve()
  }
  enableCrypto() {
    return Promise.resolve()
  }
  close() {
    if (this.closed) return
    this.closed = true
    queueMicrotask(() => {
      this.onclose?.()
      if (!this.peer.closed) {
        this.peer.closed = true
        this.peer.onclose?.()
      }
    })
  }
}
