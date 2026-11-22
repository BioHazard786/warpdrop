import Strands from "@/components/strands"
import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"

/** The "something is happening" orb for waiting states. `energy` (0–1) sets how lively it is; `listening` adds a speech-like pulse. */
export function Orb({
  energy = 0.5,
  listening = false,
  className,
}: {
  energy?: number
  listening?: boolean
  className?: string
}) {
  const { theme } = useTheme()
  return (
    <div aria-hidden className={cn("size-36 shrink-0", className)}>
      <Strands
        energy={energy}
        listening={listening}
        lightMode={theme === "light"}
        maxDevicePixelRatio={2}
      />
    </div>
  )
}
