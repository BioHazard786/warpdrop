import { describe, expect, it } from "vitest"
import { SpeedMeter, formatBytes, formatETA } from "./format"

describe("format", () => {
  it("formats bytes without overflowing the unit table or choking on bad input", () => {
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(-5)).toBe("0 B")
    expect(formatBytes(Number.NaN)).toBe("0 B")
    expect(formatBytes(1023)).toBe("1023 B")
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
})
