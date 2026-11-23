// The web app's pages (routes/index.tsx and routes/r.$roomId.tsx) rebuilt from the same components over the Go
// engine's state instead of the browser transfer stores. Keep the markup in step with those two files.
import {
  IconAlertTriangle,
  IconArrowDownLeft,
  IconArrowUpRight,
  IconExternalLink,
  IconHistory,
  IconLink,
  IconRadar2,
  IconFolderOpen,
  IconCheck,
  IconCircleDashed,
  IconDownload,
  IconInfoCircle,
  IconLock,
  IconPlayerStop,
  IconPlus,
  IconX,
} from "@tabler/icons-react"
import { useEffect, useRef, useState } from "react"
import { Dropzone } from "@/components/dropzone"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemActions, ItemContent, ItemDescription, ItemMedia, ItemTitle } from "@/components/ui/item"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { FileList } from "@/components/file-list"
import { NearbyDevices, type TileStatus } from "@/components/nearby-devices"
import { Orb } from "@/components/orb"
import { PeerRow } from "@/components/peer-row"
import { Section } from "@/components/section"
import { Share } from "@/components/share-card"
import { TransportBadge } from "@/components/transport-badge"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardFooter } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Progress } from "@/components/ui/progress"
import { formatBytes, formatCode, formatETA, formatSpeed, SpeedMeter } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Pulse } from "~/components/loading-ui/pulse"
import { ChromeCheck } from "~/components/effects"
import type { Transfer } from "~/api"
import { actions, useApp } from "~/store"

const files = (t: Transfer) => (t.files ?? []).map((f) => ({ name: f.name, size: f.size }))

/** Smoothed speed and time left, sampled at the rate the engine reports progress. */
function useRate(sent: number, total: number, live: boolean) {
  const m = useRef(new SpeedMeter())
  const speed = live ? m.current.sample(sent) : 0
  return { speed, eta: live ? m.current.eta(total - sent) : 0 }
}

// ---------------------------------------------------------------- idle (web: routes/index.tsx SendPage)

/** The Link page: share files with a code, or keep showing the running share. */
export function LinkPage({ share }: { share?: Transfer }) {
  return share ? <SendPage t={share} /> : <Dropzone onFiles={() => {}} onBrowse={actions.sendWithCode} />
}

/** The Receive page: type a code when idle, otherwise the focused incoming transfer with any others listed under it. */
export function ReceivePane({ t, others }: { t?: Transfer; others: Transfer[] }) {
  return (
    <div className="flex flex-col gap-10">
      {t ? <ReceivePage t={t} /> : <CodeForm />}
      {others.length > 0 && (
        <Section title="Also receiving" aside={others.length}>
          <ul className="flex flex-col gap-2">
            {others.map((x) => (
              <TransferRow key={x.id} t={x} />
            ))}
          </ul>
        </Section>
      )}
    </div>
  )
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })
function ago(ms: number) {
  const m = Math.round((Date.now() - ms) / 60000)
  if (m < 1) return "just now"
  if (m < 60) return rtf.format(-m, "minute")
  const h = Math.round(m / 60)
  return h < 24 ? rtf.format(-h, "hour") : new Date(ms).toLocaleDateString()
}

/** Why a finished transfer didn't deliver, in a word or two. */
const why = (t: Transfer) =>
  t.status === "declined" ? "Declined" : t.status === "cancelled" ? "Cancelled" : (DECLINES[t.error] ?? (t.error || "Failed"))

function Stat({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="flex flex-col gap-1 rounded-xl border p-4">
      <span className="font-mono text-xs tracking-wider text-muted-foreground uppercase">{label}</span>
      <span className="font-pixel text-3xl leading-none tabular-nums">{value}</span>
      <span className="text-xs text-muted-foreground">{detail}</span>
    </div>
  )
}

