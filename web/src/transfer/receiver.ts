import { deviceName, forceRelay, registerTransport } from "./device"
import { DownloadSink } from "./download"
import type { FileEntry } from "./protocol"
import { RtcPeer, isSignalPayload } from "./rtc"
import { RelayTransport } from "./relay-transport"
import { ReceiveState, ReceiverSession } from "./session"
import { type AppConfig, type ServerMsg, Signaling, fetchConfig, wsUrl } from "./signaling"
import type { Transport } from "./transport"

export type ReceiverStatus =
  | "connecting" // signaling
  | "negotiating" // WebRTC / relay
  | "authenticating"
  | "offered" // waiting for the user to accept
  | "receiving"
  | "reconnecting" // lost transport mid-transfer, resuming
  | "done"
  | "failed"

export type ReceiverEvent =
  | {
      t: "status"
      status: ReceiverStatus
      transport?: "rtc" | "relay"
      reason?: string
      code?: string
    }
  | { t: "offer"; files: (FileEntry & { safeName: string })[] }
  | { t: "progress"; idx: number; bytes: number }
  | { t: "file_done"; idx: number }

const FALLBACK_CONFIG: AppConfig = {
  v: 2,
  iceServers: [{ urls: ["stun:stun.l.google.com:19302"] }],
  relay: true,
  maxReceivers: 10,
}
const MAX_RECONNECTS = 5
const REASONS: Record<string, string> = {
  declined: "Declined",
  user: "The sender cancelled",
  auth_failed: "Wrong words: the secret did not match",
  hash_mismatch: "The file arrived corrupted and was discarded",
  protocol_error: "Protocol error",
}

const SERVER_ERRORS: Record<string, string> = {
  not_found: "No transfer with that code. It may have finished or expired.",
  room_full: "This transfer already has the maximum number of receivers.",
  rate_limited: "Too many attempts. Wait a minute and try again.",
  unsupported_version: "This link was made by an incompatible WarpDrop version.",
}

export class ReceiverClient {
  private roomId: string
  private secret: string
  private emit: (e: ReceiverEvent) => void
  private config: AppConfig = FALLBACK_CONFIG
  private sig: Signaling | null = null
  private state = new ReceiveState()
  private sink = new DownloadSink(() => this.state.files)
  private decide: ((accept: boolean) => void) | null = null
  private rtc: RtcPeer | null = null
  private session: ReceiverSession | null = null
  private transport: Transport | null = null
  private joined: ((ok: boolean) => void) | null = null
  private ticket: ((t: string | null) => void) | null = null
  private attempt = 0
  private ended = false

  constructor(roomId: string, secret: string, emit: (e: ReceiverEvent) => void) {
    this.roomId = roomId
    this.secret = secret
    this.emit = emit
  }

  /** The user's answer to the file list. */
  respond(accept: boolean) {
    this.decide?.(accept)
    this.decide = null
  }

  cancel() {
    if (this.session) this.session.cancel()
    else this.end("failed", "Cancelled")
  }

  async start() {
    this.status("connecting")
    this.config = await fetchConfig().catch(() => FALLBACK_CONFIG)
    this.sig = new Signaling({
      url: wsUrl("/ws"),
      onOpen: () => {}, // joining is driven by connect(), not by the socket
      onMessage: (m) => this.onMessage(m),
    })
    this.sig.connect()
    try {
      await this.run()
    } catch (e) {
      this.end("failed", e instanceof Error ? e.message : "Connection failed")
    }
  }

  private status(
    status: ReceiverStatus,
    extra: { transport?: "rtc" | "relay"; reason?: string; code?: string } = {},
  ) {
    if (!this.ended) this.emit({ t: "status", status, ...extra })
  }

  private end(status: "done" | "failed", reason?: string, code?: string) {
    if (this.ended) return
    this.status(status, { reason, code })
    this.ended = true
    this.rtc?.close()
    this.transport?.close()
    this.sig?.close()
  }

