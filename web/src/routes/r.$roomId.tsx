import { IconAlertTriangle, IconCheck, IconDownload } from "@tabler/icons-react"
import { Link, createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { FileIcon } from "@/components/file-icon"
import { Section } from "@/components/section"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes, formatETA, formatSpeed } from "@/lib/format"
import { useReceive } from "@/store/receive"
import { downloadMode } from "@/transfer/download"
import { isValidSecret } from "@/transfer/words"

export const Route = createFileRoute("/r/$roomId")({ component: ReceivePage })

const STEP = {
  connecting: "Connecting to the server…",
  negotiating: "Finding a route to the sender…",
  authenticating: "Verifying the secret…",
  reconnecting: "Connection lost. Resuming…",
} as const

function ReceivePage() {
  const { roomId } = Route.useParams()
  const r = useReceive()
  // the secret lives in the URL fragment, which browsers never send to any server
  const [secret, setSecret] = useState(() => {
    const h = decodeURIComponent(location.hash.slice(1))
    return isValidSecret(h) ? h : ""
  })
  const [typed, setTyped] = useState("")
  useCloseConfirmation(r.status === "receiving" || r.status === "reconnecting")

  // Start/stop the transfer in an effect (never during render). Cleanup also covers navigating away and StrictMode's double mount.
  useEffect(() => {
    if (!secret) return
    useReceive.getState().start(roomId, secret)
    return () => useReceive.getState().reset()
  }, [roomId, secret])

  if (!secret) {
    const words = typed.trim().toLowerCase().replace(/\s+/g, "-")
    const ok = isValidSecret(words)
    return (
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (ok) setSecret(words)
        }}
      >
        <Section title={`Transfer ${roomId}`}>
          <p className="text-sm text-muted-foreground">
            This link has no secret. Ask the sender for the four words that follow the number.
          </p>
          <Input
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder="amber otter kite moth"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            className="font-mono"
            aria-label="Four secret words"
            aria-invalid={typed.length > 0 && !ok}
            autoFocus
          />
          <Button type="submit" disabled={!ok}>
            Connect
          </Button>
        </Section>
      </form>
    )
  }

  if (r.status === "failed") {
    const wrong = r.code === "auth_failed"
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle className="flex items-center justify-center gap-2">
            <IconAlertTriangle className="size-4" aria-hidden /> Transfer failed
          </EmptyTitle>
          <EmptyDescription>{r.reason ?? "Something went wrong."}</EmptyDescription>
        </EmptyHeader>
        <div className="flex gap-2">
          {wrong && (
            <Button
              onClick={() => {
                setSecret("")
                setTyped("")
                history.replaceState(null, "", location.pathname)
              }}
            >
              Try different words
            </Button>
          )}
          <Button variant="outline" render={<Link to="/" />}>
            Send files instead
          </Button>
        </div>
      </Empty>
    )
  }

  if (r.status === "done") {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle className="flex items-center justify-center gap-2">
            <IconCheck className="size-4" aria-hidden /> Received {r.files.length}{" "}
            {r.files.length === 1 ? "file" : "files"}
          </EmptyTitle>
          <EmptyDescription>
            {formatBytes(r.total)} · every file's checksum matched.
            {r.files.length > 1 && " Saved as one zip."}
          </EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" render={<Link to="/" />}>
          Send files
        </Button>
      </Empty>
    )
  }

  if (
    r.status === "idle" ||
    r.status === "connecting" ||
    r.status === "negotiating" ||
    r.status === "authenticating"
  ) {
    return (
      <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner /> {STEP[r.status === "idle" ? "connecting" : r.status]}
      </p>
    )
  }

  const pct = r.total ? Math.min(100, (r.received / r.total) * 100) : 0
  const renamed = r.files.filter((f) => f.safeName !== f.name).length
  const memory = downloadMode() === "memory" && r.total > 1024 ** 3

  return (
    <div className="space-y-6">
      <Section
        title={r.status === "offered" ? "Incoming files" : "Receiving"}
        aside={
          <span className="flex items-center gap-2">
            {r.transport && (
              <Badge variant="outline">{r.transport === "rtc" ? "P2P" : "RELAY"}</Badge>
            )}
            {formatBytes(r.total)}
          </span>
        }
      >
        <ul className="divide-y divide-border rounded-lg border border-border">
          {r.files.slice(0, 50).map((f, i) => (
            <li key={f.id} className="flex items-center gap-3 px-3 py-2 text-sm">
              <FileIcon name={f.safeName} type={f.type} />
              <span className="min-w-0 flex-1 truncate" title={f.name}>
                {f.safeName}
              </span>
              <span className="font-mono text-xs text-muted-foreground tabular-nums">
                {formatBytes(f.size)}
              </span>
              {r.status !== "offered" && i < r.filesDone && (
                <IconCheck className="size-4 text-muted-foreground" aria-label="Received" />
              )}
            </li>
          ))}
          {r.files.length > 50 && (
            <li className="px-3 py-2 text-xs text-muted-foreground">
              and {r.files.length - 50} more
            </li>
          )}
        </ul>
        {renamed > 0 && (
          <p className="text-xs text-muted-foreground">
            {renamed} unsafe {renamed === 1 ? "name was" : "names were"} cleaned up before saving.
          </p>
        )}
        {memory && (
          <p role="alert" className="text-xs text-destructive">
            This browser can't stream to disk, so the whole transfer is held in memory. Over 1 GB it
            may fail; use Chrome, Edge or Firefox, or the CLI.
          </p>
        )}
      </Section>

      {r.status === "offered" ? (
        <div className="flex gap-2">
          <Button onClick={() => r.respond(true)}>
            <IconDownload data-icon="inline-start" /> Accept and download
          </Button>
          <Button variant="outline" onClick={() => r.respond(false)}>
            Decline
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          {r.status === "reconnecting" && (
            <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
              <Spinner /> {STEP.reconnecting}
            </p>
          )}
          <Progress value={pct} aria-label="Download progress" />
          <div className="flex justify-between font-mono text-xs text-muted-foreground tabular-nums">
            <span>
              {formatBytes(r.received)} / {formatBytes(r.total)}
            </span>
            <span>
              {formatSpeed(r.speed)} · {formatETA(r.eta)}
            </span>
          </div>
          <Button variant="outline" onClick={r.cancel}>
            Cancel
          </Button>
        </div>
      )}
    </div>
  )
}