/** A finished transfer: what went where, how it ended, and when. */
function HistoryRow({ t }: { t: Transfer }) {
  const sent = t.kind === "send"
  const ok = t.status === "done"
  const fs = t.files ?? []
  const name = fs.length ? fs[0].name + (fs.length > 1 ? ` and ${fs.length - 1} more` : "") : "Files"
  const peer = t.peer ? `${sent ? "to" : "from"} ${t.peer}` : sent ? "by link" : ""
  const path = t.paths?.[0]
  return (
    <Item variant="outline" render={<li />} className="rounded-xl">
      <ItemMedia
        variant="icon"
        className={cn("size-9 rounded-lg", ok ? "bg-muted" : "bg-destructive/10 text-destructive")}
      >
        {sent ? <IconArrowUpRight /> : <IconArrowDownLeft />}
      </ItemMedia>
      <ItemContent className="min-w-0">
        <ItemTitle className="w-full truncate">{name}</ItemTitle>
        <ItemDescription className="truncate font-mono text-xs tabular-nums">
          {peer && <>{peer} · </>}
          {formatBytes(t.total)}
          {!ok && <span className="text-destructive"> · {why(t)}</span>}
          {t.started > 0 && <span title={new Date(t.started).toLocaleString()}> · {ago(t.started)}</span>}
        </ItemDescription>
      </ItemContent>
      <ItemActions>
        {ok && t.transport && <TransportBadge transport={t.transport as "rtc" | "relay" | "lan"} />}
        {ok && !sent && fs.length === 1 && path && (
          <Button variant="ghost" size="icon-sm" aria-label={`Open ${name}`} onClick={() => actions.openFile(path)}>
            <IconExternalLink />
          </Button>
        )}
        {ok && !sent && (
          <Button variant="ghost" size="icon-sm" aria-label="Show in folder" onClick={() => actions.showFile(path)}>
            <IconFolderOpen />
          </Button>
        )}
        <Button variant="ghost" size="icon-sm" aria-label="Remove from list" onClick={() => actions.dismiss(t.id)}>
          <IconX />
        </Button>
      </ItemActions>
    </Item>
  )
}

type Filter = "all" | "send" | "receive"

