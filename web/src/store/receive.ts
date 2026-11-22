import { create } from "zustand"
import { SpeedMeter } from "@/lib/format"
import type { FileEntry } from "@/transfer/protocol"
import { ReceiverClient, type ReceiverEvent, type ReceiverStatus } from "@/transfer/receiver"

export type OfferedFile = FileEntry & { safeName: string }

interface ReceiveState {
  status: ReceiverStatus | "idle"
  transport?: "rtc" | "relay"
  reason?: string
  code?: string
  files: OfferedFile[]
  /** The sender's device name, known once the file list arrives. */
  from: string
  /** Nearby transfers only: the 6-digit code that must match the sender's screen. */
  sas: string
  total: number
  received: number
  filesDone: number
  speed: number
  eta: number
  start(roomId: string, secret: string, ticket?: string): void
  respond(accept: boolean): void
  cancel(): void
  reset(): void
}

let client: ReceiverClient | null = null
let prefix: number[] = []
let meter = new SpeedMeter()
let latest = 0 // latest received byte count, flushed to the store at ~4 Hz
let timer: ReturnType<typeof setInterval> | null = null

export const useReceive = create<ReceiveState>((set, get) => {
  const stopTimer = () => {
    if (timer) clearInterval(timer)
    timer = null
  }
  const startTimer = () => {
    timer ??= setInterval(() => {
      const speed = meter.sample(latest)
      set({ received: latest, speed, eta: meter.eta(get().total - latest) })
    }, 250)
  }

  const onEvent = (e: ReceiverEvent) => {
    switch (e.t) {
      case "status":
        if (e.status === "done" || e.status === "failed") {
          stopTimer()
          set({ received: e.status === "done" ? get().total : latest, speed: 0, eta: 0 })
        }
        if (e.status === "receiving") startTimer()
        set({
          status: e.status,
          transport: e.transport ?? get().transport,
          reason: e.reason,
          code: e.code,
        })
        break
      case "offer": {
        prefix = []
        let acc = 0
        for (const f of e.files) {
          prefix.push(acc)
          acc += f.size
        }
        set({ files: e.files, from: e.from, sas: e.sas, total: acc })
        break
      }
      case "progress":
        latest = (prefix[e.idx] ?? 0) + e.bytes
        break
      case "file_done":
        set({ filesDone: get().filesDone + 1 })
        break
    }
  }

  const blank = {
    status: "idle" as const,
    files: [],
    from: "",
    sas: "",
    total: 0,
    received: 0,
    filesDone: 0,
    speed: 0,
    eta: 0,
    reason: undefined,
    code: undefined,
    transport: undefined,
  }
  return {
    ...blank,
    start(roomId, secret, ticket) {
      const old = client
      client = null // detach first so the old client's farewell events are dropped
      old?.cancel()
      stopTimer()
      meter = new SpeedMeter()
      latest = 0
      set({ ...blank, status: "connecting" })
      const c = new ReceiverClient(
        roomId,
        secret,
        (e) => {
          if (client === c) onEvent(e) // ignore events from a client that was replaced or reset
        },
        ticket,
      )
      client = c
      void c.start()
    },
    respond: (accept) => client?.respond(accept),
    cancel: () => client?.cancel(),
    reset() {
      const old = client
      client = null
      old?.cancel()
      stopTimer()
      set(blank)
    },
  }
})
