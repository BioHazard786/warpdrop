import { Badge } from "@/components/ui/badge"

const TEXT = {
  rtc: ["P2P", "Direct peer-to-peer connection"],
  relay: ["RELAY", "Through the server, still end-to-end encrypted"],
  lan: ["LAN", "Straight over your local network, no server involved"],
} as const

export function TransportBadge({ transport }: { transport: keyof typeof TEXT }) {
  const [label, title] = TEXT[transport]
  return (
    <Badge variant="outline" className="font-mono tracking-wider" title={title}>
      {label}
    </Badge>
  )
}
