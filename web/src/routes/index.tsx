import { CliBox } from "@/components/cli-box"
import { Dropzone } from "@/components/dropzone"
import { FileList } from "@/components/file-list"
import { NearbyList } from "@/components/nearby-list"
import { PeerRow } from "@/components/peer-row"
import { Section } from "@/components/section"
import { Share } from "@/components/share-card"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes } from "@/lib/format"
import { useIsSending, useSend } from "@/store/send"
import {
  IconAlertTriangle,
  IconLink,
  IconLock,
  IconPlayerStop,
  IconPlus,
  IconRadar2,
  IconRefresh,
} from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { type ReactNode, useRef, useState } from "react"

export const Route = createFileRoute("/")({ component: SendPage })

const cli = () => ({
  install: {
    cmd: `curl -fsSL ${location.origin}/install.sh | bash`,
    hint: "Installs the warpdrop CLI. It talks to browsers too.",
  },
  send: { cmd: "warpdrop send photos.zip", hint: "Prints a code and a link anyone can open here." },
  nearby: {
    cmd: "warpdrop send --nearby photos.zip",
    hint: "Pick a device on the same network from a list. No code, and it shows up under Nearby.",
  },
  receive: {
    cmd: "warpdrop receive <code>",
    hint: "Takes the code from a browser tab or another terminal.",
  },
})

type Mode = "nearby" | "link"

function SendPage() {
  const s = useSend()
  const [tab, setTab] = useState<Mode>("nearby")
  const live = s.phase === "ready" || s.phase === "reconnecting"
  useCloseConfirmation(live && (s.mode === "link" || s.peers.length > 0))
  const active = s.phase !== "idle"
  return (
    <Tabs value={active ? s.mode : tab} onValueChange={(v) => setTab(v as Mode)} className="gap-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <TabsList className="h-10 w-full sm:w-auto">
          <TabsTrigger value="nearby" disabled={active && s.mode !== "nearby"} className="px-4">
            <IconRadar2 /> Nearby
          </TabsTrigger>
          <TabsTrigger value="link" disabled={active && s.mode !== "link"} className="px-4">
            <IconLink /> Link
          </TabsTrigger>
        </TabsList>
        {active && (
          <span className="font-mono text-xs text-muted-foreground">Stop sharing to switch</span>
        )}
      </div>
      <TabsContent value="nearby" className="animate-in fade-in duration-300">
        <NearbyTab />
      </TabsContent>
      <TabsContent value="link" className="animate-in fade-in duration-300">
        <LinkTab />
      </TabsContent>
    </Tabs>
  )
}

function Stopped() {
  const s = useSend()
  return (
    <Empty className="flex-none border border-dashed py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <IconAlertTriangle />
        </EmptyMedia>
        <EmptyTitle>{s.phase === "error" ? "Transfer stopped" : "Transfer closed"}</EmptyTitle>
        <EmptyDescription>
          {s.error ?? (s.mode === "link" ? "The share link no longer works." : "The share ended.")}
        </EmptyDescription>
      </EmptyHeader>
      <Button onClick={s.reset}>
        <IconRefresh data-icon="inline-start" /> Start over
      </Button>
    </Empty>
  )
}

function Reconnecting() {
  return (
    <Alert>
      <Spinner />
      <AlertTitle>Reconnecting to the server</AlertTitle>
      <AlertDescription>
        The connection dropped. Transfers already running carry on.
      </AlertDescription>
    </Alert>
  )
}

function FilesSection({ title, children }: { title: string; children?: ReactNode }) {
  const s = useSend()
  const total = s.files.reduce((n, f) => n + f.size, 0)
  const locked = s.peers.length > 0
  return (
    <Section
      title={title}
      aside={
        <>
          {s.files.length} · {formatBytes(total)}
          {!locked && <AddFiles onFiles={s.addFiles} />}
        </>
      }
    >
      <FileList files={s.files} onRemove={locked ? undefined : s.removeFile} />
      {locked && (
        <Marker className="text-xs">
          <MarkerIcon>
            <IconLock />
          </MarkerIcon>
          <MarkerContent>
            Files are locked once someone joins. Stop sharing to change them.
          </MarkerContent>
        </Marker>
      )}
      {children}
    </Section>
  )
}

