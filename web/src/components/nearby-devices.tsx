import { IconBrandBrave } from "@/components/brave-icon"
import { Orb } from "@/components/orb"
import { Section } from "@/components/section"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Kbd } from "@/components/ui/kbd"
import { Toggle } from "@/components/ui/toggle"
import { splitDevice } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  IconBrandAndroid,
  IconBrandApple,
  IconBrandChrome,
  IconBrandEdge,
  IconBrandFirefox,
  IconBrandOpera,
  IconBrandSafari,
  IconBrandVivaldi,
  IconBrandWindows,
  IconCheck,
  IconDeviceDesktop,
  IconEye,
  IconEyeOff,
  IconTerminal2,
  IconWifiOff,
  IconX,
} from "@tabler/icons-react"
import type { ComponentType } from "react"

// First match wins: the browser's own logo, else the OS's, else a generic screen.
const ICONS: [RegExp, ComponentType<{ className?: string }>][] = [
  [/\(CLI[^)]*\)$/, IconTerminal2],
  [/^Brave\b/, IconBrandBrave],
  [/^Edge\b/, IconBrandEdge],
  [/^Opera\b/, IconBrandOpera],
  [/^Vivaldi\b/, IconBrandVivaldi],
  [/^Firefox\b/, IconBrandFirefox],
  [/^Safari\b/, IconBrandSafari],
  [/^Chrome\b/, IconBrandChrome],
  [/ on Android$/, IconBrandAndroid],
  [/ on (iOS|iPadOS|macOS)$/, IconBrandApple],
  [/ on Windows$/, IconBrandWindows],
]
const iconFor = (name: string) => ICONS.find(([re]) => re.test(name))?.[1] ?? IconDeviceDesktop

/** What a tile shows while a transfer to that device is under way or just ended. */
export interface TileStatus {
  tone: "live" | "done" | "error"
  /** One short line under the name: "Sending 42%", "Declined". */
  label: string
  /** 0–1 fills the ring; undefined while live spins it. */
  progress?: number
}

/** Devices on the same network, presentational: the web app and the desktop app each feed it from their own state. */
export function NearbyDevices({
  devices,
  visible,
  connected,
  self,
  setVisible,
  status,
  onPick,
}: {
  devices: { id: string; name: string }[]
  visible: boolean
  connected: boolean
  /** This device's own name, shown as "You appear as". */
  self?: string
  setVisible: (v: boolean) => void
  /** The transfer to that device, if any. A live one turns the tile into a cancel button. */
  status?: (device: { id: string; name: string }) => TileStatus | undefined
  /** Tapped: send to it, or cancel it when its status is live. */
  onPick: (device: { id: string; name: string }) => void
}) {
  return (
    <Section
      title="Nearby devices"
      aside={
        <Toggle
          size="sm"
          pressed={visible}
          onPressedChange={setVisible}
          aria-label="Let other devices on this network see this one"
        >
          {visible ? <IconEye data-icon="inline-start" /> : <IconEyeOff data-icon="inline-start" />}
          {visible ? "Visible" : "Hidden"}
        </Toggle>
      }
    >
      {self && (
        <p className="text-sm text-muted-foreground">
          You appear as <span className="font-medium text-foreground">{self}</span>
          {visible ? " to everyone on this network." : ", but only you can see the others."}
        </p>
      )}
      {devices.length === 0 ? (
        <Empty className="gap-4 border-2 border-input border-dashed py-8">
          <EmptyHeader>
            {connected ? (
              <Orb listening energy={0.35} className="mx-auto size-28" />
            ) : (
              <EmptyMedia variant="icon" className="size-11 rounded-xl">
                <IconWifiOff className="size-5" />
              </EmptyMedia>
            )}
            <EmptyTitle className={cn("text-base", connected && "shimmer")} role="status">
              {connected ? "Looking for devices on this network" : "Not connected"}
            </EmptyTitle>
            <EmptyDescription>
              Open WarpDrop on another device on the same Wi-Fi and it shows up here. For a
              different network, use a link.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent className="flex-row flex-wrap justify-center font-mono text-xs text-muted-foreground">
            <Kbd>same Wi-Fi</Kbd>
            <Kbd>no code to share</Kbd>
            <Kbd>they accept first</Kbd>
          </EmptyContent>
        </Empty>
      ) : (
        <ul
          aria-label="Devices on this network"
          className="grid grid-cols-[repeat(auto-fill,minmax(6.5rem,1fr))] gap-2"
        >
          {devices.map((d) => (
            <DeviceTile key={d.id} device={d} status={status?.(d)} onPick={() => onPick(d)} />
          ))}
        </ul>
      )}
    </Section>
  )
}

