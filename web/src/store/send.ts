import { create } from "zustand"
import { SpeedMeter } from "@/lib/format"
import { type PeerView, SenderRoom, type SenderEvent } from "@/transfer/sender"

export interface PeerRow extends PeerView {
  speed: number
  eta: number
}

interface SendState {
  phase: "idle" | "starting" | "ready" | "reconnecting" | "closed" | "error"
  error?: string
  files: { name: string; size: number }[]
  roomId: string
  secret: string
  peers: PeerRow[]
  start(files: File[]): void
  stop(): void
  cancelPeer(id: string): void
  reset(): void
}

let room: SenderRoom | null = null
const meters = new Map<string, SpeedMeter>()
let pending = new Map<string, PeerView>()
let timer: ReturnType<typeof setTimeout> | null = null

export const useSend = create<SendState>((set, get) => {
  const flush = () => {
    timer = null
    if (pending.size === 0) return
    const batch = pending
    pending = new Map()
    set((s) => {
      const rows = new Map(s.peers.map((p) => [p.id, p]))
      for (const [id, v] of batch) {
        const m = meters.get(id) ?? meters.set(id, new SpeedMeter()).get(id)!
        const speed = v.status === "sending" ? m.sample(v.sent) : 0
        rows.set(id, { ...v, speed, eta: v.status === "sending" ? m.eta(v.total - v.sent) : 0 })
      }
      return { peers: [...rows.values()] }
    })
  }
  const schedule = () => {
    timer ??= setTimeout(flush, 250) // ~4 Hz: a 1 GB/s link emits thousands of chunk events per second
  }

  const onEvent = (e: SenderEvent) => {
    switch (e.t) {
      case "room":
        set({ roomId: e.roomId, secret: e.secret })
        break
      case "status":
        set({ phase: e.status, error: e.error })
        break
      case "peer": {
        pending.set(e.peer.id, e.peer)
        // state changes (not progress ticks) should show immediately
        const prev = get().peers.find((p) => p.id === e.peer.id)
        if (!prev || prev.status !== e.peer.status || prev.transport !== e.peer.transport) {
          if (timer) clearTimeout(timer)
          flush()
        } else schedule()
        break
      }
      case "peer_removed":
        pending.delete(e.id)
        meters.delete(e.id)
        set((s) => ({ peers: s.peers.filter((p) => p.id !== e.id) }))
        break
    }
  }

  return {
    phase: "idle",
    files: [],
    roomId: "",
    secret: "",
    peers: [],
    start(files) {
      const old = room
      room = null
      old?.stop()
      pending = new Map()
      meters.clear()
      set({
        phase: "starting",
        error: undefined,
        peers: [],
        roomId: "",
        secret: "",
        files: files.map((f) => ({ name: f.name, size: f.size })),
      })
      room = new SenderRoom(files, onEvent)
      void room.start().catch((err) =>
        onEvent({
          t: "status",
          status: "error",
          error: err instanceof Error ? err.message : "Could not start",
        }),
      )
    },
    cancelPeer: (id) => room?.cancelPeer(id),
    stop() {
      const old = room
      room = null
      old?.stop()
      set({ phase: "closed" })
    },
    reset() {
      const old = room
      room = null
      old?.stop()
      set({ phase: "idle", files: [], peers: [], roomId: "", secret: "", error: undefined })
    },
  }
})

/** True while any receiver is mid-transfer: used to warn before the tab closes. */
export const useIsSending = () => useSend((s) => s.peers.some((p) => p.status === "sending"))
