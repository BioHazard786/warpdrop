import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"
import MetallicPaint from "~/components/MetallicPaint"

// The alpha channel is the shape MetallicPaint fills, so this is a thick rounded check on a transparent square.
const CHECK = `data:image/svg+xml;utf8,${encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512"><path d="M96 272l112 112 208-240" fill="none" stroke="#fff" stroke-width="72" stroke-linecap="round" stroke-linejoin="round"/></svg>',
)}`

/** The "it arrived" mark: a liquid-chrome check. Only on screen for a finished receive, so it never runs for long. */
export function ChromeCheck({ className }: { className?: string }) {
  const light = useTheme().theme === "light"
  return (
    <div aria-hidden className={cn("size-52 shrink-0", className)}>
      <MetallicPaint imageSrc={CHECK} lightMode={light} scale={0.7} />
    </div>
  )
}
