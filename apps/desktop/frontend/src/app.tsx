import {
  IconActivity,
  IconBrightness,
  IconDownload,
  IconEye,
  IconEyeOff,
  IconFolderOpen,
  IconLink,
  IconRadar2,
  IconSettings,
} from "@tabler/icons-react"
import { type ReactNode, useEffect } from "react"
import { useTheme } from "@/components/theme-provider"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { Toaster } from "@/components/ui/toast"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Ring } from "~/components/loading-ui/ring"
import { Ripple } from "~/components/loading-ui/ripple"
import { ActivityPage, LinkPage, NearbyPage, ReceivePane } from "~/pages"
import { SettingsPage } from "~/settings"
import { actions, type Tab, useApp } from "~/store"

const TITLES: Record<Tab, string> = {
  nearby: "Nearby",
  link: "Send with a link",
  receive: "Receive",
  activity: "Activity",
  settings: "Settings",
}
const PAGES: Tab[] = ["nearby", "link", "receive", "activity"]
const MOD = /Mac/.test(navigator.platform) ? "⌘" : "Ctrl+"

const typing = (t: EventTarget | null) =>
  t instanceof HTMLElement && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))
// a full share code or link with its secret words (the CLI's codeRe and linkRe); the engine has the final say
const looksLikeCode = (s: string) => /^\d{4,6}-[a-z-]+$/.test(s) || /\/r\/\d{4,6}#[a-z-]+$/.test(s)

/** Ctrl/⌘ 1–4 and , switch pages, O picks files, and pasting a code anywhere outside a field receives it. */
function useShortcuts() {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return
      const page = PAGES[Number(e.key) - 1]
      if (page) actions.setTab(page)
      else if (e.key === ",") actions.setTab("settings")
      else if (e.key.toLowerCase() === "o") {
        const { tab, transfers } = useApp.getState()
        const sharing = transfers.some((t) => t.kind === "send" && !t.nearby && !t.done)
        if (tab === "link" && !sharing) void actions.sendWithCode()
        else {
          actions.setTab("nearby")
          void actions.stage()
        }
      } else return
      e.preventDefault()
    }
    const onPaste = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData("text").trim() ?? ""
      if (typing(e.target) || !looksLikeCode(text)) return
      e.preventDefault()
      void actions.receiveCode(text)
    }
    window.addEventListener("keydown", onKey)
    window.addEventListener("paste", onPaste)
    return () => {
      window.removeEventListener("keydown", onKey)
      window.removeEventListener("paste", onPaste)
    }
  }, [])
}

export function App() {
  const tab = useApp((s) => s.tab)
  const all = useApp((s) => s.transfers)
  const hidden = useApp((s) => s.hidden)
  const { device, visible, devices, server } = useApp()
  const { theme, setTheme } = useTheme()
  useShortcuts()
  // finished ones cleared from these pages (or from an earlier run) only show on Activity
  const transfers = all.filter((t) => !hidden.includes(t.id))

  // a code share (never a nearby send): the live one first, else the latest result stays up until dismissed
  const shares = transfers.filter((t) => t.kind === "send" && !t.nearby)
  const share = shares.find((t) => !t.done) ?? shares[0]
  // a decision waiting on you first, then live ones, then the latest result; the rest are listed under it
  const receives = transfers.filter((t) => t.kind === "receive")
  const focus = receives.find((t) => t.status === "offered" && !t.done) ?? receives.find((t) => !t.done) ?? receives[0]
  const others = receives.filter((x) => x !== focus && !x.done)
  const offers = receives.filter((t) => t.status === "offered" && !t.done).length
  const running = all.filter((t) => !t.done)
  const moving = running.filter((t) => t.total > 0 && (t.status === "sending" || t.status === "receiving"))
  const pct = moving.length
    ? Math.floor((moving.reduce((n, t) => n + t.sent, 0) / moving.reduce((n, t) => n + t.total, 0)) * 100)
    : null
  const scanning = server === "" && (devices ?? []).length === 0

  const item = (value: Tab, label: string, icon: ReactNode, badge?: ReactNode) => (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={tab === value}
        tooltip={`${label} (${MOD}${value === "settings" ? "," : PAGES.indexOf(value) + 1})`}
        onClick={() => actions.setTab(value)}
      >
        {icon}
        <span>{label}</span>
      </SidebarMenuButton>
      {badge != null && <SidebarMenuBadge>{badge}</SidebarMenuBadge>}
    </SidebarMenuItem>
  )

  return (
    <Toaster>
      <TooltipProvider>
        <SidebarProvider className="h-svh">
          <Sidebar collapsible="icon">
            <SidebarHeader>
              <div className="flex items-center gap-2 px-2 py-1.5">
                <img src="/favicon.svg" alt="" className="size-6 shrink-0" />
                <span className="font-serif text-lg tracking-tight group-data-[collapsible=icon]:hidden">WarpDrop</span>
              </div>
            </SidebarHeader>
            <SidebarContent>
              <SidebarGroup>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {item(
                      "nearby",
                      "Nearby",
                      scanning ? <Ripple className="size-4" /> : <IconRadar2 />,
                      (devices ?? []).length || undefined,
                    )}
                    {item("link", "Link", <IconLink />)}
                    {item("receive", "Receive", <IconDownload />, offers || undefined)}
                    {item(
                      "activity",
                      "Activity",
                      running.length ? <Ring className="size-4" /> : <IconActivity />,
                      pct !== null ? `${pct}%` : running.length || undefined,
                    )}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            </SidebarContent>
            <SidebarFooter>
              <SidebarMenu>
                <SidebarMenuItem>
                  <SidebarMenuButton
                    tooltip={visible ? "Visible to nearby devices" : "Hidden from nearby devices"}
                    onClick={() => actions.setVisible(!visible)}
                  >
                    {visible ? <IconEye /> : <IconEyeOff />}
                    <span className="flex min-w-0 flex-col leading-tight">
                      <span className="truncate font-medium">{device || "This device"}</span>
                      <span className="text-xs text-muted-foreground">{visible ? "Visible" : "Hidden"}</span>
                    </span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                <SidebarMenuItem>
                  <SidebarMenuButton tooltip="Open downloads folder" onClick={actions.openDownloads}>
                    <IconFolderOpen />
                    <span>Downloads</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
                {item("settings", "Settings", <IconSettings />)}
                <SidebarMenuItem>
                  <SidebarMenuButton
                    tooltip="Switch theme (D)"
                    onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
                  >
                    <IconBrightness />
                    <span>{theme === "dark" ? "Light" : "Dark"} theme</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              </SidebarMenu>
            </SidebarFooter>
          </Sidebar>
          <SidebarInset className="min-w-0">
            <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
              <SidebarTrigger />
              <h1 className="font-medium">{TITLES[tab]}</h1>
            </header>
            <div className="flex-1 overflow-y-auto">
              <div key={tab} className="mx-auto w-full max-w-3xl animate-in px-6 py-8 duration-200 fade-in slide-in-from-bottom-1">
                {tab === "nearby" && <NearbyPage />}
                {tab === "link" && <LinkPage share={share} />}
                {tab === "receive" && <ReceivePane t={focus} others={others} />}
                {tab === "activity" && <ActivityPage />}
                {tab === "settings" && <SettingsPage />}
              </div>
            </div>
          </SidebarInset>
        </SidebarProvider>
      </TooltipProvider>
    </Toaster>
  )
}
