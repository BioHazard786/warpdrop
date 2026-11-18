import { cn } from "@/lib/utils"
import { useEffect, useRef, useState } from "react"

// "ANSI Shadow" glyphs, 6 rows each. The solid layer is derived by blanking every box-drawing character.
const GLYPHS: Record<string, string[]> = {
  W: ["██╗    ██╗", "██║    ██║", "██║ █╗ ██║", "██║███╗██║", "╚███╔███╔╝", " ╚══╝╚══╝ "],
  A: [" █████╗ ", "██╔══██╗", "███████║", "██╔══██║", "██║  ██║", "╚═╝  ╚═╝"],
  R: ["██████╗ ", "██╔══██╗", "██████╔╝", "██╔══██╗", "██║  ██║", "╚═╝  ╚═╝"],
  P: ["██████╗ ", "██╔══██╗", "██████╔╝", "██╔═══╝ ", "██║     ", "╚═╝     "],
  D: ["██████╗ ", "██╔══██╗", "██║  ██║", "██║  ██║", "██████╔╝", "╚═════╝ "],
  O: [" ██████╗ ", "██╔═══██╗", "██║   ██║", "██║   ██║", "╚██████╔╝", " ╚═════╝ "],
}

const word = (w: string) =>
  Array.from({ length: 6 }, (_, r) => [...w].map((c) => GLYPHS[c]![r]).join("")).join("\n")
const SHADOW = word("WARPDROP")
const SOLID = SHADOW.replace(/[^█\n]/g, " ")

const SIZE =
  "text-[8.5px] sm:text-[11px] md:text-[12.5px] lg:text-[14px] tracking-[-1px] leading-[125%] font-mono whitespace-pre select-none"

/** Typewriter-reveal ASCII wordmark. All timers and frames are cancelled on unmount. */
export function AsciiLogo({
  className,
  delay = 100,
  duration = 450,
}: {
  className?: string
  delay?: number
  duration?: number
}) {
  const [text, setText] = useState(() =>
    matchMedia("(prefers-reduced-motion: reduce)").matches ? SOLID : "",
  )
  const [typing, setTyping] = useState(false)
  const done = useRef(text === SOLID)

  useEffect(() => {
    if (done.current) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let raf: number | undefined
    void document.fonts.ready.then(() => {
      if (cancelled) return
      timer = setTimeout(() => {
        setTyping(true)
        const t0 = performance.now()
        const tick = (now: number) => {
          const t = Math.min((now - t0) / duration, 1)
          const eased = 1 - (1 - t) ** 3
          setText(SOLID.slice(0, Math.floor(eased * SOLID.length)))
          if (t < 1) raf = requestAnimationFrame(tick)
          else {
            setText(SOLID)
            setTyping(false)
            done.current = true
          }
        }
        raf = requestAnimationFrame(tick)
      }, delay)
    })
    return () => {
      cancelled = true
      clearTimeout(timer)
      if (raf !== undefined) cancelAnimationFrame(raf)
    }
  }, [delay, duration])

  return (
    <div
      className={cn("relative max-w-full overflow-hidden", className)}
      role="img"
      aria-label="WarpDrop"
    >
      <pre aria-hidden className={cn(SIZE, "invisible")}>
        {SHADOW}
      </pre>
      <pre aria-hidden className={cn(SIZE, "absolute top-0 left-0 text-muted-foreground")}>
        {SHADOW}
      </pre>
      <pre aria-hidden className={cn(SIZE, "absolute top-0 left-0 text-foreground")}>
        {text}
        {typing && (
          <span className="ml-px inline-block h-[1em] w-[0.8em] animate-pulse bg-muted-foreground/80 align-middle" />
        )}
      </pre>
    </div>
  )
}
