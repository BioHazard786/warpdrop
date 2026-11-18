import { deviceName, registerTransport } from "./device"
import { MAX_FILES, MAX_MANIFEST_BYTES } from "./protocol"
import { RtcPeer, isSignalPayload } from "./rtc"
import { RelayTransport } from "./relay-transport"
import { type FileSource, SenderSession } from "./session"
import type { Transport } from "./transport"
import { type AppConfig, type ServerMsg, Signaling, fetchConfig, wsUrl } from "./signaling"
import { generateSecret } from "./words"

export interface PeerView {
  id: string
  device: string
  transport: "rtc" | "relay" | null
  status: "connecting" | "authenticating" | "sending" | "done" | "failed"
  /** Why it failed, human readable. */
  reason?: string
  /** Bytes delivered so far, across all files. */
  sent: number
  total: number
  filesDone: number
}

export type SenderEvent =
  | { t: "room"; roomId: string; secret: string }
  | {
      t: "status"
      status: "starting" | "ready" | "reconnecting" | "closed" | "error"
      error?: string
    }
  | { t: "peer"; peer: PeerView }
  | { t: "peer_removed"; id: string }

const MAX_AUTH_FAILURES = 3
const FALLBACK_CONFIG: AppConfig = {
  v: 2,
  iceServers: [{ urls: ["stun:stun.l.google.com:19302"] }],
  relay: true,
  maxReceivers: 10,
}

export function fileSource(file: File, id: number): FileSource {
  return {
    id,
    name: file.name || `file-${id}`,
    size: file.size,
    type: file.type,
    stream: (start, end) => file.slice(start, end).stream(),
  }
}

const REASONS: Record<string, string> = {
  declined: "Receiver declined",
  user: "Cancelled",
  auth_failed: "Wrong secret",
  hash_mismatch: "File arrived corrupted",
  protocol_error: "Protocol error",
}

/** One sender room: owns the signaling socket and one PeerLink per receiver. */
export class SenderRoom {
  private sources: FileSource[]
  private prefix: number[] // prefix[i] = bytes in files before i; files are served in order
  readonly total: number
  private emit: (e: SenderEvent) => void
  private sig: Signaling | null = null
  private links = new Map<string, PeerLink>()
  private roomId = ""
  private token = ""
  private secret = generateSecret()
  private authFailures = 0
  private stopped = false
  config: AppConfig = FALLBACK_CONFIG

  constructor(files: File[], emit: (e: SenderEvent) => void) {
    this.sources = files.map((f, i) => fileSource(f, i))
    this.prefix = []
    let acc = 0
    for (const f of this.sources) {
      this.prefix.push(acc)
      acc += f.size
    }
    this.total = acc
    this.emit = emit
  }

  async start() {
    const manifest = JSON.stringify(
      this.sources.map(({ id, name, size, type }) => ({ id, name, size, type })),
    )
    if (this.sources.length === 0) return this.fatal("Choose at least one file.")
    if (this.sources.length > MAX_FILES || manifest.length > MAX_MANIFEST_BYTES) {
      return this.fatal("Too many files for one transfer. Put them in a zip first.")
    }
    this.emit({ t: "status", status: "starting" })
    this.config = await fetchConfig().catch(() => FALLBACK_CONFIG)
    this.sig = new Signaling({
      url: wsUrl("/ws"),
      onOpen: (reconnect) => {
        if (reconnect && this.roomId)
          this.sig!.send({ t: "resume_room", room_id: this.roomId, token: this.token })
        else this.sig!.send({ t: "create_room", v: 2, client: { device: deviceName() } })
      },
      onStatus: (s) => {
        if (this.stopped) return
        if (s === "reconnecting") this.emit({ t: "status", status: "reconnecting" })
      },
      onMessage: (m) => this.onMessage(m),
    })
    this.sig.connect()
  }

  private fatal(error: string) {
    this.emit({ t: "status", status: "error", error })
    this.stop()
  }

  private onMessage(m: ServerMsg) {
    switch (m.t) {
      case "room_created":
        this.roomId = m.room_id
        this.token = m.token
        this.emit({ t: "room", roomId: m.room_id, secret: this.secret })
        this.emit({ t: "status", status: "ready" })
        break
      case "room_resumed":
        this.emit({ t: "status", status: "ready" })
        break
      case "peer_joined": {
        const device =
          typeof (m.client as { device?: unknown })?.device === "string"
            ? (m.client as { device: string }).device
            : "Receiver"
        this.links.get(m.peer_id)?.dispose()
        const link = new PeerLink(this, m.peer_id, device.slice(0, 64))
        this.links.set(m.peer_id, link)
        link.startRtc()
        break
      }
      case "peer_left":
        this.links.get(m.peer_id)?.onSignalingLeft()
        break
      case "signal":
        this.links.get(m.from)?.onSignal(m.payload)
        break
      case "relay_ticket":
        this.links.get(m.peer_id)?.openRelay(m.ticket)
        break
      case "room_closed":
        this.emit({ t: "status", status: "closed" })
        this.stop()
        break
      case "error":
        if (m.code === "not_found" && this.roomId)
          this.fatal("This share link expired. Start a new transfer.")
        else if (m.code === "rate_limited")
          this.fatal("Too many requests. Wait a minute and try again.")
        else this.fatal(m.message)
        break
    }
  }

