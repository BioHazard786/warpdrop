import { describe, expect, it } from "vitest"
import { SenderRoom } from "./sender"

const f = (name: string, bytes: number) => new File([new Uint8Array(bytes)], name)

describe("SenderRoom.setFiles", () => {
  it("swaps the list and recomputes the total", () => {
    const room = new SenderRoom([f("a", 10)], () => {})
    expect(room.total).toBe(10)
    expect(room.setFiles([f("a", 10), f("b", 5)])).toBeNull()
    expect(room.total).toBe(15)
    expect(room.files().map((s) => s.id)).toEqual([0, 1])
    expect(room.offsetOf(1)).toBe(10)
  })

  it("refuses an empty list and keeps the old one", () => {
    const room = new SenderRoom([f("a", 10)], () => {})
    expect(room.setFiles([])).toMatch(/at least one/)
    expect(room.total).toBe(10)
  })

  it("refuses changes once a receiver has joined", () => {
    const room = new SenderRoom([f("a", 10)], () => {})
    ;(room as unknown as { links: Map<string, unknown> }).links.set("peer", {})
    expect(room.setFiles([f("b", 1)])).toMatch(/once someone has joined/)
    expect(room.total).toBe(10)
  })
})
