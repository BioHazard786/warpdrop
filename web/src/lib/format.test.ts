import { describe, expect, it } from "vitest"
import { SpeedMeter, formatBytes, formatETA, splitDevice } from "./format"

describe("format", () => {
  it("formats bytes without overflowing the unit table or choking on bad input", () => {
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(-5)).toBe("0 B")
    expect(formatBytes(Number.NaN)).toBe("0 B")
    expect(formatBytes(1023)).toBe("1023 B")
    expect(formatBytes(4.152156325334297)).toBe("4 B")
    expect(formatBytes(1536)).toBe("1.5 KB")
    expect(formatBytes(5 * 1024 ** 3)).toBe("5.0 GB")
    expect(formatBytes(1e30)).toMatch(/PB$/)
    // regression: a stalled transfer's smoothed speed decays to denormals and used to render "Infinity undefined/s"
    for (const tiny of [5e-324, 1e-300, 0.4, 0.999]) expect(formatBytes(tiny)).toBe("0 B")
  })
  it("formats ETA", () => {
    expect(formatETA(0)).toBe("--")
    expect(formatETA(Number.POSITIVE_INFINITY)).toBe("--")
    expect(formatETA(59.2)).toBe("60s")
    expect(formatETA(125)).toBe("2m 5s")
    expect(formatETA(7380)).toBe("2h 3m")
  })
  it("meters speed and resets when bytes go backwards (new transport)", () => {
    const m = new SpeedMeter()
    m.sample(0, 1000)
    expect(m.sample(1_000_000, 2000)).toBeCloseTo(1_000_000)
    expect(m.eta(2_000_000)).toBeCloseTo(2)
    m.sample(10, 3000) // went backwards: restart the window, keep last speed
    expect(m.speed).toBeCloseTo(1_000_000)
  })
  it("drops to 0 on a stall instead of decaying toward absurd ETAs", () => {
    const m = new SpeedMeter()
    m.sample(0, 1000)
    m.sample(1_000_000, 2000)
    expect(m.sample(1_000_000, 2500)).toBeCloseTo(1_000_000) // brief gap: hold
    expect(m.sample(1_000_000, 4500)).toBe(0)
    expect(m.eta(1_000_000)).toBe(0) // rendered as "--"
  })

  it("splits device labels into a title and a detail line", () => {
    expect(splitDevice("Chrome 156 on Linux")).toEqual({ title: "Chrome 156", detail: "Linux" })
    expect(splitDevice("Brave (Chromium 131) on macOS")).toEqual({
      title: "Brave",
      detail: "macOS",
    })
    expect(splitDevice("devbox (CLI v1.2.0)")).toEqual({ title: "devbox", detail: "CLI v1.2.0" })
    expect(splitDevice("Browser")).toEqual({ title: "Browser", detail: "" })
    // names the user typed stay whole, even when they contain " on "
    expect(splitDevice("Fedora on Linux stuff")).toEqual({
      title: "Fedora on Linux stuff",
      detail: "",
    })
    expect(splitDevice("Fedora oN Linux")).toEqual({ title: "Fedora oN Linux", detail: "" })
  })
})
