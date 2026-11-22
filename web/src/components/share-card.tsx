import { CliBox } from "@/components/cli-box"
import FluxLines from "@/components/flux-lines"
import { LiveDot } from "@/components/live-dot"
import { QR } from "@/components/qr"
import { useTheme } from "@/components/theme-provider"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Spinner } from "@/components/ui/spinner"
import { IconLock } from "@tabler/icons-react"
import { Fragment } from "react"

/** The live share card: QR, code, link and CLI command. `link` is the full URL, which only the caller knows (web origin vs the app's server). */
export function Share({
  roomId,
  secret,
  link,
  receivers,
}: {
  roomId: string
  secret: string
  link: string
  receivers: number
}) {
  const { theme } = useTheme()
  const code = roomId && secret ? `${roomId}-${secret}` : ""
  return (
    <section className="flex flex-col gap-3" aria-label="Share">
      <div className="relative isolate overflow-hidden rounded-3xl border bg-card text-card-foreground">
        {/* the link is live: a slow field behind the code, faded toward the QR so it never fights the text */}
        <div className="pointer-events-none absolute inset-0 -z-10 opacity-35 mask-l-from-50% dark:opacity-80 mask-l-to-100%">
          <FluxLines lightMode={theme === "light"} speed={0.3} />
        </div>
        {!code ? (
          <Marker role="status" className="justify-center py-24">
            <MarkerIcon>
              <Spinner />
            </MarkerIcon>
            <MarkerContent className="shimmer">Creating your link…</MarkerContent>
          </Marker>
        ) : (
          <div className="flex flex-col gap-6 p-5 sm:flex-row sm:items-center sm:p-6">
            <QR value={link} className="self-center sm:self-auto" />
            <div className="flex min-w-0 flex-1 flex-col gap-4">
              <p className="flex flex-wrap items-center gap-1.5 select-all" aria-label="Share code">
                <span className="mb-1 basis-full font-pixel text-5xl leading-none tabular-nums">
                  {roomId}
                </span>
                {secret.split("-").map((w, i) => (
                  <Fragment key={i}>
                    <span className="sr-only">-</span>
                    <span className="rounded-lg bg-muted px-2 py-1 font-mono text-sm">{w}</span>
                  </Fragment>
                ))}
              </p>
              <CliBox command={link} prompt={false} />
              <CliBox command={`warpdrop receive ${code}`} />
            </div>
          </div>
        )}
        {code && (
          <div className="flex items-center gap-2.5 border-t bg-card/70 px-5 py-3 text-sm backdrop-blur-sm sm:px-6">
            <LiveDot />
            {receivers === 0 ? (
              <>
                <span className="shimmer">Waiting for someone to open the link</span>
                <span className="ml-auto hidden text-muted-foreground sm:inline">
                  Live while this tab is open
                </span>
              </>
            ) : (
              <span>
                {receivers < 10
                  ? "Link is live. More people can still join."
                  : "Full: 10 people are connected."}
              </span>
            )}
          </div>
        )}
      </div>
      <Marker className="text-xs">
        <MarkerIcon>
          <IconLock />
        </MarkerIcon>
        <MarkerContent>
          The words after the number never reach our server. Without them the link is useless, even
          to someone who guesses the number.
        </MarkerContent>
      </Marker>
    </section>
  )
}
