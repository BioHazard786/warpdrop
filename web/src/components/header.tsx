import { IconBrandGithub, IconDeviceDesktop, IconMoon, IconSun } from "@tabler/icons-react"
import { Link } from "@tanstack/react-router"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"

const NEXT = { light: "dark", dark: "system", system: "light" } as const
const ICON = { light: IconSun, dark: IconMoon, system: IconDeviceDesktop }

export function Header() {
  const { theme, setTheme } = useTheme()
  const Icon = ICON[theme]
  return (
    <header className="sticky top-0 z-50 bg-background/90 backdrop-blur">
      <div className="mx-auto flex h-14 max-w-2xl items-center justify-between px-4">
        <Link
          to="/"
          className="font-mono text-xs tracking-widest uppercase text-muted-foreground hover:text-foreground"
        >
          warpdrop
        </Link>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            render={
              <a href="https://github.com/BioHazard786/Warpdrop" target="_blank" rel="noreferrer" />
            }
            aria-label="Source on GitHub"
          >
            <IconBrandGithub />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setTheme(NEXT[theme])}
            aria-label={`Theme: ${theme}. Click to change.`}
          >
            <Icon />
          </Button>
        </div>
      </div>
    </header>
  )
}
