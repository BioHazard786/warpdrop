import { FrameCipher } from "./handshake"
import { CHUNK_SIZE, LOW_WATER } from "./protocol"
import type { Transport, Wire } from "./transport"

const KEEPALIVE_MS = 20_000
const enc = new TextEncoder()
const dec = new TextDecoder()

/** WebSocket relay transport. After `enableCrypto` every frame is AES-256-GCM sealed; the server only sees ciphertext. */
export class RelayTransport implements Transport {
  readonly kind = "relay" as const
  readonly maxMessage = CHUNK_SIZE
  onframe: ((f: Wire) => void) | null = null
  onclose: (() => void) | null = null

  private ws: WebSocket
  private tx: FrameCipher | null = null
  private rx: FrameCipher | null = null
  private early: ArrayBuffer[] = [] // binary arriving before the cipher is ready is ciphertext: hold it
  private rxChain: Promise<void> = Promise.resolve()
  private txChain: Promise<void> = Promise.resolve()
  private pending = 0 // plaintext bytes sealed-but-not-yet-handed-to-the-socket
  private keepalive: ReturnType<typeof setInterval> | null = null
  private closed = false

  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.binaryType = "arraybuffer"
    ws.onmessage = (e) => this.onMessage(e.data as string | ArrayBuffer)
    ws.onclose = ws.onerror = () => this.shutdown()
  }

  static open(url: string, timeoutMs = 10_000): Promise<RelayTransport> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(url)
      const t = setTimeout(
        () => (ws.close(), reject(new Error("relay connect timeout"))),
        timeoutMs,
      )
      ws.onopen = () => (clearTimeout(t), resolve(new RelayTransport(ws)))
      ws.onerror = ws.onclose = () => (clearTimeout(t), reject(new Error("relay connect failed")))
    })
  }

  get buffered() {
    return this.ws.bufferedAmount + this.pending
  }
  localFingerprint() {
    return null // the relay is not DTLS: the handshake transcript omits fingerprints
  }
  remoteFingerprint() {
    return null
  }

  async enableCrypto(sessionKey: Uint8Array, role: "sender" | "receiver") {
    this.tx = await FrameCipher.create(sessionKey, role === "sender" ? 1 : 2)
    this.rx = await FrameCipher.create(sessionKey, role === "sender" ? 2 : 1)
    this.keepalive = setInterval(() => void this.seal(2, new Uint8Array()), KEEPALIVE_MS)
    for (const c of this.early.splice(0)) this.decrypt(c)
  }

  send(frame: Wire) {
    if (this.closed) return
    if (!this.tx) {
      if (typeof frame === "string") this.ws.send(frame) // handshake frames are plaintext text
      return
    }
    if (typeof frame === "string") void this.seal(0, enc.encode(frame))
    else void this.seal(1, frame)
  }

  private seal(kind: 0 | 1 | 2, payload: Uint8Array) {
    const tx = this.tx!
    this.pending += payload.length
    // sealing is async but the counter nonce requires strict order, so chain it
    this.txChain = this.txChain
      .then(async () => {
        const wire = await tx.seal(kind, payload)
        if (!this.closed) this.ws.send(wire as Uint8Array<ArrayBuffer>)
      })
      .catch(() => this.shutdown())
      .finally(() => (this.pending -= payload.length))
  }

  async drain() {
    while (this.buffered > LOW_WATER && !this.closed) await new Promise((r) => setTimeout(r, 15))
  }

  private onMessage(data: string | ArrayBuffer) {
    if (typeof data === "string") {
      if (!this.rx) this.onframe?.(data) // plaintext handshake frame
      else this.shutdown() // text after the cipher is on is a protocol violation
      return
    }
    if (!this.rx) this.early.push(data)
    else this.decrypt(data)
  }

  private decrypt(data: ArrayBuffer) {
    const rx = this.rx!
    this.rxChain = this.rxChain
      .then(async () => {
        const { kind, payload } = await rx.open(new Uint8Array(data))
        if (kind === 2) return
        this.onframe?.(kind === 0 ? dec.decode(payload) : payload)
      })
      .catch(() => this.shutdown()) // bad tag / replay / reorder
  }

  close() {
    this.shutdown()
  }

  private shutdown() {
    if (this.closed) return
    this.closed = true
    if (this.keepalive) clearInterval(this.keepalive)
    try {
      this.ws.close()
    } catch {
      /* already closed */
    }
    this.onclose?.()
  }
}
