import { z } from "zod"

const peerId = z.string().max(64)
const serverMsg = z.discriminatedUnion("t", [
  z.object({ t: z.literal("room_created"), room_id: z.string(), token: z.string() }),
  z.object({ t: z.literal("room_resumed"), room_id: z.string(), peers: z.array(peerId) }),
  z.object({ t: z.literal("joined"), peer_id: peerId }),
  z.object({ t: z.literal("peer_joined"), peer_id: peerId, client: z.unknown() }),
  z.object({ t: z.literal("peer_left"), peer_id: peerId }),
  z.object({ t: z.literal("signal"), from: peerId, payload: z.unknown() }),
  z.object({ t: z.literal("relay_ticket"), peer_id: peerId, ticket: z.string() }),
  z.object({ t: z.literal("room_closed") }),
  z.object({ t: z.literal("error"), code: z.string(), message: z.string() }),
])
export type ServerMsg = z.infer<typeof serverMsg>

export type SignalingStatus = "connecting" | "open" | "reconnecting" | "closed"

export interface SignalingOptions {
  url: string
  onMessage: (m: ServerMsg) => void
  /** Fires on every (re)connect: the owner decides whether to create, resume or join. */
  onOpen: (reconnect: boolean) => void
  onStatus?: (s: SignalingStatus) => void
}

/** WebSocket to /ws with backoff reconnect. Messages are handled in the socket callback, never via React state, so none are dropped. */
export class Signaling {
  private ws: WebSocket | null = null
  private attempt = 0
  private stopped = false
  private everOpen = false
  private timer: ReturnType<typeof setTimeout> | null = null
  private o: SignalingOptions
  constructor(o: SignalingOptions) {
    this.o = o
  }

  connect() {
    this.o.onStatus?.(this.everOpen ? "reconnecting" : "connecting")
    const ws = new WebSocket(this.o.url)
    this.ws = ws
    ws.onopen = () => {
      this.attempt = 0
      this.o.onStatus?.("open")
      this.o.onOpen(this.everOpen)
      this.everOpen = true
    }
    ws.onmessage = (e) => {
      if (typeof e.data !== "string") return
      let raw: unknown
      try {
        raw = JSON.parse(e.data)
      } catch {
        return
      }
      const m = serverMsg.safeParse(raw)
      if (m.success) this.o.onMessage(m.data)
    }
    ws.onclose = () => {
      if (this.ws !== ws || this.stopped) return
      const delay = Math.min(8000, 500 * 2 ** this.attempt++) * (0.75 + Math.random() * 0.5)
      this.o.onStatus?.("reconnecting")
      this.timer = setTimeout(() => this.connect(), delay)
    }
  }

  get open() {
    return this.ws?.readyState === WebSocket.OPEN
  }

  send(msg: Record<string, unknown>): boolean {
    if (!this.open) return false
    this.ws!.send(JSON.stringify(msg))
    return true
  }

  close() {
    this.stopped = true
    if (this.timer) clearTimeout(this.timer)
    this.o.onStatus?.("closed")
    this.ws?.close()
  }
}

export const wsUrl = (path: string) => {
  const u = new URL(path, location.href)
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:"
  return u.toString()
}

const configSchema = z.object({
  v: z.literal(2),
  iceServers: z.array(
    z.object({
      urls: z.array(z.string()),
      username: z.string().optional(),
      credential: z.string().optional(),
    }),
  ),
  relay: z.boolean(),
  maxReceivers: z.number(),
})
export type AppConfig = z.infer<typeof configSchema>

export async function fetchConfig(): Promise<AppConfig> {
  const r = await fetch("/api/config", { cache: "no-store" })
  if (!r.ok) throw new Error(`config ${r.status}`)
  return configSchema.parse(await r.json())
}
