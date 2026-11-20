const UNITS = ["B", "KB", "MB", "GB", "TB", "PB"]

export function formatBytes(bytes: number, digits = 1): string {
  // below 1 byte is nothing: a decaying speed average reaches denormals, whose log gives a negative unit index
  if (!Number.isFinite(bytes) || bytes < 1) return "0 B"
  const i = Math.min(UNITS.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  const v = bytes / 1024 ** i
  return `${i === 0 ? Math.round(v) : v.toFixed(v >= 100 ? 0 : digits)} ${UNITS[i]}`
}

export const formatSpeed = (bps: number) => `${formatBytes(bps)}/s`

export function formatETA(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "--"
  if (seconds < 60) return `${Math.ceil(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}

/** Smoothed transfer speed: exponential moving average over samples taken at the store's flush rate. */
export class SpeedMeter {
  private t = 0
  private bytes = 0
  speed = 0
  sample(bytes: number, now = performance.now()): number {
    if (this.t === 0 || bytes < this.bytes) {
      this.t = now
      this.bytes = bytes
      return this.speed
    }
    const dt = (now - this.t) / 1000
    if (dt < 0.2) return this.speed
    if (bytes === this.bytes) {
      // stalled: keep the window open so the next reading averages over the gap; after 2 s report 0, not a decaying crawl
      if (dt > 2) this.speed = 0
      return this.speed
    }
    const inst = (bytes - this.bytes) / dt
    this.speed = this.speed === 0 ? inst : this.speed * 0.7 + inst * 0.3
    this.t = now
    this.bytes = bytes
    return this.speed
  }
  eta(remaining: number): number {
    return this.speed > 1 ? remaining / this.speed : 0
  }
}