  // --- called by PeerLink
  files() {
    return this.sources
  }
  offsetOf(idx: number) {
    return this.prefix[idx] ?? 0
  }
  get id() {
    return this.roomId
  }
  get sharedSecret() {
    return this.secret
  }
  signal(to: string, payload: unknown) {
    this.sig?.send({ t: "signal", to, payload })
  }
  emitPeer(peer: PeerView) {
    this.emit({ t: "peer", peer })
  }
  removePeer(id: string) {
    this.links.delete(id)
    this.emit({ t: "peer_removed", id })
  }
  authFailed() {
    if (++this.authFailures >= MAX_AUTH_FAILURES) {
      this.sig?.send({ t: "close_room" })
      this.fatal("Too many failed attempts: the room was closed.")
    }
  }

  cancelPeer(id: string) {
    this.links.get(id)?.cancel()
  }

  stop() {
    if (this.stopped) return
    this.stopped = true
    for (const l of this.links.values()) l.dispose()
    this.links.clear()
    this.sig?.send({ t: "close_room" })
    this.sig?.close()
  }
}

class PeerLink {
  private room: SenderRoom
  private id: string
  private rtc: RtcPeer | null = null
  private session: SenderSession | null = null
  private transport: Transport | null = null
  private unregister: (() => void) | null = null
  private signalingGone = false
  private disposed = false
  private view: PeerView

  constructor(room: SenderRoom, id: string, device: string) {
    this.room = room
    this.id = id
    this.view = {
      id,
      device,
      transport: null,
      status: "connecting",
      sent: 0,
      total: room.total,
      filesDone: 0,
    }
    room.emitPeer(this.view)
  }

  private update(patch: Partial<PeerView>) {
    this.view = { ...this.view, ...patch }
    if (!this.disposed) this.room.emitPeer(this.view)
  }

  startRtc() {
    this.rtc?.close()
    const rtc = new RtcPeer({
      role: "offerer",
      iceServers: this.room.config.iceServers,
      sendSignal: (p) => this.room.signal(this.id, p),
    })
    this.rtc = rtc
    rtc.ready.then((t) => this.rtc === rtc && this.attach(t)).catch(() => {}) // the receiver asks for the relay if RTC fails
    void rtc.start()
  }

  onSignal(payload: unknown) {
    if (!isSignalPayload(payload)) return
    if (payload.kind === "restart") {
      this.session = null
      this.startRtc()
    } else void this.rtc?.onSignal(payload)
  }

  openRelay(ticket: string) {
    RelayTransport.open(wsUrl(`/relay/${ticket}`))
      .then((t) => this.attach(t))
      .catch(() => this.update({ status: "failed", reason: "Relay connection failed" }))
  }

  private attach(t: Transport) {
    if (this.disposed) return t.close()
    this.unregister?.()
    this.transport?.close() // a newer transport replaces the old one; the old session's result is ignored below
    this.transport = t
    if (this.rtc && t.kind === "relay") {
      this.rtc.close()
      this.rtc = null
    }
    const session = new SenderSession({
      transport: t,
      roomId: this.room.id,
      secret: this.room.sharedSecret,
      device: deviceName(),
      files: this.room.files(),
      onProgress: (idx, bytes) =>
        this.update({ sent: this.room.offsetOf(idx) + bytes, status: "sending" }),
      onFileDone: () => this.update({ filesDone: this.view.filesDone + 1 }),
    })
    this.session = session
    this.unregister = registerTransport(() => t.close())
    this.update({ transport: t.kind === "relay" ? "relay" : "rtc", status: "authenticating" })
    void session.run().then((r) => {
      if (this.session !== session || this.disposed) return // superseded by a newer transport
      this.unregister?.()
      if (r.status === "done") this.update({ status: "done" })
      else if (r.status === "closed") {
        if (this.signalingGone) this.room.removePeer(this.id)
        else this.update({ status: "connecting", transport: null }) // waiting for the receiver to reconnect and resume
      } else {
        if (r.reason === "auth_failed") this.room.authFailed()
        this.update({ status: "failed", reason: REASONS[r.reason] ?? r.reason })
      }
    })
  }

  /** Server says the receiver's signaling socket is gone. A live P2P/relay session must outlive that. */
  onSignalingLeft() {
    this.signalingGone = true
    if (!this.session || this.view.status === "connecting") this.dispose(true)
  }

  cancel() {
    this.session?.cancel()
  }

  dispose(remove = false) {
    this.disposed = true
    this.unregister?.()
    this.rtc?.close()
    this.rtc = null
    this.session = null
    this.transport?.close()
    this.transport = null
    if (remove) this.room.removePeer(this.id)
  }
}
