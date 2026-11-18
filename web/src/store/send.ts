import { create } from "zustand"
import { toast } from "@/components/ui/toast"
import { newFiles } from "@/lib/files"
import { SpeedMeter } from "@/lib/format"
import { type PeerView, SenderRoom, type SenderEvent } from "@/transfer/sender"

export interface PeerRow extends PeerView {
  speed: number
  eta: number
}

interface SendState {
  phase: "idle" | "starting" | "ready" | "reconnecting" | "closed" | "error"
  error?: string
  files: { name: string; size: number; type: string; preview?: string }[]
  roomId: string
  secret: string
  peers: PeerRow[]
  start(files: File[]): void
  stop(): void
  addFiles(files: File[]): void
  removeFile(file: { name: string; size: number }): void
  cancelPeer(id: string): void
  reset(): void
}

// Thumbnails are blob URLs (no copy of the bytes). shortcut: >20 MB images get an icon instead, since decoding them costs more than it's worth.
const PREVIEW_MAX = 20 * 1024 ** 2
const preview = (f: File) =>
  f.type.startsWith("image/") && f.size <= PREVIEW_MAX ? URL.createObjectURL(f) : undefined
const describe = (f: File) => ({ name: f.name, size: f.size, type: f.type, preview: preview(f) })
const revoke = (files: { preview?: string }[]) =>
  files.forEach((f) => f.preview && URL.revokeObjectURL(f.preview))

let room: SenderRoom | null = null
let list: File[] = [] // the File objects behind state.files, same order
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
          if (prev && e.peer.status === "done")
            toast.add({ type: "success", title: `Sent to ${e.peer.device}` })
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

  /** Apply a new file list. Previews are only made or revoked once the room accepts it. */
  const change = (
    next: File[],
    build: () => SendState["files"],
    dropped: { preview?: string }[] = [],
  ) => {
    if (next.length === 0 && get().peers.length === 0) return get().reset() // removed the last file: back to the start
    const err = room?.setFiles(next)
    if (err)
      return void toast.add({ type: "error", title: "Can't change the files", description: err })
    list = next
    revoke(dropped)
    set({ files: build() })
  }

  return {
    phase: "idle",
    files: [],
    roomId: "",
    secret: "",
    peers: [],
    start(files) {
      revoke(get().files)
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
        files: files.map((f) => ({
          name: f.name,
          size: f.size,
          type: f.type,
          preview: preview(f),
        })),
      })
      list = files
      room = new SenderRoom(files, onEvent)
      void room.start().catch((err) =>
        onEvent({
          t: "status",
          status: "error",
          error: err instanceof Error ? err.message : "Could not start",
        }),
      )
    },
    addFiles(added) {
      const fresh = newFiles(list, added)
      if (fresh.length < added.length)
        toast.add({
          type: "info",
          title: `${added.length - fresh.length === 1 ? "That file is" : "Those files are"} already added`,
        })
      if (fresh.length) change([...list, ...fresh], () => [...get().files, ...fresh.map(describe)])
    },
    removeFile(file) {
      const files = get().files
      const i = files.findIndex((f) => f === file) // by identity, so a stale click can't remove a different file
      if (i < 0) return
      change(
        list.filter((_, j) => j !== i),
        () => files.filter((_, j) => j !== i),
        [files[i]!],
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
      revoke(get().files)
      const old = room
      room = null
      old?.stop()
      set({ phase: "idle", files: [], peers: [], roomId: "", secret: "", error: undefined })
    },
  }
})

/** True while any receiver is mid-transfer: used to warn before the tab closes. */
export const useIsSending = () => useSend((s) => s.peers.some((p) => p.status === "sending"))
