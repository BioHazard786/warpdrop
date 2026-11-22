import { create } from "zustand"
import { toast } from "@/components/ui/toast"
import { newFiles } from "@/lib/files"
import { peerFor } from "@/lib/nearby-status"
import { SpeedMeter } from "@/lib/format"
import { type PeerView, SenderRoom, type SenderEvent } from "@/transfer/sender"

export interface PeerRow extends PeerView {
  speed: number
  eta: number
}

/** A nearby device this room invited, and what became of the invite before (or instead of) it joining. */
export interface SentInvite {
  device: { id: string; name: string }
  /** Peer ids that came through earlier invites to this device: its tile follows only a newer one. */
  stale: string[]
  state: "pending" | "declined" | "busy" | "timeout" | "failed" | "cancelled"
}

/** How long an invite may go unanswered before its tile says "No response". */
export const INVITE_TIMEOUT_MS = 60_000

interface SendState {
  phase: "idle" | "starting" | "ready" | "reconnecting" | "closed" | "error"
  error?: string
  files: { name: string; size: number; type: string; preview?: string }[]
  roomId: string
  secret: string
  /** "nearby": sent to devices picked from the same-network list, no code. */
  mode: "link" | "nearby"
  /** Nearby only, by device id. */
  invites: Record<string, SentInvite>
  peers: PeerRow[]
  start(files: File[], nearby?: boolean): void
  /** Nearby: invite one more device into the room; `send` delivers it through the lobby. */
  invite(
    device: { id: string; name: string },
    send: (roomId: string, token: string) => boolean,
  ): void
  /** Nearby: stop waiting on an invite nobody has answered yet. */
  cancelInvite(deviceId: string): void
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
const inviteTimers = new Map<string, ReturnType<typeof setTimeout>>()
const clearInviteTimers = () => {
  for (const t of inviteTimers.values()) clearTimeout(t)
  inviteTimers.clear()
}

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
      case "declined":
        setInvite(e.deviceId, e.reason)
        break
    }
  }

  const setInvite = (id: string, state: SentInvite["state"]) => {
    const inv = get().invites[id]
    if (!inv || inv.state !== "pending") return
    clearTimeout(inviteTimers.get(id))
    inviteTimers.delete(id)
    if (state === "declined" || state === "busy")
      toast.add({
        type: "info",
        title: state === "busy" ? `${inv.device.name} is busy` : `${inv.device.name} declined`,
        description:
          state === "busy" ? "They're in another transfer. Try again in a moment." : undefined,
      })
    set((s) => ({ invites: { ...s.invites, [id]: { ...inv, state } } }))
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
    mode: "link",
    invites: {},
    peers: [],
    start(files, nearby) {
      revoke(get().files)
      const old = room
      room = null
      old?.stop()
      pending = new Map()
      meters.clear()
      clearInviteTimers()
      set({
        phase: "starting",
        error: undefined,
        peers: [],
        roomId: "",
        secret: "",
        mode: nearby ? "nearby" : "link",
        invites: {},
        files: files.map((f) => ({
          name: f.name,
          size: f.size,
          type: f.type,
          preview: preview(f),
        })),
      })
      list = files
      room = new SenderRoom(files, onEvent, nearby)
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
    invite(device, send) {
      const r = room
      if (!r || get().mode !== "nearby") return
      const stale = get()
        .peers.filter((p) => p.invited === device.id)
        .map((p) => p.id)
      set((s) => ({ invites: { ...s.invites, [device.id]: { device, stale, state: "pending" } } }))
      clearTimeout(inviteTimers.get(device.id))
      inviteTimers.set(
        device.id,
        setTimeout(() => {
          const inv = get().invites[device.id]
          if (inv && !peerFor(inv, get().peers)) setInvite(device.id, "timeout")
        }, INVITE_TIMEOUT_MS),
      )
      r.invite((roomId, token) => {
        const ok = room === r && send(roomId, token)
        if (!ok) setInvite(device.id, "failed")
        return ok
      })
    },
    cancelInvite: (id) => setInvite(id, "cancelled"),
    cancelPeer: (id) => room?.cancelPeer(id),
    stop() {
      const old = room
      room = null
      old?.stop()
      clearInviteTimers()
      set({ phase: "closed" })
    },
    reset() {
      revoke(get().files)
      const old = room
      room = null
      old?.stop()
      clearInviteTimers()
      set({
        phase: "idle",
        files: [],
        peers: [],
        roomId: "",
        secret: "",
        mode: "link",
        invites: {},
        error: undefined,
      })
    },
  }
})

/** True while any receiver is mid-transfer: used to warn before the tab closes. */
export const useIsSending = () => useSend((s) => s.peers.some((p) => p.status === "sending"))
