import { Outlet, createRootRoute } from "@tanstack/react-router"
import { AsciiLogo } from "@/components/ascii-logo"
import { Header } from "@/components/header"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"

export const Route = createRootRoute({
  component: () => (
    <TooltipProvider>
      <div className="flex min-h-svh flex-col">
        <Header />
        <main className="mx-auto w-full max-w-2xl flex-1 space-y-8 px-4 pt-4 pb-16">
          <AsciiLogo />
          <p className="font-mono text-xs text-muted-foreground">
            Device to device. No upload, no account. End-to-end encrypted.
          </p>
          <Outlet />
        </main>
      </div>
      <Toaster />
    </TooltipProvider>
  ),
  notFoundComponent: () => (
    <p className="font-mono text-sm text-muted-foreground">
      404 // nothing here.{" "}
      <a className="underline" href="/">
        Send a file
      </a>
    </p>
  ),
})
