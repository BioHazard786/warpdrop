/** Short human label for the other side's UI, e.g. "Chrome on Android". Best effort, never throws. */
export function deviceName(): string {
  try {
    const ua = navigator.userAgent
    const browser = /Edg\//.test(ua)
      ? "Edge"
      : /Firefox\//.test(ua)
        ? "Firefox"
        : /Chrome\//.test(ua)
          ? "Chrome"
          : /Safari\//.test(ua)
            ? "Safari"
            : "Browser"
    const os = /Android/.test(ua)
      ? "Android"
      : /iPhone|iPad/.test(ua)
        ? "iOS"
        : /Windows/.test(ua)
          ? "Windows"
          : /Mac OS/.test(ua)
            ? "macOS"
            : /Linux/.test(ua)
              ? "Linux"
              : "device"
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
