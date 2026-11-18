import { CliBox } from "@/components/cli-box"
import { Dropzone } from "@/components/dropzone"
import { FileList } from "@/components/file-list"
import { PeerRow } from "@/components/peer-row"
import { QR } from "@/components/qr"
import { Section } from "@/components/section"
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
import { Card, CardContent, CardFooter } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Spinner } from "@/components/ui/spinner"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useCloseConfirmation } from "@/hooks/use-close-confirmation"
import { formatBytes } from "@/lib/format"
import { useIsSending, useSend } from "@/store/send"
import {
  IconAlertTriangle,
  IconDevices,
  IconLock,
  IconPlus,
  IconPlayerStop,
  IconRefresh,
} from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useRef, useState } from "react"

export const Route = createFileRoute("/")({ component: SendPage })

const cli = () => ({
  install: {
    cmd: `curl -fsSL ${location.origin}/install.sh | bash`,
    hint: "Installs the warpdrop CLI. It talks to browsers too.",
  },
  send: { cmd: "warpdrop send photos.zip", hint: "Prints a code and a link anyone can open here." },
  receive: {
    cmd: "warpdrop receive <code>",
    hint: "Takes the code from a browser tab or another terminal.",
  },
})

function SendPage() {
  const s = useSend()
  useCloseConfirmation(s.phase === "ready" || s.phase === "reconnecting")

  if (s.phase === "idle") return <Idle onFiles={s.start} />

  if (s.phase === "error" || s.phase === "closed") {
    return (
      <Empty className="flex-none border border-dashed py-12">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <IconAlertTriangle />
          </EmptyMedia>
          <EmptyTitle>{s.phase === "error" ? "Transfer stopped" : "Transfer closed"}</EmptyTitle>
          <EmptyDescription>{s.error ?? "The share link no longer works."}</EmptyDescription>
        </EmptyHeader>
        <Button onClick={s.reset}>
          <IconRefresh data-icon="inline-start" /> Start over
        </Button>
      </Empty>
    )
  }

  const total = s.files.reduce((n, f) => n + f.size, 0)
  const locked = s.peers.length > 0
  return (
    <div className="flex flex-col gap-10">
      {s.phase === "reconnecting" && (
        <Alert>
          <Spinner />
          <AlertTitle>Reconnecting to the server</AlertTitle>
          <AlertDescription>The connection dropped. Your link stays valid.</AlertDescription>
        </Alert>
      )}

      <Share roomId={s.roomId} secret={s.secret} />

      <Section title="Receivers" aside={`${s.peers.length}/10`}>
        {s.peers.length === 0 ? (
          <Empty className="border-2 border-dashed py-10">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <IconDevices />
              </EmptyMedia>
              <EmptyTitle className="shimmer">Waiting for someone to open the link</EmptyTitle>
              <EmptyDescription>
                Up to 10 people can receive at once. Keep this tab open.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="flex flex-col gap-2">
            {s.peers.map((p) => (
              <PeerRow key={p.id} peer={p} onCancel={() => s.cancelPeer(p.id)} />
            ))}
          </ul>
        )}
      </Section>

      <Section
        title="Files"
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
      </Section>

      <StopButton onStop={s.reset} />
    </div>
  )
}

function Idle({ onFiles }: { onFiles: (files: File[]) => void }) {
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

function Share({ roomId, secret }: { roomId: string; secret: string }) {
  const code = roomId && secret ? `${roomId}-${secret}` : ""
  const link = code ? `${location.origin}/r/${roomId}#${secret}` : ""
  return (
    <Section title="Share" aside={code ? "live while this tab is open" : undefined}>
      <Card>
        {!code ? (
          <CardContent>
            <Marker role="status" className="py-10 justify-center">
              <MarkerIcon>
                <Spinner />
              </MarkerIcon>
              <MarkerContent className="shimmer">Creating your link…</MarkerContent>
            </Marker>
          </CardContent>
        ) : (
          <CardContent className="flex flex-col gap-6 sm:flex-row sm:items-center">
            <QR value={link} className="self-center sm:self-auto" />
            <div className="flex min-w-0 flex-1 flex-col gap-4">
              <div className="flex flex-col gap-1">
                <span className="font-mono text-xs tracking-wider text-muted-foreground uppercase">
                  Code
                </span>
                <p
                  className="font-mono text-xl font-medium tracking-tight wrap-break-word select-all sm:text-2xl"
                  aria-label="Share code"
                >
                  <span>{roomId}</span>
                  <span className="text-muted-foreground">-{secret}</span>
                </p>
              </div>
              <CliBox command={link} prompt={false} />
              <CliBox command={`warpdrop receive ${code}`} />
            </div>
          </CardContent>
        )}
        <CardFooter>
          <Marker className="text-xs">
            <MarkerIcon>
              <IconLock />
            </MarkerIcon>
            <MarkerContent>
              The words after the number never reach our server. Without them the link is useless,
              even to someone who guesses the number.
            </MarkerContent>
          </Marker>
        </CardFooter>
      </Card>
    </Section>
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
      <Button variant="outline" size="xs" onClick={() => input.current?.click()}>
        <IconPlus data-icon="inline-start" /> Add files
      </Button>
    </>
  )
}

function StopButton({ onStop }: { onStop: () => void }) {
  const sending = useIsSending()
  const label = (
    <>
      <IconPlayerStop data-icon="inline-start" /> Stop sharing
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
      <AlertDialogTrigger render={<Button variant="outline" className="self-start" />}>
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
