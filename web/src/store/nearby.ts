import { create } from "zustand"
import { useReceive } from "@/store/receive"
import { useSend } from "@/store/send"
import { type Invite, NearbyLobby, type NearbyDevice } from "@/transfer/nearby"

const KEY = "warpdrop.nearby.visible"
// Visible by default, like PairDrop: a device nobody can see can't be sent to. The choice is remembered per browser.
const wasVisible = () => {
  try {
    return localStorage.getItem(KEY) !== "0"
  } catch {
    return true
  }
}

interface NearbyState {
  /** Listed to other devices on this network. Off still lets this device see them and send. */
  visible: boolean
  connected: boolean
  devices: NearbyDevice[]
  /** Someone picked this device and wants to send; the user has not answered yet. */
  incoming: Invite | null
  setVisible(v: boolean): void
  connect(): void
  disconnect(): void
  /** Close the prompt without answering (the sender gives up after a minute). */
  dismiss(): void
  /** Turn the prompt's invite down; the sender is told at once. */
  decline(): void
  /** Invite `device` into the nearby share, starting one with `files` if none is running. */
  sendTo(device: NearbyDevice, files?: File[]): void
}

let lobby: NearbyLobby | null = null

export const useNearby = create<NearbyState>((set, get) => {
  const join = () => {
    let live = false // this lobby's socket has been open; only then does a drop clear the list
    // assigned after construction: Signaling reports "connecting" synchronously from inside the constructor
    let me: NearbyLobby | null = null
    me = new NearbyLobby(!get().visible, {
      // a replaced lobby (visibility toggle) may still report while closing: ignore it
      devices: (devices) => lobby === me && set({ devices }),
      open: (connected) => {
        if (lobby !== me || connected === live) return
        live = connected
        set(connected ? { connected } : { connected, devices: [] })
      },
      invite: (i) => {
        if (lobby !== me) return
        // one at a time, and never over a transfer that is already running: say so, so the sender stops waiting
        if (get().incoming || useReceive.getState().status !== "idle") me?.decline(i, "busy")
        else set({ incoming: i })
      },
    })
    lobby = me
  }
  const leave = () => {
    lobby?.close()
    lobby = null
    set({ connected: false, devices: [] })
  }
  return {
    visible: wasVisible(),
    connected: false,
    devices: [],
    incoming: null,
    setVisible(visible) {
      try {
        localStorage.setItem(KEY, visible ? "1" : "0")
      } catch {
        // private mode: the choice just won't be remembered
      }
      set({ visible })
      if (lobby) {
        // the server fixes visibility at join time, so rejoin; the current list stays on screen until the new one arrives
        const old = lobby
        join()
        old.close()
      }
    },
    connect() {
      if (!lobby) join()
    },
    disconnect: leave,
    dismiss: () => set({ incoming: null }),
    decline() {
      const i = get().incoming
      if (i) lobby?.decline(i, "declined")
      set({ incoming: null })
    },
    sendTo(device, files) {
      const send = useSend.getState()
      if (files?.length) send.start(files, true)
      else if (send.mode !== "nearby" || send.phase === "idle") return
      useSend
        .getState()
        .invite(device, (roomId, token) => lobby?.invite(device.id, roomId, token) ?? false)
    },
  }
})