function DeviceTile({
  device,
  status,
  onPick,
}: {
  device: { id: string; name: string }
  status?: TileStatus
  onPick: () => void
}) {
  const Icon = iconFor(device.name)
  const { title, detail } = splitDevice(device.name)
  const live = status?.tone === "live"
  return (
    <li className="animate-in fade-in zoom-in-95 duration-300">
      <button
        type="button"
        onClick={onPick}
        title={device.name}
        aria-label={live ? `Cancel sending to ${device.name}` : `Send files to ${device.name}`}
        className="group/tile flex w-full flex-col items-center gap-2 rounded-2xl px-1.5 pt-3 pb-2.5 text-center transition-colors outline-none hover:bg-muted/60 focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <span className="relative grid size-18 place-items-center">
          <Ring status={status} />
          <span
            className={cn(
              "grid size-14 place-items-center rounded-full bg-muted text-foreground transition-colors duration-200",
              !live && "group-hover/tile:bg-foreground group-hover/tile:text-background",
              status?.tone === "error" && "bg-destructive/10 text-destructive",
            )}
          >
            <Icon className="size-7 [stroke-width:1.5]" />
          </span>
          {live && (
            <span className="absolute -top-0.5 -right-0.5 grid size-6 place-items-center rounded-full border bg-background opacity-0 shadow-sm transition-opacity group-hover/tile:opacity-100 group-focus-visible/tile:opacity-100">
              <IconX className="size-3.5" />
            </span>
          )}
          {status?.tone === "done" && (
            <span className="absolute -right-0.5 -bottom-0.5 grid size-6 place-items-center rounded-full bg-foreground text-background shadow-sm animate-in zoom-in-50 duration-300">
              <IconCheck className="size-3.5" stroke={3} />
            </span>
          )}
        </span>
        <span className="flex w-full min-w-0 flex-col gap-0.5">
          <span className="truncate text-sm font-medium">{title}</span>
          <span
            key={
              status ? `${status.tone}${status.progress === undefined ? status.label : ""}` : "idle"
            }
            className={cn(
              "truncate font-mono text-xs text-muted-foreground tabular-nums animate-in fade-in duration-300",
              status?.tone === "error" && "text-destructive",
              live && status?.progress === undefined && "shimmer",
            )}
          >
            {status?.label ?? (detail || "Nearby")}
          </span>
        </span>
      </button>
    </li>
  )
}

const R = 34
const C = 2 * Math.PI * R

/** Thin ring around the avatar: fills with progress, spins while waiting, full when done or failed. */
function Ring({ status }: { status?: TileStatus }) {
  if (!status) return null
  const spinning = status.tone === "live" && status.progress === undefined
  const fill = status.tone === "live" ? (status.progress ?? 0.25) : 1
  return (
    <svg
      viewBox="0 0 72 72"
      className={cn(
        "absolute inset-0 -rotate-90",
        spinning && "animate-spin [animation-duration:1.4s]",
      )}
      role={status.tone === "live" && !spinning ? "progressbar" : undefined}
      aria-valuenow={!spinning && status.tone === "live" ? Math.round(fill * 100) : undefined}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-hidden={spinning || status.tone !== "live" || undefined}
    >
      <circle cx="36" cy="36" r={R} fill="none" strokeWidth="3" className="stroke-muted" />
      <circle
        cx="36"
        cy="36"
        r={R}
        fill="none"
        strokeWidth="3"
        strokeLinecap="round"
        strokeDasharray={C}
        strokeDashoffset={C * (1 - fill)}
        className={cn(
          "transition-[stroke-dashoffset] duration-500 ease-out",
          status.tone === "error" ? "stroke-destructive/60" : "stroke-foreground",
          status.tone === "done" && "opacity-40",
        )}
      />
    </svg>
  )
}
