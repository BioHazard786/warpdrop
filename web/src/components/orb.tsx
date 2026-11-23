import Strands from "@/components/strands"
import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"

// Our own palettes (React Bits ships none): seven colours each, in the order Strands maps them to its roles.
const VARIANTS = {
  default: { colors: undefined, speed: 1, energy: 0.5 },
  // searching: cool and slow
  calm: {
    colors: ["#7dd3fc", "#38bdf8", "#bae6fd", "#f0f9ff", "#a5f3fc", "#3b82f6", "#1e3a8a"],
    speed: 0.7,
    energy: 0.35,
  },
  // connecting: warm and restless
  ember: {
    colors: ["#f87171", "#dc2626", "#fb923c", "#ffedd5", "#fde68a", "#c2410c", "#7f1d1d"],
    speed: 1,
    energy: 0.55,
  },
  // waiting on the other side: green into violet
  aurora: {
    colors: ["#34d399", "#10b981", "#5eead4", "#ecfdf5", "#a5b4fc", "#7c3aed", "#312e81"],
    speed: 0.85,
    energy: 0.5,
  },
}

/** The "something is happening" orb for waiting states. `energy` (0–1) sets how lively it is; `listening` adds a speech-like pulse. */
export function Orb({
  variant = "default",
  energy,
  listening = false,
  className,
}: {
  variant?: keyof typeof VARIANTS
  energy?: number
  listening?: boolean
  className?: string
}) {
  const { theme } = useTheme()
  const v = VARIANTS[variant]
  return (
    <div aria-hidden className={cn("size-36 shrink-0", className)}>
      <Strands
        {...(v.colors && { colors: v.colors })}
        speed={v.speed}
        energy={energy ?? v.energy}
        listening={listening}
        lightMode={theme === "light"}
        maxDevicePixelRatio={2}
      />
    </div>
  )
}
