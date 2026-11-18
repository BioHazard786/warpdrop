import { IconPlayerStop, IconRefresh } from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { CliBox } from "@/components/cli-box"
import { CopyButton } from "@/components/copy-button"
import { Dropzone } from "@/components/dropzone"
import { FileIcon } from "@/components/file-icon"
import { PeerRow } from "@/components/peer-row"
import { QR } from "@/components/qr"
import { Section } from "@/components/section"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes } from "@/lib/format"
import { useIsSending, useSend } from "@/store/send"

export const Route = createFileRoute("/")({ component: SendPage })

function SendPage() {
  const s = useSend()
  const sending = useIsSending()
  useCloseConfirmation(s.phase === "ready" || s.phase === "reconnecting")

  if (s.phase === "idle") {
    return (
      <div className="space-y-6">
        <Dropzone onFiles={s.start} />
        <Section title="Command line">
          <CliBox />
          <p className="text-xs text-muted-foreground">
            The CLI sends to and receives from browsers:{" "}
            <code className="font-mono">warpdrop send file.zip</code>
          </p>
        </Section>
      </div>
    )
  }

  const code = s.roomId && s.secret ? `${s.roomId}-${s.secret}` : ""
  const link = code ? `${location.origin}/r/${s.roomId}#${s.secret}` : ""
  const total = s.files.reduce((n, f) => n + f.size, 0)

  if (s.phase === "error" || s.phase === "closed") {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{s.phase === "error" ? "Transfer stopped" : "Transfer closed"}</EmptyTitle>
          <EmptyDescription>{s.error ?? "The share link no longer works."}</EmptyDescription>
        </EmptyHeader>
        <Button onClick={s.reset}>
          <IconRefresh data-icon="inline-start" /> Start over
        </Button>
      </Empty>
    )
  }

  return (
    <div className="space-y-8">
      {s.phase === "reconnecting" && (
        <p role="status" className="rounded-lg border border-border bg-muted/50 px-3 py-2 text-xs">
          <Spinner className="mr-2 inline" /> Connection to the server was lost. Reconnecting; your
          link stays valid.
        </p>
      )}

      <Section title="Share" aside={code ? "valid while this tab stays open" : undefined}>
        {!code ? (
          <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
            <Spinner /> Creating your link…
          </p>
        ) : (
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <QR value={link} />
            <div className="min-w-0 flex-1 space-y-3">
              <p
                className="font-mono text-lg break-all select-all sm:text-xl"
                aria-label="Share code"
              >
                {code}
              </p>
              <div className="flex flex-wrap gap-2">
                <CopyButton value={link} label="Copy link" />
                <CopyButton value={code} label="Copy code" />
              </div>
              <CliBox command={`warpdrop receive ${code}`} />
              <p className="text-xs text-muted-foreground">
                The words after the number never reach our server. Without them the link can't be
                used, even by someone who guesses the number.
              </p>
            </div>
          </div>
        )}
      </Section>

      <Section title={`Receivers (${s.peers.length})`}>
        {s.peers.length === 0 ? (
          <Empty className="border border-dashed">
            <EmptyHeader>
              <EmptyTitle>Waiting for someone to open the link</EmptyTitle>
              <EmptyDescription>Up to 10 people can receive at the same time.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="space-y-2">
            {s.peers.map((p) => (
              <PeerRow key={p.id} peer={p} onCancel={() => s.cancelPeer(p.id)} />
            ))}
          </ul>
        )}
      </Section>

      <Section title={`Files (${s.files.length})`} aside={formatBytes(total)}>
        <ul className="divide-y divide-border rounded-lg border border-border">
          {s.files.slice(0, 50).map((f, i) => (
            <li key={i} className="flex items-center gap-3 px-3 py-2 text-sm">
              <FileIcon name={f.name} type="" />
              <span className="min-w-0 flex-1 truncate">{f.name}</span>
              <span className="font-mono text-xs text-muted-foreground tabular-nums">
                {formatBytes(f.size)}
              </span>
            </li>
          ))}
          {s.files.length > 50 && (
            <li className="px-3 py-2 text-xs text-muted-foreground">
              and {s.files.length - 50} more
            </li>
          )}
        </ul>
      </Section>

      <Button
        variant="outline"
        onClick={() =>
          sending && !confirm("A transfer is in progress. Stop it?") ? undefined : s.reset()
        }
      >
        <IconPlayerStop data-icon="inline-start" /> Stop sharing
      </Button>
    </div>
  )
}
