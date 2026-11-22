import { AsciiLogo } from "@/components/ascii-logo"
import { ThemeToggle } from "@/components/theme-toggle"
import { Marker, MarkerContent } from "@/components/ui/marker"
import { Toaster } from "@/components/ui/toast"
import type { Transfer } from "~/api"
import { Section } from "@/components/section"
import { Home, ReceivePage, TransferRow } from "~/pages"
import { useApp } from "~/store"

/**
 * One transfer fills the page, like the web app: a decision waiting on you first, then live ones, then the latest
 * result. Nearby sends never take over: they live on the Nearby tab, one device tile each.
 */
function focused(ts: Transfer[]): Transfer | undefined {
  ts = ts.filter((t) => !(t.kind === "send" && t.nearby))
  return ts.find((t) => t.status === "offered" && !t.done) ?? ts.find((t) => !t.done) ?? ts[0]
}

export function App() {
  const transfers = useApp((s) => s.transfers)
  const t = focused(transfers)
  const receiving = t?.kind === "receive"
  // several devices can send at once: the ones not on screen are listed under the focused one
  const others = receiving ? transfers.filter((x) => x !== t && x.kind === "receive" && !x.done) : []
  return (
    <Toaster>
      <div className="flex min-h-svh flex-col">
        <ThemeToggle />
        <main className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-10 px-4 pt-16 pb-16 sm:px-6 sm:pt-20">
          {!receiving && (
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
          {receiving ? <ReceivePage t={t} /> : <Home share={t} />}
          {others.length > 0 && (
            <Section title="Also receiving" aside={others.length}>
              <ul className="flex flex-col gap-2">
                {others.map((x) => (
                  <TransferRow key={x.id} t={x} />
                ))}
              </ul>
            </Section>
          )}
        </main>
        <footer className="mx-auto w-full max-w-3xl px-4 pb-8 sm:px-6">
          <Marker variant="separator" className="font-mono text-xs">
            <MarkerContent>
              warpdrop · desktop ·{" "}
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
