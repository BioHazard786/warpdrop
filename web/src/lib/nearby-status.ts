import type { TileStatus } from "@/components/nearby-devices"
import { formatCode } from "@/lib/format"
import type { PeerRow, SentInvite } from "@/store/send"

/** The newest peer that joined for this invite, if any. */
export const peerFor = (inv: SentInvite, peers: PeerRow[]) =>
  peers.findLast((p) => p.invited === inv.device.id && !inv.stale.includes(p.id))

const INVITE: Record<Exclude<SentInvite["state"], "pending">, string> = {
  declined: "Declined",
  busy: "Busy",
  timeout: "No response",
  failed: "Couldn't reach",
  cancelled: "Cancelled",
}

export function tileStatus(inv: SentInvite | undefined, peers: PeerRow[]): TileStatus | undefined {
  if (!inv) return undefined
  const p = peerFor(inv, peers)
  if (p) {
    switch (p.status) {
      case "connecting":
      case "authenticating":
        return { tone: "live", label: "Connecting…" }
      case "waiting":
        return { tone: "live", label: p.sas ? formatCode(p.sas) : "Waiting…" }
      case "sending":
        return {
          tone: "live",
          label: `${Math.floor((p.sent / (p.total || 1)) * 100)}%`,
          progress: p.total ? p.sent / p.total : 0,
        }
      case "done":
        return { tone: "done", label: "Sent" }
      case "failed":
        return { tone: "error", label: p.reason ?? "Failed" }
    }
  }
  if (inv.state === "pending") return { tone: "live", label: "Waiting…" }
  return { tone: "error", label: INVITE[inv.state] }
}
