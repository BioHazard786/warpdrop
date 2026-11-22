import { deviceName } from "./device"
import { Signaling, wsUrl } from "./signaling"

export interface NearbyDevice {
  id: string
  name: string
}

export interface Invite {
  from: NearbyDevice
  roomId: string
  ticket: string
}

/** The device name shown to others, trimmed the way the CLI trims it. */
const label = (n: string) => [...n.trim()].slice(0, 64).join("") || "Device"

/**
 * Membership in the same-network device list (the server groups clients by network). While connected this
 * client sees the others, can invite one of them, and, unless `hidden`, is listed to them too.
 */
export class NearbyLobby {
  private sig: Signaling
  private list = new Map<string, NearbyDevice>()
  private on: {
    devices: (d: NearbyDevice[]) => void
    invite: (i: Invite) => void
    open: (connected: boolean) => void
  }

  constructor(hidden: boolean, on: NearbyLobby["on"]) {
    this.on = on
    const name = deviceName()
    this.sig = new Signaling({
      url: wsUrl("/ws?lobby=1"), // its own connection pool on the server: an idle tab must not use up the transfer cap
      // every (re)connect joins afresh: the server dropped us from the list with the old socket
      onOpen: () =>
        this.sig.send({ t: "nearby_join", v: 2, device: name, hidden, client: { device: name } }),
      onStatus: (s) => on.open(s === "open"),
      onMessage: (m) => {
        switch (m.t) {
          case "nearby_joined":
            this.list = new Map(
              m.devices.map((d) => [d.peer_id, { id: d.peer_id, name: label(d.device) }]),
            )
            this.publish()
            break
          case "nearby_peer":
            this.list.set(m.peer.peer_id, { id: m.peer.peer_id, name: label(m.peer.device) })
            this.publish()
            break
          case "nearby_peer_left":
            if (this.list.delete(m.peer_id)) this.publish()
            break
          case "nearby_invite":
            on.invite({
              from: { id: m.peer.peer_id, name: label(m.peer.device) },
              roomId: m.room_id,
              ticket: m.ticket,
            })
            break
        }
      },
    })
    this.sig.connect()
  }

  private publish() {
    this.on.devices([...this.list.values()].sort((a, b) => a.name.localeCompare(b.name)))
  }

  /** Ask `deviceId` to join the room this client just created (`roomId` and `token` come from room_created). */
  invite(deviceId: string, roomId: string, token: string) {
    return this.sig.send({ t: "nearby_invite", to: deviceId, room_id: roomId, token })
  }

  /** Turn an invite down so the sender stops waiting. */
  decline(i: Invite, reason: "declined" | "busy") {
    this.sig.send({ t: "nearby_decline", room_id: i.roomId, ticket: i.ticket, reason })
  }

  close() {
    this.sig.close()
  }
}
