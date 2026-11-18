import { IconX } from "@tabler/icons-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { formatBytes, formatETA, formatSpeed } from "@/lib/format"
import type { PeerRow as Peer } from "@/store/send"

const LABEL: Record<Peer["status"], string> = {
  connecting: "Connecting",
  authenticating: "Verifying",
  sending: "Sending",
  done: "Delivered",
  failed: "Failed",
}

export function PeerRow({ peer, onCancel }: { peer: Peer; onCancel: () => void }) {
  const pct = peer.total
    ? Math.min(100, (peer.sent / peer.total) * 100)
    : peer.status === "done"
      ? 100
      : 0
  const live =
    peer.status === "sending" || peer.status === "authenticating" || peer.status === "connecting"
  return (
    <li className="space-y-2 rounded-lg border border-border p-3">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{peer.device}</span>
        {peer.transport && (
          <Badge
            variant="outline"
            title={
              peer.transport === "rtc"
                ? "Direct peer-to-peer connection"
                : "Through the server, still end-to-end encrypted"
            }
          >
            {peer.transport === "rtc" ? "P2P" : "RELAY"}
          </Badge>
        )}
        <Badge
          variant={
            peer.status === "failed"
              ? "destructive"
              : peer.status === "done"
                ? "default"
                : "secondary"
          }
        >
          {peer.status === "connecting" || peer.status === "authenticating" ? <Spinner /> : null}
          {LABEL[peer.status]}
        </Badge>
        {live && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onCancel}
            aria-label={`Cancel transfer to ${peer.device}`}
          >
            <IconX />
          </Button>
        )}
      </div>
      {(peer.status === "sending" || peer.status === "done") && (
        <>
          <Progress value={pct} aria-label={`Progress for ${peer.device}`} />
          <div className="flex justify-between font-mono text-xs text-muted-foreground tabular-nums">
            <span>
              {formatBytes(peer.sent)} / {formatBytes(peer.total)}
              {peer.total > 0 && ` · ${peer.filesDone} files done`}
            </span>
            {peer.status === "sending" && (
              <span>
                {formatSpeed(peer.speed)} · {formatETA(peer.eta)}
              </span>
            )}
          </div>
        </>
      )}
      {peer.status === "failed" && peer.reason && (
        <p className="text-xs text-destructive">{peer.reason}</p>
      )}
    </li>
  )
}
