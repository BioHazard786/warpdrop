// First match wins: Chromium forks and iOS wrappers also carry "Chrome/" or "Safari/", so they come first.
// Group 1 is the version, when the UA has one (Chromium reports only the major: "Chrome/131.0.0.0").
const BROWSERS: [RegExp, string][] = [
  [/(?:EdgA?|EdgiOS)\/(\d+(?:\.\d+)?)/, "Edge"],
  [/(?:OPR|OPT)\/(\d+(?:\.\d+)?)|Opera/, "Opera"],
  [/SamsungBrowser\/(\d+(?:\.\d+)?)/, "Samsung Internet"],
  [/YaBrowser\/(\d+(?:\.\d+)?)/, "Yandex"],
  [/Vivaldi\/(\d+(?:\.\d+)?)/, "Vivaldi"],
  [/(?:DuckDuckGo|Ddg)\/(\d+(?:\.\d+)?)/, "DuckDuckGo"],
  [/UCBrowser\/(\d+(?:\.\d+)?)/, "UC Browser"],
  [/MiuiBrowser\/(\d+(?:\.\d+)?)/, "Mi Browser"],
  [/(?:Firefox|FxiOS)\/(\d+(?:\.\d+)?)/, "Firefox"],
  [/(?:CriOS|Chrome)\/(\d+(?:\.\d+)?)/, "Chrome"],
  [/Version\/(\d+(?:\.\d+)?).*Safari\/|Safari\//, "Safari"],
]

const OSES: [RegExp, string][] = [
  [/Android/, "Android"],
  [/iPhone|iPod/, "iOS"],
  [/iPad/, "iPadOS"],
  [/Windows/, "Windows"],
  [/CrOS/, "ChromeOS"],
  [/Mac OS/, "macOS"],
  [/Linux/, "Linux"],
]

/** Short human label for the other side's UI, e.g. "Chrome 157 on Android". Best effort, never throws. */
export function deviceName(
  ua = navigator.userAgent,
  brave = "brave" in navigator, // Brave's UA is identical to Chrome's; it only exposes navigator.brave
  touch = navigator.maxTouchPoints > 1,
): string {
  try {
    let browser = "Browser"
    for (const [re, name] of BROWSERS) {
      const m = re.exec(ua)
      if (!m) continue
      const v = m[1]?.replace(/\.0$/, "") // "131.0" → "131", "17.5" stays
      // Brave's own version is not in its UA; the number there is its Chromium base
      browser =
        brave && name === "Chrome"
          ? `Brave${v ? ` (Chromium ${v})` : ""}`
          : v
            ? `${name} ${v}`
            : name
      break
    }
    let os = OSES.find(([re]) => re.test(ua))?.[1] ?? "device"
    if (os === "macOS" && touch) os = "iPadOS" // iPadOS 13+ claims to be a Mac
    return `${browser} on ${os}`
  } catch {
    return "Browser"
  }
}

/** Test-only switches, compiled in only for E2E builds (VITE_E2E=1). */
export const e2e = import.meta.env.VITE_E2E === "1"
export const forceRelay = () =>
  e2e && new URLSearchParams(location.search).get("transport") === "relay"

interface TestHooks {
  transports: Set<() => void>
  signaling: Set<() => void>
  /** How many live transports drop() has closed so far (lets a test prove its fault really fired). */
  dropped: number
  /** Kill every live transport, as a network drop would. */
  drop(): void
  /** Kill the signaling socket(s) only; the client must reconnect and resume. */
  dropSignaling(): void
}

function hooks(): TestHooks {
  const w = window as unknown as { __warpdropTest?: TestHooks }
  return (w.__warpdropTest ??= {
    transports: new Set(),
    signaling: new Set(),
    dropped: 0,
    drop() {
      for (const c of this.transports) {
        this.dropped++
        c()
      }
    },
    dropSignaling() {
      for (const c of this.signaling) c()
    },
  })
}

export function registerTransport(close: () => void): () => void {
  if (!e2e) return () => {}
  const set = hooks().transports
  set.add(close)
  return () => set.delete(close)
}

export function registerSignaling(close: () => void): () => void {
  if (!e2e) return () => {}
  const set = hooks().signaling
  set.add(close)
  return () => set.delete(close)
}