/** Totals, what's running, and what already happened, newest first. Lives as long as the app does. */
export function ActivityPage() {
  const all = useApp((s) => s.transfers)
  const [filter, setFilter] = useState<Filter>("all")
  const [, tick] = useState(0)
  useEffect(() => {
    const i = setInterval(() => tick((n) => n + 1), 30_000) // keeps "5 minutes ago" honest
    return () => clearInterval(i)
  }, [])

  if (!all.length)
    return (
      <Empty className="min-h-80 border-2 border-dashed border-input">
        <EmptyHeader>
          <EmptyMedia variant="icon" className="size-11 rounded-xl">
            <IconHistory className="size-5" />
          </EmptyMedia>
          <EmptyTitle className="text-base">Nothing sent or received yet</EmptyTitle>
          <EmptyDescription>Every transfer since WarpDrop opened shows up here, with where it went.</EmptyDescription>
        </EmptyHeader>
        <EmptyContent className="flex-row flex-wrap justify-center">
          <Button variant="outline" size="sm" onClick={() => actions.setTab("nearby")}>
            <IconRadar2 data-icon="inline-start" /> Send nearby
          </Button>
          <Button variant="outline" size="sm" onClick={() => actions.setTab("link")}>
            <IconLink data-icon="inline-start" /> Share a link
          </Button>
        </EmptyContent>
      </Empty>
    )

  const delivered = (kind: string) => all.filter((t) => t.kind === kind && t.status === "done")
  const bytes = (ts: Transfer[]) => ts.reduce((n, t) => n + t.total, 0)
  const count = (ts: Transfer[]) => {
    const n = ts.reduce((k, t) => k + (t.files ?? []).length, 0)
    return `${n} ${n === 1 ? "file" : "files"}`
  }
  const running = all.filter((t) => !t.done).length
  const sent = delivered("send")
  const got = delivered("receive")
  const shown = filter === "all" ? all : all.filter((t) => t.kind === filter)
  const live = shown.filter((t) => !t.done)
  const past = shown.filter((t) => t.done)
  return (
    <div className="flex flex-col gap-10">
      <div className="grid grid-cols-3 gap-3">
        <Stat label="Sent" value={formatBytes(bytes(sent))} detail={count(sent)} />
        <Stat label="Received" value={formatBytes(bytes(got))} detail={count(got)} />
        <Stat label="Active" value={String(running)} detail={running === 1 ? "transfer running" : "transfers running"} />
      </div>
      <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
        <TabsList>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="send">Sent</TabsTrigger>
          <TabsTrigger value="receive">Received</TabsTrigger>
        </TabsList>
      </Tabs>
      {live.length > 0 && (
        <Section title="In progress" aside={live.length}>
          <ul className="flex flex-col gap-2">
            {live.map((t) => (
              <TransferRow key={t.id} t={t} />
            ))}
          </ul>
        </Section>
      )}
      <Section
        title="History"
        aside={
          past.length > 0 && (
            <Button variant="ghost" size="sm" onClick={() => past.forEach((t) => actions.dismiss(t.id))}>
              Clear
            </Button>
          )
        }
      >
        {past.length ? (
          <ul className="flex flex-col gap-2">
            {past.map((t) => (
              <HistoryRow key={t.id} t={t} />
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">Nothing finished yet.</p>
        )}
      </Section>
    </div>
  )
}

const DECLINES: Record<string, string> = {
  "receiver declined": "Declined",
  "device is busy": "Busy",
  "no response": "No response",
  cancelled: "Cancelled",
}

/** A nearby send as its device tile shows it. */
function tile(t: Transfer | undefined): TileStatus | undefined {
  if (!t) return undefined
  switch (t.status) {
    case "listening":
    case "connecting":
      return { tone: "live", label: "Waiting…" }
    case "authenticating":
      return { tone: "live", label: "Connecting…" }
    case "waiting":
      return { tone: "live", label: t.sas ? formatCode(t.sas) : "Waiting…" }
    case "sending":
      return {
        tone: "live",
        label: `${Math.floor((t.sent / (t.total || 1)) * 100)}%`,
        progress: t.total ? t.sent / t.total : 0,
      }
    case "done":
      return { tone: "done", label: "Sent" }
    case "cancelled":
      return { tone: "error", label: "Cancelled" }
  }
  return { tone: "error", label: DECLINES[t.error] ?? (t.error || "Failed") }
}

/** Pick files once, then tap any number of devices: each tap is its own transfer with its own ring. */
export function NearbyPage() {
  const { devices, visible, device, staged, transfers, hidden } = useApp()
  const sends = transfers.filter((t) => t.kind === "send" && t.nearby && !hidden.includes(t.id))
  const latest = (id: string) => sends.find((t) => t.to === id) // newest first
  const total = staged.files.reduce((n, f) => n + f.size, 0)
  return (
    <div className="flex flex-col gap-10">
      <Section
        title="Selected files"
        aside={
          staged.files.length > 0 && (
            <>
              {staged.files.length} · {formatBytes(total)}
              <Button variant="outline" size="sm" onClick={() => actions.stage()}>
                <IconPlus data-icon="inline-start" /> Add files
              </Button>
            </>
          )
        }
      >
        {staged.files.length ? (
          <FileList files={staged.files} onRemove={(f) => actions.unstage(staged.files.findIndex((x) => x === f))} />
        ) : (
          <Dropzone
            compact
            onFiles={() => {}}
            onBrowse={() => actions.stage()}
            hint="or click to browse, then tap the devices below. Send to as many as you like."
          />
        )}
      </Section>
      <NearbyDevices
        devices={devices ?? []}
        visible={visible}
        connected
        self={device}
        status={(d) => tile(latest(d.id))}
        onPick={(d) => {
          const t = latest(d.id)
          if (t && !t.done) actions.cancel(t.id)
          else void actions.sendTo(d as never)
        }}
      />
      {sends.length > 0 && (
        <Section
          title="Transfers"
          aside={
            sends.some((t) => t.done) && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => actions.hide(sends.filter((t) => t.done).map((t) => t.id))}
              >
                Clear finished
              </Button>
            )
          }
        >
          <ul className="flex flex-col gap-2">
            {sends.map((t) => (
              <TransferRow key={t.id} t={t} />
            ))}
          </ul>
        </Section>
      )}
      {(staged.files.length > 0 || sends.length > 0) && (
        <StopButton
          sending={sends.some((t) => t.status === "sending")}
          label={sends.some((t) => !t.done) ? "Stop sharing" : "Clear files"}
          onStop={() => {
            sends.forEach((t) => !t.done && actions.cancel(t.id))
            actions.hide(sends.map((t) => t.id))
            actions.clearStaged()
          }}
        />
      )}
    </div>
  )
}

/** One transfer as a PeerRow; receives are shown in the send-side vocabulary the row knows. */
export function TransferRow({ t }: { t: Transfer }) {
  const live = t.status === "sending" || t.status === "receiving"
  const rate = useRate(t.sent, t.total, live)
  const status = (
    t.status === "receiving"
      ? "sending"
      : t.status === "listening" || t.status === "offered"
        ? "waiting"
        : t.status === "cancelled" || t.status === "declined"
          ? "failed"
          : t.status
  ) as PeerState
  return (
    <PeerRow
      peer={{
        id: t.id,
        device: t.peer || "device",
        transport: (t.transport || null) as never,
        status: (PEER_STATES as readonly string[]).includes(status) ? status : "connecting",
        reason:
          DECLINES[t.error] ??
          (t.error || (t.status === "cancelled" ? "Cancelled" : t.status === "declined" ? "Declined" : undefined)),
        sent: t.sent,
        total: t.total,
        filesDone: t.status === "done" ? (t.files ?? []).length : 0,
        sas: t.sas || undefined,
        speed: rate.speed,
        eta: rate.eta,
      }}
      onCancel={() => actions.cancel(t.id)}
      incoming={t.kind === "receive"}
    />
  )
}

function CodeForm() {
  const [typed, setTyped] = useState("")
  const ok = typed.trim().length > 0
  return (
    <Section title="Have a code?">
      <Card>
        <CardContent>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              if (ok) {
                actions.receiveCode(typed)
                setTyped("")
              }
            }}
          >
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="code">Share code or link</FieldLabel>
                <Input
                  id="code"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  placeholder="4821-amber-otter-kite-moth"
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono"
                  aria-label="Share code or link"
                />
                <FieldDescription>
                  Paste the code or link another device shows. The four words are part of it.
                </FieldDescription>
              </Field>
              <Button type="submit" disabled={!ok} className="self-start">
                Receive
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </Section>
  )
}

