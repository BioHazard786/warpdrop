import type { ReactNode } from "react"

/** Mono uppercase section label, the page's main structural device. */
export function Section({
  title,
  aside,
  children,
}: {
  title: string
  aside?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-baseline justify-between gap-3 border-b border-border pb-2">
        <h2 className="font-mono text-xs tracking-widest text-muted-foreground uppercase">
          {title}
        </h2>
        {aside && (
          <div className="font-mono text-xs text-muted-foreground tabular-nums">{aside}</div>
        )}
      </div>
      {children}
    </section>
  )
}
