import QRCodeStyling from "qr-code-styling"
import { useEffect, useRef } from "react"

/** Always dark-on-white: scanners read it best, whatever the page theme is. */
export function QR({ value, size = 168 }: { value: string; size?: number }) {
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
      className="inline-block rounded-md bg-white p-2"
      role="img"
      aria-label="QR code for the share link"
    />
  )
}
