import { FileList } from "@/components/file-list"
import { Orb } from "@/components/orb"
import { Section } from "@/components/section"
import { TransportBadge } from "@/components/transport-badge"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button, buttonVariants } from "@/components/ui/button"
import { Card, CardContent, CardFooter } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Progress } from "@/components/ui/progress"
import { Spinner } from "@/components/ui/spinner"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes, formatCode, formatETA, formatSpeed } from "@/lib/format"
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
  IconX,
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
  // a nearby invite carries a one-time ticket instead of the secret words: "#n-<16 hex>"
  const [ticket] = useState(() => /^n-([0-9a-f]{16})$/.exec(location.hash.slice(1))?.[1] ?? "")
  useCloseConfirmation(r.status === "receiving" || r.status === "reconnecting")

  // Start/stop the transfer in an effect (never during render). Cleanup also covers navigating away and StrictMode's double mount.
  useEffect(() => {
    if (!secret && !ticket) return
    useReceive.getState().start(roomId, secret, ticket)
    return () => useReceive.getState().reset()
  }, [roomId, secret, ticket])

  if (!secret && !ticket) return <SecretForm roomId={roomId} onSecret={setSecret} />

  if (r.status === "failed") {
    const wrong = r.code === "auth_failed" && !ticket
    const declined = r.code === "declined"
    return (
      <div className="flex flex-col items-start gap-6">
        <div
          className={cn(
            "flex size-12 items-center justify-center rounded-2xl",
            declined ? "bg-muted text-muted-foreground" : "bg-destructive/10 text-destructive",
          )}
        >
          {declined ? <IconX className="size-6" /> : <IconAlertTriangle className="size-6" />}
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
            {declined ? "You declined the files" : "Transfer failed"}
          </h1>
          <p className="text-pretty text-muted-foreground">
            {declined
              ? "Nothing was downloaded, and the sender has been told."
              : (r.reason ?? "Something went wrong.")}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
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
      </div>
    )
  }

  if (r.status === "done") {
    return (
      <div className="flex flex-col items-start gap-6">
        <div className="flex size-12 items-center justify-center rounded-2xl bg-foreground text-background">
          <IconCheck className="size-6" />
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
            Received {r.files.length} {r.files.length === 1 ? "file" : "files"}
            {r.from && ` from ${r.from}`}
          </h1>
          <p className="text-pretty text-muted-foreground">
            {formatBytes(r.total)}, and every file's checksum matched.
            {r.files.length > 1 && " Saved as one zip in your downloads."}
          </p>
        </div>
        <Link to="/" className={buttonVariants({ variant: "outline" })}>
          Send files
        </Link>
      </div>
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
      <div className="flex flex-col items-center gap-8 py-4">
        <Orb variant="ember" energy={0.4 + at * 0.25} className="size-44 sm:size-52" />
        <div className="flex flex-col items-center gap-4">
          <p className="text-sm text-muted-foreground">
            Transfer <span className="font-mono text-foreground">{roomId}</span>
          </p>
          <ol className="flex flex-col gap-3" role="status" aria-live="polite">
            {STEPS.map(([k, stepLabel], i) => {
              const label = k === "authenticating" && ticket ? "Securing the connection" : stepLabel
              return (
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
              )
            })}
          </ol>
        </div>
      </div>
    )
  }

  const pct = r.total ? Math.min(100, (r.received / r.total) * 100) : 0
  const renamed = r.files.filter((f) => f.safeName !== f.name).length
  const memory = downloadMode() === "memory" && r.total > 1024 ** 3
  const offered = r.status === "offered"
  const files = r.files.map((f) => ({ ...f, name: f.safeName, title: f.name }))

  const n = `${r.files.length} ${r.files.length === 1 ? "file" : "files"}`
  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
          {offered
            ? `${r.from || "Someone"} wants to send you ${n}`
            : `Receiving ${n} from ${r.from || "the sender"}`}
        </h1>
        <p className="flex items-center gap-2 text-muted-foreground">
          {formatBytes(r.total)} in total
          {r.transport && <TransportBadge transport={r.transport} />}
        </p>
      </div>

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

      <Section title="Files">
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

      {offered && r.sas ? (
        <Card>
          <CardContent className="flex flex-col gap-2">
            <span className="font-mono text-xs tracking-wider text-muted-foreground uppercase">
              Check this code with {r.from || "the sender"}
            </span>
            <p
              className="font-pixel text-5xl leading-none tabular-nums sm:text-6xl"
              aria-label={`Verification code ${formatCode(r.sas).replace(" ", ", ")}`}
            >
              {formatCode(r.sas)}
            </p>
            <p className="text-xs text-muted-foreground">
              It must match the code on their screen. If it doesn't, decline: someone may be
              intercepting the connection.
            </p>
          </CardContent>
          <CardFooter className="flex-wrap gap-2">
            <OfferActions respond={r.respond} />
          </CardFooter>
        </Card>
      ) : offered ? (
        <div className="flex flex-wrap gap-2">
          <OfferActions respond={r.respond} />
        </div>
      ) : (
        <Button variant="destructive" className="self-start" onClick={r.cancel}>
          Cancel
        </Button>
      )}
    </div>
  )
}

function OfferActions({ respond }: { respond: (accept: boolean) => void }) {
  return (
    <>
      <Button onClick={() => respond(true)}>
        <IconDownload data-icon="inline-start" /> Accept and download
      </Button>
      <Button variant="outline" onClick={() => respond(false)}>
        Decline
      </Button>
    </>
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
                  className="font-mono"
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
              <Button type="submit" disabled={!ok} className="self-start">
                Connect
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </Section>
  )
}
