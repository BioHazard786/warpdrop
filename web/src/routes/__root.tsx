import { AsciiLogo } from "@/components/ascii-logo"
import { NearbyHost } from "@/components/nearby-host"
import { ThemeToggle } from "@/components/theme-toggle"
import { buttonVariants } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Marker, MarkerContent } from "@/components/ui/marker"
import { Toaster } from "@/components/ui/toast"
import { Link, Outlet, createRootRoute, useLocation } from "@tanstack/react-router"

export const Route = createRootRoute({
  component: Root,
  notFoundComponent: NotFound,
})

function Root() {
  // a receiver landed here for one transfer, and About has its own intro: the product headline fits neither
  const ownHero = useLocation({
    select: (l) => l.pathname.startsWith("/r/") || l.pathname === "/about",
  })
  return (
    <Toaster>
      <div className="flex min-h-svh flex-col">
        <ThemeToggle />
        <main className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-10 px-4 pt-16 pb-16 sm:px-6 sm:pt-20">
          {!ownHero && (
            <section className="flex flex-col gap-5">
              <AsciiLogo />
              <div className="flex flex-col gap-2">
                <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
                  Files, straight from your device to theirs.
                </h1>
                <p className="text-base leading-relaxed text-pretty text-muted-foreground">
                  No upload, no account, no size limit. Open the link on any device, or use the CLI.
                </p>
              </div>
            </section>
          )}
          <Outlet />
          <NearbyHost />
        </main>
        <footer className="mx-auto w-full max-w-3xl px-4 pb-8 sm:px-6">
          <Marker variant="separator" className="font-mono text-xs">
            <MarkerContent>
              warpdrop · <Link to="/about">about</Link> ·{" "}
              <a href="https://github.com/BioHazard786/Warpdrop" target="_blank" rel="noreferrer">
                open source
              </a>
            </MarkerContent>
          </Marker>
        </footer>
      </div>
    </Toaster>
  )
}

function NotFound() {
  return (
    <Empty className="flex-none border-2 border-dashed py-12">
      <EmptyHeader>
        <EmptyTitle className="font-pixel text-6xl leading-none">404</EmptyTitle>
        <EmptyDescription>Nothing here.</EmptyDescription>
      </EmptyHeader>
      <a href="/" className={buttonVariants({ variant: "outline" })}>
        Send a file
      </a>
    </Empty>
  )
}