// ---------------------------------------------------------------- send (web: routes/index.tsx SendPage)

const PEER_STATES = ["connecting", "authenticating", "waiting", "sending", "done", "failed"] as const
type PeerState = (typeof PEER_STATES)[number]

export function SendPage({ t }: { t: Transfer }) {
  const total = t.total
  const live = t.status === "sending"
  const rate = useRate(t.sent, total, live)
  const stateOk = (PEER_STATES as readonly string[]).includes(t.status)
  // a code share lists a receiver once someone connects; a nearby send has its one receiver from the start
  const hasPeer = stateOk && (t.nearby ? t.status !== "connecting" : t.peer !== "")
  const code = t.code
  const dash = code.indexOf("-")

  if (t.done && (t.status === "failed" || t.status === "cancelled")) {
    return <Stopped t={t} />
  }
  return (
    <div className="flex flex-col gap-10">
      {!t.nearby && (
        <Share
          roomId={dash > 0 ? code.slice(0, dash) : ""}
          secret={dash > 0 ? code.slice(dash + 1) : ""}
          link={t.link}
          receivers={hasPeer ? 1 : 0}
        />
      )}

      {t.nearby && !hasPeer && (
        <div className="flex flex-col items-center gap-4 py-6 text-center" role="status">
          <Orb variant="aurora" listening className="size-44" />
          <div className="flex max-w-sm flex-col gap-1">
            <p className="shimmer font-medium">Waiting for {t.peer} to respond</p>
            <p className="text-sm text-muted-foreground">They get a prompt on their device. Keep this window open.</p>
          </div>
        </div>
      )}

      {hasPeer && (
        <Section title={t.nearby ? "Receiver" : "Receivers"}>
          <ul className="flex flex-col gap-2">
            <PeerRow
              peer={{
                id: t.id,
                device: t.peer,
                transport: (t.transport || null) as never,
                status: t.status as PeerState,
                reason: t.error || undefined,
                sent: t.sent,
                total,
                filesDone: t.status === "done" ? (t.files ?? []).length : 0,
                sas: t.sas || undefined,
                speed: rate.speed,
                eta: rate.eta,
              }}
              onCancel={() => actions.cancel(t.id)}
            />
          </ul>
        </Section>
      )}

      <Section title="Files" aside={`${(t.files ?? []).length} · ${formatBytes(total)}`}>
        <FileList files={files(t)} />
        {hasPeer && !t.done && (
          <Marker className="text-xs">
            <MarkerIcon>
              <IconLock />
            </MarkerIcon>
            <MarkerContent>Files are locked once someone joins. Stop sharing to change them.</MarkerContent>
          </Marker>
        )}
      </Section>

      {t.done ? (
        <Button variant="outline" className="self-start" onClick={() => actions.hide([t.id])}>
          Send more files
        </Button>
      ) : (
        <StopButton sending={live} onStop={() => actions.cancel(t.id)} />
      )}
    </div>
  )
}

