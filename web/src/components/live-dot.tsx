/** Pulsing dot for "this is live and listening". The ping stops under reduced motion (global CSS). */
export function LiveDot() {
  return (
    <span aria-hidden className="relative flex size-2 shrink-0">
      <span className="absolute inline-flex size-full animate-ping rounded-full bg-foreground/60" />
      <span className="relative inline-flex size-2 rounded-full bg-foreground" />
    </span>
  )
}
