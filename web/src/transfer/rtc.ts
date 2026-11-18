import { dtlsFingerprint } from "./handshake"
import { CHUNK_SIZE, LOW_WATER } from "./protocol"
import type { Transport, Wire } from "./transport"

export type SignalPayload =
  | { kind: "offer" | "answer"; sdp: string }
  | { kind: "candidate"; candidate: RTCIceCandidateInit | null }
  | { kind: "restart" }

export function isSignalPayload(p: unknown): p is SignalPayload {
  if (typeof p !== "object" || p === null) return false
  const k = (p as { kind?: unknown }).kind
  if (k === "offer" || k === "answer") return typeof (p as { sdp?: unknown }).sdp === "string"
  return k === "candidate" || k === "restart"
}

/** RTCDataChannel as a Transport. Fingerprints come from the negotiated SDP so the handshake binds to this DTLS session. */
export class RtcTransport implements Transport {
  readonly kind = "rtc" as const
  readonly maxMessage: number
  onframe: ((f: Wire) => void) | null = null
  onclose: (() => void) | null = null
  private closed = false

  private pc: RTCPeerConnection
  private dc: RTCDataChannel

  constructor(pc: RTCPeerConnection, dc: RTCDataChannel) {
    this.pc = pc
    this.dc = dc
    const m = pc.sctp?.maxMessageSize
    this.maxMessage = Math.min(CHUNK_SIZE, Number.isFinite(m) && m! >= 16 * 1024 ? m! : 64 * 1024)
    dc.binaryType = "arraybuffer"
    dc.bufferedAmountLowThreshold = LOW_WATER
    dc.onmessage = (e) =>
      this.onframe?.(typeof e.data === "string" ? e.data : new Uint8Array(e.data as ArrayBuffer))
    dc.onclose = () => this.shutdown()
    dc.onerror = () => this.shutdown()
    pc.addEventListener("connectionstatechange", () => {
      if (pc.connectionState === "failed" || pc.connectionState === "closed") this.shutdown()
    })
  }

  get buffered() {
    return this.dc.bufferedAmount
  }
  localFingerprint() {
    return dtlsFingerprint(this.pc.currentLocalDescription?.sdp)
  }
  remoteFingerprint() {
    return dtlsFingerprint(this.pc.currentRemoteDescription?.sdp)
  }
  enableCrypto() {
    return Promise.resolve() // DTLS already encrypts the channel
  }
  send(frame: Wire) {
    if (this.closed || this.dc.readyState !== "open") return
    if (typeof frame === "string") this.dc.send(frame)
    else this.dc.send(frame as Uint8Array<ArrayBuffer>)
  }
  drain() {
    return new Promise<void>((resolve) => {
      if (this.closed || this.dc.bufferedAmount <= LOW_WATER) return resolve()
      const done = () => (this.dc.removeEventListener("bufferedamountlow", done), resolve())
      this.dc.addEventListener("bufferedamountlow", done)
      this.dc.addEventListener("close", done, { once: true })
    })
  }
  close() {
    this.shutdown()
  }
  private shutdown() {
    if (this.closed) return
    this.closed = true
    this.onclose?.() // local readers see the close immediately
    // Closing the data channel first lets queued data (a final cancel, say) reach the peer; only then drop the connection.
    const { pc, dc } = this
    const dropPc = () => {
      try {
        pc.close()
      } catch {
        /* already closed */
      }
    }
    try {
      dc.close()
    } catch {
      /* already closed */
    }
    if (dc.readyState === "closed") return dropPc()
    const timer = setTimeout(dropPc, 1000)
    dc.addEventListener(
      "close",
      () => {
        clearTimeout(timer)
        dropPc()
      },
      { once: true },
    )
  }
}

export interface RtcPeerOptions {
  iceServers: RTCIceServer[]
  /** The offerer creates the data channel; the answerer waits for it. */
  role: "offerer" | "answerer"
  sendSignal: (p: SignalPayload) => void
  /** Give up if the channel is not open by then. */
  timeoutMs?: number
}

/** One RTCPeerConnection attempt. Never reuse an instance for a second connection: create a fresh one. */
export class RtcPeer {
  /** Resolves with an open transport; rejects on failure or timeout. */
  readonly ready: Promise<RtcTransport>
  private pc: RTCPeerConnection
  private queue: RTCIceCandidateInit[] = [] // ICE that arrives before the remote description is set
  private remoteSet = false
  private o: RtcPeerOptions
  private done = false

  constructor(o: RtcPeerOptions) {
    this.o = o
    this.pc = new RTCPeerConnection({ iceServers: o.iceServers })
    this.ready = new Promise((resolve, reject) => {
      const t = setTimeout(() => this.fail(reject, "rtc timeout"), o.timeoutMs ?? 8000)
      const ok = (dc: RTCDataChannel) => {
        const open = () => (
          (this.done = true),
          clearTimeout(t),
          resolve(new RtcTransport(this.pc, dc))
        )
        if (dc.readyState === "open") open()
        else dc.onopen = open
      }
      this.pc.onicecandidate = (e) =>
        o.sendSignal({ kind: "candidate", candidate: e.candidate ? e.candidate.toJSON() : null })
      this.pc.onconnectionstatechange = () => {
        if (this.pc.connectionState === "failed") {
          clearTimeout(t)
          this.fail(reject, "rtc failed")
        }
      }
      if (o.role === "offerer") ok(this.pc.createDataChannel("warpdrop", { ordered: true }))
      else this.pc.ondatachannel = (e) => ok(e.channel)
    })
    this.ready.catch(() => {})
  }

  private fail(reject: (e: Error) => void, why: string) {
    if (this.done) return
    this.done = true
    this.pc.close()
    reject(new Error(why))
  }

  async start() {
    if (this.o.role !== "offerer") return
    await this.pc.setLocalDescription(await this.pc.createOffer())
    this.o.sendSignal({ kind: "offer", sdp: this.pc.localDescription!.sdp })
  }

  async onSignal(p: SignalPayload) {
    try {
      if (p.kind === "offer" && this.o.role === "answerer") {
        await this.pc.setRemoteDescription({ type: "offer", sdp: p.sdp })
        await this.flush()
        await this.pc.setLocalDescription(await this.pc.createAnswer())
        this.o.sendSignal({ kind: "answer", sdp: this.pc.localDescription!.sdp })
      } else if (p.kind === "answer" && this.o.role === "offerer") {
        await this.pc.setRemoteDescription({ type: "answer", sdp: p.sdp })
        await this.flush()
      } else if (p.kind === "candidate" && p.candidate) {
        if (this.remoteSet) await this.pc.addIceCandidate(p.candidate)
        else this.queue.push(p.candidate)
      }
    } catch {
      /* a stale or malformed signal must not take the page down; the timeout handles a dead negotiation */
    }
  }

  private async flush() {
    this.remoteSet = true
    for (const c of this.queue.splice(0)) await this.pc.addIceCandidate(c).catch(() => {})
  }

  close() {
    this.done = true
    this.pc.close()
  }
}
