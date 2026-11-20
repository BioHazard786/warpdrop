import { FileList } from "@/components/file-list"
import { Section } from "@/components/section"
import { TransportBadge } from "@/components/transport-badge"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes, formatETA, formatSpeed } from "@/lib/format"
import { cn } from "@/lib/utils"
import { useReceive } from "@/store/receive"
import { downloadMode } from "@/transfer/download"
import { isValidSecret } from "@/transfer/words"
import {
  IconAlertTriangle,
  IconCheck,
  IconCircleDashed,
  IconDownload,
  IconInfoCircle,
} from "@tabler/icons-react"
import { Link, createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"

export const Route = createFileRoute("/r/$roomId")({ component: ReceivePage })

const STEPS = [
  ["connecting", "Connecting to the server"],
  ["negotiating", "Finding a route to the sender"],
  ["authenticating", "Verifying the secret"],
] as const

function ReceivePage() {
  const { roomId } = Route.useParams()
  const r = useReceive()
  // the secret lives in the URL fragment, which browsers never send to any server
  const [secret, setSecret] = useState(() => {
    const h = decodeURIComponent(location.hash.slice(1))
    return isValidSecret(h) ? h : ""
  })
  useCloseConfirmation(r.status === "receiving" || r.status === "reconnecting")

  // Start/stop the transfer in an effect (never during render). Cleanup also covers navigating away and StrictMode's double mount.
  useEffect(() => {
    if (!secret) return
    useReceive.getState().start(roomId, secret)
    return () => useReceive.getState().reset()
  }, [roomId, secret])

  if (!secret) return <SecretForm roomId={roomId} onSecret={setSecret} />

  if (r.status === "failed") {
    const wrong = r.code === "auth_failed"
    return (
      <Empty className="flex-none border border-dashed py-12">
        <EmptyHeader>
          <EmptyMedia variant="icon" className="bg-destructive/10 text-destructive">
            <IconAlertTriangle />
          </EmptyMedia>
          <EmptyTitle>Transfer failed</EmptyTitle>
          <EmptyDescription>{r.reason ?? "Something went wrong."}</EmptyDescription>
        </EmptyHeader>
        <div className="flex gap-2">
          {wrong && (
            <Button
              onClick={() => {
                setSecret("")
                history.replaceState(null, "", location.pathname)
              }}
            >
              Try different words
            </Button>
          )}
          <Link to="/" className={buttonVariants({ variant: "outline" })}>
            Send files instead
          </Link>
        </div>
      </Empty>
    )
  }

  if (r.status === "done") {
    return (
      <Empty className="flex-none border border-dashed py-12">
        <EmptyHeader>
          <EmptyMedia variant="icon" className="bg-foreground text-background">
            <IconCheck />
          </EmptyMedia>
          <EmptyTitle className="text-base">
            Received {r.files.length} {r.files.length === 1 ? "file" : "files"}
          </EmptyTitle>
          <EmptyDescription>
            {formatBytes(r.total)} · every file's checksum matched.
            {r.files.length > 1 && " Saved as one zip."}
          </EmptyDescription>
        </EmptyHeader>
        <Link to="/" className={buttonVariants({ variant: "outline" })}>
          Send files
        </Link>
      </Empty>
    )
  }

  if (
    r.status === "idle" ||
    r.status === "connecting" ||
    r.status === "negotiating" ||
    r.status === "authenticating"
  ) {
    const at = Math.max(
      0,
      STEPS.findIndex(([k]) => k === r.status),
    )
    return (
      <Section title={`Transfer ${roomId}`}>
        <Card>
          <CardContent>
            <ol className="flex flex-col gap-3" role="status" aria-live="polite">
              {STEPS.map(([k, label], i) => (
                <Marker
                  key={k}
                  render={<li />}
                  className={cn(i === at && "text-foreground", i > at && "opacity-50")}
                >
                  <MarkerIcon>
                    {i < at ? <IconCheck /> : i === at ? <Spinner /> : <IconCircleDashed />}
                  </MarkerIcon>
                  <MarkerContent className={cn(i === at && "shimmer")}>{label}</MarkerContent>
                </Marker>
              ))}
            </ol>
          </CardContent>
        </Card>
      </Section>
    )
  }

  const pct = r.total ? Math.min(100, (r.received / r.total) * 100) : 0
  const renamed = r.files.filter((f) => f.safeName !== f.name).length
  const memory = downloadMode() === "memory" && r.total > 1024 ** 3
  const offered = r.status === "offered"
  const files = r.files.map((f) => ({ ...f, name: f.safeName, title: f.name }))

  return (
    <div className="flex flex-col gap-10">
      {!offered && (
        <Card>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-end justify-between gap-4">
              <span className="font-pixel text-5xl leading-none tabular-nums sm:text-6xl">
                {Math.floor(pct)}%
              </span>
              <span className="text-right font-mono text-xs text-muted-foreground tabular-nums">
                {formatSpeed(r.speed)}
                <br />
                {formatETA(r.eta)} left
              </span>
            </div>
            <Progress value={pct} aria-label="Download progress" />
            <div className="flex justify-between font-mono text-xs text-muted-foreground tabular-nums">
              <span>
                {formatBytes(r.received)} / {formatBytes(r.total)}
              </span>
              <span>
                {r.filesDone}/{r.files.length} files
              </span>
            </div>
            {r.status === "reconnecting" && (
              <Marker role="status" className="text-xs">
                <MarkerIcon>
                  <Spinner />
                </MarkerIcon>
                <MarkerContent className="shimmer">Connection lost. Resuming…</MarkerContent>
              </Marker>
            )}
          </CardContent>
        </Card>
      )}

      <Section
        title={offered ? "Incoming files" : "Receiving"}
        aside={
          <>
            {r.transport && <TransportBadge transport={r.transport} />}
            {r.files.length} · {formatBytes(r.total)}
          </>
        }
      >
        <FileList
          files={files}
          checks={!offered}
          state={(i) =>
            offered || i > r.filesDone ? "idle" : i < r.filesDone ? "done" : "uploading"
          }
        />
        {renamed > 0 && (
          <Marker className="text-xs">
            <MarkerIcon>
              <IconInfoCircle />
            </MarkerIcon>
            <MarkerContent>
              {renamed} unsafe {renamed === 1 ? "name was" : "names were"} cleaned up before saving.
            </MarkerContent>
          </Marker>
        )}
        {memory && (
          <Alert variant="destructive">
            <IconAlertTriangle />
            <AlertDescription>
              This browser can't stream to disk, so the whole transfer is held in memory. Over 1 GB
              it may fail; use Chrome, Edge or Firefox, or the CLI.
            </AlertDescription>
          </Alert>
        )}
      </Section>

      {offered ? (
        <div className="flex flex-wrap gap-2">
          <Button size="lg" onClick={() => r.respond(true)}>
            <IconDownload data-icon="inline-start" /> Accept and download
          </Button>
          <Button size="lg" variant="outline" onClick={() => r.respond(false)}>
            Decline
          </Button>
        </div>
      ) : (
        <Button variant="destructive" className="self-start" onClick={r.cancel}>
          Cancel
        </Button>
      )}
    </div>
  )
}

function SecretForm({ roomId, onSecret }: { roomId: string; onSecret: (s: string) => void }) {
  const [typed, setTyped] = useState("")
  const words = typed.trim().toLowerCase().replace(/\s+/g, "-")
  const ok = isValidSecret(words)
  const count = words ? words.split("-").length : 0
  return (
    <Section title={`Transfer ${roomId}`}>
      <Card>
        <CardContent>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              if (ok) onSecret(words)
            }}
          >
            <FieldGroup>
              <Field data-invalid={(typed.length > 0 && count >= 4 && !ok) || undefined}>
                <FieldLabel htmlFor="secret">Four secret words</FieldLabel>
                <Input
                  id="secret"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  placeholder="amber otter kite moth"
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                  className="h-10 font-mono"
                  aria-label="Four secret words"
                  aria-invalid={typed.length > 0 && count >= 4 && !ok}
                  autoFocus
                />
                <FieldDescription className="flex justify-between gap-4">
                  <span>
                    This link has no secret. Ask the sender for the words after the number.
                  </span>
                  <span className="shrink-0 font-mono tabular-nums">{count}/4</span>
                </FieldDescription>
              </Field>
              <Button type="submit" size="lg" disabled={!ok} className="self-start">
                Connect
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </Section>
  )
}
