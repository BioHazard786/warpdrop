import { describe, expect, it } from "vitest"
import { deviceName } from "./device"

const android = (tail: string) =>
  `Mozilla/5.0 (Linux; Android 14; K) AppleWebKit/537.36 (KHTML, like Gecko) ${tail}`
const ios = (tail: string) =>
  `Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) ${tail}`
const win = (tail: string) =>
  `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) ${tail}`
const mac =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"

describe("deviceName", () => {
  it.each([
    [android("Chrome/131.0.0.0 Mobile Safari/537.36"), false, false, "Chrome 131 on Android"],
    [
      android("Chrome/131.0.0.0 Mobile Safari/537.36"),
      true,
      false,
      "Brave (Chromium 131) on Android",
    ],
    [
      android("Chrome/131.0.0.0 Mobile Safari/537.36 OPR/86.0.0.0"),
      false,
      false,
      "Opera 86 on Android",
    ],
    [
      android("SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36"),
      false,
      false,
      "Samsung Internet 26 on Android",
    ],
    [
      android("Chrome/131.0.0.0 Mobile Safari/537.36 EdgA/131.0.0.0"),
      false,
      false,
      "Edge 131 on Android",
    ],
    [
      "Mozilla/5.0 (Android 14; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0",
      false,
      false,
      "Firefox 132 on Android",
    ],
    [ios("Version/17.5 Mobile/15E148 Safari/604.1"), false, false, "Safari 17.5 on iOS"],
    [ios("CriOS/131.0.6778.73 Mobile/15E148 Safari/604.1"), false, false, "Chrome 131 on iOS"],
    [ios("FxiOS/132.0 Mobile/15E148 Safari/605.1.15"), false, false, "Firefox 132 on iOS"],
    [win("Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"), false, false, "Edge 131 on Windows"],
    [win("Chrome/131.0.0.0 Safari/537.36 OPR/115.0.0.0"), false, false, "Opera 115 on Windows"],
    [mac, false, false, "Safari 17.5 on macOS"],
    [mac, false, true, "Safari 17.5 on iPadOS"],
    ["curl/8.0", false, false, "Browser on device"],
  ])("%s", (ua, brave, touch, want) => {
    expect(deviceName(ua, brave, touch)).toBe(want)
  })
})
