import { describe, expect, it } from "vitest"
import { tileStatus } from "@/lib/nearby-status"
import type { PeerRow, SentInvite } from "@/store/send"

const inv = (state: SentInvite["state"], stale: string[] = []): SentInvite => ({
  device: { id: "d1", name: "Pixel on Android" },
  stale,
  state,
})
const peer = (id: string, status: PeerRow["status"], extra: Partial<PeerRow> = {}): PeerRow => ({
  id,
  device: "Pixel on Android",
  invited: "d1",
  transport: "rtc",
  status,
  sent: 0,
  total: 100,
  filesDone: 0,
  speed: 0,
  eta: 0,
  ...extra,
})

describe("tileStatus", () => {
  it("is empty for a device never invited", () => {
    expect(tileStatus(undefined, [])).toBeUndefined()
  })
  it("waits on a pending invite, then follows the peer that joined", () => {
    expect(tileStatus(inv("pending"), [])).toEqual({
      tone: "live",
      label: "Waiting…",
    })
    expect(tileStatus(inv("pending"), [peer("p1", "sending", { sent: 42 })])).toMatchObject({
      tone: "live",
      label: "42%",
      progress: 0.42,
    })
    expect(tileStatus(inv("pending"), [peer("p1", "done")])).toMatchObject({
      tone: "done",
    })
  })
  it("ignores other devices with the same name", () => {
    expect(tileStatus(inv("pending"), [peer("p9", "done", { invited: "d2" })])).toMatchObject({
      tone: "live",
    })
  })
  it("ignores peers from an earlier send to the same device", () => {
    expect(tileStatus(inv("pending", ["p1"]), [peer("p1", "done")])).toMatchObject({ tone: "live" })
  })
  it("reports declines, busy and silence", () => {
    expect(tileStatus(inv("declined"), [])).toEqual({
      tone: "error",
      label: "Declined",
    })
    expect(tileStatus(inv("busy"), [])).toEqual({
      tone: "error",
      label: "Busy",
    })
    expect(tileStatus(inv("timeout"), [])).toEqual({
      tone: "error",
      label: "No response",
    })
  })
  it("a joined peer wins over a late timeout", () => {
    expect(tileStatus(inv("timeout"), [peer("p2", "waiting", { sas: "123456" })])).toMatchObject({
      tone: "live",
    })
  })
})
