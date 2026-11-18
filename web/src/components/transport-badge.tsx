import { Badge } from "@/components/ui/badge"

export function TransportBadge({ transport }: { transport: "rtc" | "relay" }) {
  return (
    <Badge
      variant="outline"
      className="font-mono tracking-wider"
      title={
        transport === "rtc"
          ? "Direct peer-to-peer connection"
          : "Through the server, still end-to-end encrypted"
      }
    >
      {transport === "rtc" ? "P2P" : "RELAY"}
    </Badge>
  )
}
