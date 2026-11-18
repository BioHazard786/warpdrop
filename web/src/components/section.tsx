import { Marker, MarkerContent } from "@/components/ui/marker"
import type { ReactNode } from "react"

/** Pixel-font section label over a hairline, the page's main structural device. */
export function Section({
  title,
  aside,
  children,
}: {
  title: ReactNode
  aside?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3">
      <Marker variant="border" className="justify-between gap-3">
        <MarkerContent className="font-mono text-xs tracking-wider text-muted-foreground uppercase">
          {title}
        </MarkerContent>
        {aside && (
          <span className="flex items-center gap-2 font-mono text-xs text-muted-foreground tabular-nums">
            {aside}
          </span>
        )}
      </Marker>
      {children}
    </section>
  )
}