function StopButton({
  sending,
  onStop,
  label: text = "Stop sharing",
}: {
  sending: boolean
  onStop: () => void
  label?: string
}) {
  const label = (
    <>
      <IconPlayerStop data-icon="inline-start" /> {text}
    </>
  )
  if (!sending)
    return (
      <Button variant="destructive" className="self-start" onClick={onStop}>
        {label}
      </Button>
    )
  return (
    <AlertDialog>
      <AlertDialogTrigger render={<Button variant="destructive" className="self-start" />}>{label}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Stop sharing?</AlertDialogTitle>
          <AlertDialogDescription>
            A transfer is still running. Stopping cancels it for every receiver and the link stops working.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Keep sending</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onStop}>
            Stop sharing
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// ---------------------------------------------------------------- receive (web: routes/r.$roomId.tsx)

const STEPS = [
  ["connecting", "Connecting to the sender"],
  ["authenticating", "Securing the connection"],
] as const

export function ReceivePage({ t }: { t: Transfer }) {
  const received = t.sent
  const fs = t.files ?? []
  const live = t.status === "receiving"
  const rate = useRate(received, t.total, live)
  const from = t.peer
  const n = `${fs.length} ${fs.length === 1 ? "file" : "files"}`

  if (t.status === "failed" || t.status === "declined" || t.status === "cancelled") {
    const declined = t.status === "declined"
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
            {declined ? "You declined the files" : t.status === "cancelled" ? "Transfer cancelled" : "Transfer failed"}
          </h1>
          <p className="text-pretty text-muted-foreground">
            {declined
              ? "Nothing was downloaded, and the sender has been told."
              : t.status === "cancelled"
                ? "Nothing more was downloaded."
                : t.error || "Something went wrong."}
          </p>
        </div>
        <Button variant="outline" onClick={actions.dismissReceived}>
          Back
        </Button>
      </div>
    )
  }

  if (t.status === "done") {
    const path = t.paths?.[0]
    const one = fs.length === 1 && path
    return (
      <div className="flex flex-col items-start gap-6">
        <ChromeCheck />
        <div className="flex flex-col gap-2">
          <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
            Received {n}
            {from && ` from ${from}`}
          </h1>
          <p className="text-pretty text-muted-foreground">
            {formatBytes(t.total)}, and every file's checksum matched. Saved to <SavedTo />.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {one && (
            <Button onClick={() => actions.openFile(path)}>
              <IconExternalLink data-icon="inline-start" /> Open
            </Button>
          )}
          <Button variant={one ? "outline" : "default"} onClick={() => actions.showFile(path)}>
            <IconFolderOpen data-icon="inline-start" /> Show in folder
          </Button>
          <Button variant="outline" onClick={actions.dismissReceived}>
            Done
          </Button>
        </div>
      </div>
    )
  }

  if (t.status === "connecting" || t.status === "authenticating") {
    const at = Math.max(
      0,
      STEPS.findIndex(([k]) => k === t.status),
    )
    return (
      <div className="flex flex-col items-center gap-8 py-4">
        <Orb variant="ember" energy={0.4 + at * 0.25} className="size-44 sm:size-52" />
        <ol className="flex flex-col gap-3" role="status" aria-live="polite">
          {STEPS.map(([k, label], i) => (
            <Marker key={k} render={<li />} className={cn(i === at && "text-foreground", i > at && "opacity-50")}>
              <MarkerIcon>{i < at ? <IconCheck /> : i === at ? <Pulse className="size-4" /> : <IconCircleDashed />}</MarkerIcon>
              <MarkerContent className={cn(i === at && "shimmer")}>{label}</MarkerContent>
            </Marker>
          ))}
        </ol>
        <Button variant="outline" onClick={() => actions.cancel(t.id)}>
          Cancel
        </Button>
      </div>
    )
  }

  const offered = t.status === "offered"
  const pct = t.total ? Math.min(100, (received / t.total) * 100) : 0
  let acc = 0
  const filesDone = fs.filter((f) => (acc += f.size) <= received).length
  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-2">
        <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
          {offered ? `${from || "Someone"} wants to send you ${n}` : `Receiving ${n} from ${from || "the sender"}`}
        </h1>
        <p className="flex items-center gap-2 text-muted-foreground">
          {formatBytes(t.total)} in total
          {t.transport && <TransportBadge transport={t.transport as "rtc" | "relay" | "lan"} />}
        </p>
      </div>

      {!offered && (
        <Card>
          <CardContent className="flex flex-col gap-4">
            <div className="flex items-end justify-between gap-4">
              <span className="font-pixel text-5xl leading-none tabular-nums sm:text-6xl">{Math.floor(pct)}%</span>
              <span className="text-right font-mono text-xs text-muted-foreground tabular-nums">
                {formatSpeed(rate.speed)}
                <br />
                {formatETA(rate.eta)} left
              </span>
            </div>
            <Progress value={pct} aria-label="Download progress" />
            <div className="flex justify-between font-mono text-xs text-muted-foreground tabular-nums">
              <span>
                {formatBytes(received)} / {formatBytes(t.total)}
              </span>
              <span>
                {filesDone}/{fs.length} files
              </span>
            </div>
          </CardContent>
        </Card>
      )}

      <Section title="Files">
        <FileList
          files={files(t)}
          checks={!offered}
          state={(i) => (offered || i > filesDone ? "idle" : i < filesDone ? "done" : "uploading")}
        />
        <Marker className="text-xs">
          <MarkerIcon>
            <IconInfoCircle />
          </MarkerIcon>
          <MarkerContent>
            Files are saved to <SavedTo /> and never overwrite what is already there.
          </MarkerContent>
        </Marker>
      </Section>

      {offered && t.sas ? (
        <Card>
          <CardContent className="flex flex-col gap-2">
            <span className="font-mono text-xs tracking-wider text-muted-foreground uppercase">
              Check this code with {from || "the sender"}
            </span>
            <p
              className="font-pixel text-5xl leading-none tabular-nums sm:text-6xl"
              aria-label={`Verification code ${formatCode(t.sas).replace(" ", ", ")}`}
            >
              {formatCode(t.sas)}
            </p>
            <p className="text-xs text-muted-foreground">
              It must match the code on their screen. If it doesn't, decline: someone may be intercepting the
              connection.
            </p>
          </CardContent>
          <CardFooter className="flex-wrap gap-2">
            <OfferActions id={t.id} />
          </CardFooter>
        </Card>
      ) : offered ? (
        <div className="flex flex-wrap gap-2">
          <OfferActions id={t.id} />
        </div>
      ) : (
        <Button variant="destructive" className="self-start" onClick={() => actions.cancel(t.id)}>
          Cancel
        </Button>
      )}
    </div>
  )
}

const SavedTo = () => <span className="font-mono">{useApp((s) => s.dir)}</span>

function OfferActions({ id }: { id: string }) {
  return (
    <>
      <Button onClick={() => actions.respond(id, true)}>
        <IconDownload data-icon="inline-start" /> Accept and download
      </Button>
      <Button variant="outline" onClick={() => actions.respond(id, false)}>
        Decline
      </Button>
    </>
  )
}

function Stopped({ t }: { t: Transfer }) {
  return (
    <div className="flex flex-col items-start gap-6">
      <div className="flex size-12 items-center justify-center rounded-2xl bg-destructive/10 text-destructive">
        <IconAlertTriangle className="size-6" />
      </div>
      <div className="flex flex-col gap-2">
        <h1 className="font-serif text-2xl tracking-tight text-balance sm:text-3xl">
          {t.status === "cancelled" ? "Transfer closed" : "Transfer stopped"}
        </h1>
        <p className="text-pretty text-muted-foreground">{t.error || "The share is no longer active."}</p>
      </div>
      <Button onClick={() => actions.hide([t.id])}>Start over</Button>
    </div>
  )
}
