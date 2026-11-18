import QRCodeStyling from "qr-code-styling"
import { useEffect, useRef } from "react"
import { cn } from "@/lib/utils"

/** Always dark-on-white: scanners read it best, whatever the page theme is. */
export function QR({
  value,
  size = 160,
  className,
}: {
  value: string
  size?: number
  className?: string
}) {
  const host = useRef<HTMLDivElement>(null)
  const qr = useRef<QRCodeStyling | null>(null)
  useEffect(() => {
    const opts = {
      width: size,
      height: size,
      data: value,
      margin: 0,
      qrOptions: { errorCorrectionLevel: "M" as const },
      dotsOptions: { type: "square" as const, color: "#0a0a0a" },
      backgroundOptions: { color: "#ffffff" },
    }
    if (!qr.current) {
      qr.current = new QRCodeStyling(opts)
      if (host.current) qr.current.append(host.current)
    } else qr.current.update(opts)
  }, [value, size])
  return (
    <div
      ref={host}
      className={cn(
        "inline-block shrink-0 rounded-xl bg-white p-3 ring-1 ring-foreground/10",
        className,
      )}
      role="img"
      aria-label="QR code for the share link"
    />
  )
}
