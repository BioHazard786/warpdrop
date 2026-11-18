import { IconDeviceDesktop, IconDeviceMobile, IconX } from "@tabler/icons-react"
import { TransportBadge } from "@/components/transport-badge"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { formatBytes, formatETA, formatSpeed } from "@/lib/format"
import { cn } from "@/lib/utils"
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
  const busy = peer.status === "connecting" || peer.status === "authenticating"
  const live = busy || peer.status === "sending"
  const DeviceIcon = /android|iphone|ipad|mobile/i.test(peer.device)
    ? IconDeviceMobile
    : IconDeviceDesktop
  return (
    <Item variant="outline" render={<li />} className="rounded-xl">
      <ItemMedia variant="icon" className="size-9 rounded-lg bg-muted">
        <DeviceIcon />
      </ItemMedia>
      <ItemContent className="min-w-0">
        <ItemTitle className="w-full truncate">{peer.device}</ItemTitle>
        <ItemDescription className="font-mono text-xs tabular-nums">
          {peer.status === "failed" ? (
            <span className="text-destructive">{peer.reason ?? "Transfer stopped"}</span>
          ) : peer.status === "sending" ? (
            <>
              {formatBytes(peer.sent)} / {formatBytes(peer.total)} · {formatSpeed(peer.speed)} ·{" "}
              {formatETA(peer.eta)}
            </>
          ) : peer.status === "done" ? (
            <>
              {formatBytes(peer.total)} · {peer.filesDone} {peer.filesDone === 1 ? "file" : "files"}
            </>
          ) : (
            <span className="shimmer">Setting up an encrypted channel</span>
          )}
        </ItemDescription>
      </ItemContent>
      <ItemActions>
        {peer.transport && <TransportBadge transport={peer.transport} />}
        <Badge
          variant={
            peer.status === "failed"
              ? "destructive"
              : peer.status === "done"
                ? "default"
                : "secondary"
          }
        >
          {busy && <Spinner data-icon="inline-start" />}
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
      </ItemActions>
      {(peer.status === "sending" || peer.status === "done") && (
        <Progress
          value={pct}
          aria-label={`Progress for ${peer.device}`}
          className={cn("basis-full", peer.status === "done" && "opacity-40")}
        />
      )}
    </Item>
  )
}