  private onMessage(m: ServerMsg) {
    switch (m.t) {
      case "joined":
        this.joined?.(true)
        break
      case "signal":
        if (isSignalPayload(m.payload)) void this.rtc?.onSignal(m.payload)
        break
      case "relay_ticket":
        this.ticket?.(m.ticket)
        break
      case "room_closed":
        // an established session finishes or fails on its own transport; only a not-yet-connected receiver gives up here
        if (!this.transport) this.end("failed", "The sender closed this transfer.", "room_closed")
        break
      case "error":
        this.lastError = SERVER_ERRORS[m.code] ?? m.message
        this.joined?.(false) // wake whoever is waiting; they throw lastError
        this.ticket?.(null)
        break
    }
  }

  private lastError = ""

  private async waitOpen(ms = 10_000) {
    const t0 = Date.now()
    while (!this.sig?.open) {
      if (Date.now() - t0 > ms) throw new Error("Cannot reach the server.")
      await new Promise((r) => setTimeout(r, 100))
    }
  }

  private async join() {
    await this.waitOpen()
    const ok = await new Promise<boolean>((resolve) => {
      const t = setTimeout(() => resolve(false), 10_000)
      this.joined = (v) => (clearTimeout(t), resolve(v))
      this.sig!.send({
        t: "join_room",
        v: 2,
        room_id: this.roomId,
        client: { device: deviceName() },
      })
    })
    this.joined = null
    if (!ok) throw new Error(this.lastError || "Could not join the transfer.")
  }

  /** Get a live transport: direct WebRTC first, relay if that does not come up in time. */
  private async establish(resuming: boolean): Promise<Transport> {
    this.status(resuming ? "reconnecting" : "negotiating")
    if (!forceRelay()) {
      const rtc = new RtcPeer({
        role: "answerer",
        iceServers: this.config.iceServers,
        sendSignal: (p) => this.sig?.send({ t: "signal", to: "sender", payload: p }),
      })
      this.rtc = rtc
      if (resuming) this.sig?.send({ t: "signal", to: "sender", payload: { kind: "restart" } }) // sender builds a fresh offer
      try {
        return await rtc.ready
      } catch {
        rtc.close()
      }
    }
    if (!this.config.relay)
      throw new Error(
        "A direct connection could not be made and the relay is disabled on this server.",
      )
    const ticket = await new Promise<string | null>((resolve) => {
      const t = setTimeout(() => resolve(null), 10_000)
      this.ticket = (v) => (clearTimeout(t), resolve(v))
      this.sig!.send({ t: "relay_request", to: "sender" })
    })
    this.ticket = null
    if (!ticket) throw new Error(this.lastError || "The relay did not respond.")
    return RelayTransport.open(wsUrl(`/relay/${ticket}`))
  }

  private async run() {
    await this.join()
    for (;;) {
      const resuming = this.attempt++ > 0
      if (resuming && !this.sig?.open) await this.join() // the signaling socket dropped meanwhile: rejoin (new peer id)
      const t = await this.establish(resuming)
      this.transport = t
      const unregister = registerTransport(() => t.close())
      this.status("authenticating", { transport: t.kind === "relay" ? "relay" : "rtc" })
      const session = new ReceiverSession({
        transport: t,
        roomId: this.roomId,
        secret: this.secret,
        device: deviceName(),
        sink: this.sink,
        state: this.state,
        onOffer: (files) => {
          this.status("offered")
          this.emit({ t: "offer", files })
          return new Promise<boolean>((resolve) => {
            this.decide = (accept) => {
              if (accept)
                this.status("receiving", { transport: t.kind === "relay" ? "relay" : "rtc" })
              resolve(accept)
            }
          })
        },
        onProgress: (idx, bytes) => {
          this.emit({ t: "progress", idx, bytes })
        },
        onFileDone: (idx) => this.emit({ t: "file_done", idx }),
      })
      this.session = session
      const progressOnce = () =>
        this.status("receiving", { transport: t.kind === "relay" ? "relay" : "rtc" })
      if (this.state.accepted) progressOnce()
      const r = await session.run()
      unregister()
      this.session = null
      this.transport = null
      if (r.status === "done") return this.end("done")
      if (r.status === "failed") {
        const reason = REASONS[r.reason] ?? r.reason
        return this.end("failed", reason, r.reason)
      }
      if (this.attempt > MAX_RECONNECTS)
        return this.end("failed", "The connection kept dropping.", "connection_lost")
    }
  }
}
