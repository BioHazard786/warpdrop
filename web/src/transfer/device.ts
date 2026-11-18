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
  closers: Set<() => void>
  /** Kill every live transport, as a network drop would. */
  drop(): void
}
export function registerTransport(close: () => void): () => void {
  if (!e2e) return () => {}
  const w = window as unknown as { __warpdropTest?: TestHooks }
  w.__warpdropTest ??= {
    closers: new Set(),
    drop() {
      for (const c of this.closers) c()
    },
  }
  w.__warpdropTest.closers.add(close)
  return () => w.__warpdropTest!.closers.delete(close)
}