function Receivers({ title }: { title: string }) {
  const s = useSend()
  if (s.peers.length === 0) return null
  return (
    <Section title={title} aside={s.mode === "link" ? `${s.peers.length} of 10` : undefined}>
      <ul className="flex flex-col gap-2">
        {s.peers.map((p) => (
          <PeerRow key={p.id} peer={p} onCancel={() => s.cancelPeer(p.id)} />
        ))}
      </ul>
    </Section>
  )
}

/** Pick files once, then tap any number of devices: each gets its own prompt and progress ring. */
function NearbyTab() {
  const s = useSend()
  const room = s.mode === "nearby" && s.phase !== "idle"
  if (room && (s.phase === "error" || s.phase === "closed")) return <Stopped />
  return (
    <div className="flex flex-col gap-10">
      {room && s.phase === "reconnecting" && <Reconnecting />}
      {room ? (
        <FilesSection title="Selected files" />
      ) : (
        <Section title="Selected files">
          <Dropzone
            compact
            onFiles={(f) => s.start(f, true)}
            hint="or click to browse, then tap the devices below. Send to as many as you like."
          />
        </Section>
      )}
      <NearbyList />
      {room && <Receivers title="Transfers" />}
      {room && (
        <StopButton
          onStop={s.reset}
          label={s.peers.length || Object.keys(s.invites).length ? "Stop sharing" : "Clear files"}
        />
      )}
    </div>
  )
}

function LinkTab() {
  const s = useSend()
  if (s.phase === "idle") return <LinkIdle onFiles={(f) => s.start(f)} />
  if (s.phase === "error" || s.phase === "closed") return <Stopped />
  return (
    <div className="flex flex-col gap-10">
      {s.phase === "reconnecting" && <Reconnecting />}
      <Share
        roomId={s.roomId}
        secret={s.secret}
        link={s.roomId && s.secret ? `${location.origin}/r/${s.roomId}#${s.secret}` : ""}
        receivers={s.peers.length}
      />
      <Receivers title="Receivers" />
      <FilesSection title="Files" />
      <StopButton onStop={s.reset} label="Stop sharing" />
    </div>
  )
}

function LinkIdle({ onFiles }: { onFiles: (files: File[]) => void }) {
  const CLI = cli()
  const [tab, setTab] = useState<keyof typeof CLI>("install")
  return (
    <div className="flex flex-col gap-10">
      <Dropzone onFiles={onFiles} />
      <Section
        title="Terminal"
        aside={
          <ToggleGroup
            variant="outline"
            size="sm"
            spacing={0}
            value={[tab]}
            onValueChange={(v) => v[0] && setTab(v[0] as keyof typeof CLI)}
            aria-label="Command"
          >
            {Object.keys(CLI).map((k) => (
              <ToggleGroupItem key={k} value={k} className="font-mono text-xs">
                {k}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      >
        <CliBox command={CLI[tab].cmd} />
        <p className="text-xs text-muted-foreground">{CLI[tab].hint}</p>
      </Section>
    </div>
  )
}

function AddFiles({ onFiles }: { onFiles: (files: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <input
        ref={input}
        type="file"
        multiple
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        onChange={(e) => {
          const files = [...(e.target.files ?? [])]
          e.target.value = "" // allow picking the same file again later
          if (files.length) onFiles(files)
        }}
      />
      <Button variant="outline" size="sm" onClick={() => input.current?.click()}>
        <IconPlus data-icon="inline-start" /> Add files
      </Button>
    </>
  )
}

function StopButton({ onStop, label: text }: { onStop: () => void; label: string }) {
  const sending = useIsSending()
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
      <AlertDialogTrigger render={<Button variant="destructive" className="self-start" />}>
        {label}
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Stop sharing?</AlertDialogTitle>
          <AlertDialogDescription>
            A transfer is still running. Stopping cancels it for every receiver and the link stops
            working.
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
