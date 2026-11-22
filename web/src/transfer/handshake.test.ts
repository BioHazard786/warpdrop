import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import { xxhash64 } from "hash-wasm"
import {
  FrameCipher,
  commit,
  sas,
  concat,
  deriveKeys,
  ephemeralFromSeed,
  fromHex,
  mac,
  newEphemeral,
  toHex,
  transcript,
  verifyMac,
} from "./handshake"

const v = JSON.parse(
  readFileSync(new URL("../../../protocol/fixtures/crypto-vectors.json", import.meta.url), "utf8"),
)

describe("crypto vectors (shared with the Go CLI)", () => {
  it("derives the same X25519 keys and shared secret", async () => {
    const s = await ephemeralFromSeed(fromHex(v.x25519.senderSeed))
    const r = await ephemeralFromSeed(fromHex(v.x25519.receiverSeed))
    expect(toHex(s.pub)).toBe(v.x25519.senderPub)
    expect(toHex(r.pub)).toBe(v.x25519.receiverPub)
    const ks = await deriveKeys(v.secret, v.roomId, s, r.pub)
    const kr = await deriveKeys(v.secret, v.roomId, r, s.pub)
    for (const k of [ks, kr]) {
      expect(toHex(k.rootKey)).toBe(v.rootKey)
      expect(toHex(k.authKey)).toBe(v.authKey)
      expect(toHex(k.sessionKey)).toBe(v.sessionKey)
    }
  })

  it("derives the nearby (empty-secret) keys, commitment and code", async () => {
    const n = v.nearby
    const s = await ephemeralFromSeed(fromHex(v.x25519.senderSeed))
    const r = await ephemeralFromSeed(fromHex(v.x25519.receiverSeed))
    const ks = await deriveKeys("", n.roomId, s, r.pub)
    const kr = await deriveKeys("", n.roomId, r, s.pub)
    for (const k of [ks, kr]) {
      expect(toHex(k.rootKey)).toBe(n.rootKey)
      expect(toHex(k.authKey)).toBe(n.authKey)
      expect(toHex(k.sessionKey)).toBe(n.sessionKey)
    }
    expect(await commit(s.pub)).toBe(n.commit)
    expect(await sas(ks.authKey, fromHex(n.transcript))).toBe(n.sas)
  })

  it("builds transcripts and MACs", async () => {
    const sPub = fromHex(v.x25519.senderPub),
      rPub = fromHex(v.x25519.receiverPub)
    const rtc = transcript(sPub, rPub, fromHex(v.senderFingerprint), fromHex(v.receiverFingerprint))
    const relay = transcript(sPub, rPub)
    expect(toHex(rtc)).toBe(v.transcriptRtc)
    expect(toHex(relay)).toBe(v.transcriptRelay)
    const k = fromHex(v.authKey)
    expect(await mac(k, "recv", rtc)).toBe(v.macs.rtc.recv)
    expect(await mac(k, "send", rtc)).toBe(v.macs.rtc.send)
    expect(await mac(k, "recv", relay)).toBe(v.macs.relay.recv)
    expect(await verifyMac(k, "recv", rtc, v.macs.rtc.recv)).toBe(true)
    expect(await verifyMac(k, "send", rtc, v.macs.rtc.recv)).toBe(false) // label is bound
    expect(await verifyMac(k, "recv", relay, v.macs.rtc.recv)).toBe(false) // transcript is bound
    expect(await verifyMac(k, "recv", rtc, "zz")).toBe(false)
  })

  it("seals and opens relay frames byte-for-byte", async () => {
    const key = fromHex(v.sessionKey)
    const ciphers = { 1: await FrameCipher.create(key, 1), 2: await FrameCipher.create(key, 2) }
    const openers = { 1: await FrameCipher.create(key, 1), 2: await FrameCipher.create(key, 2) }
    for (const f of v.relayFrames) {
      const dir = f.dir as 1 | 2
      const wire = await ciphers[dir].seal(f.kind, fromHex(f.payload))
      expect(toHex(wire)).toBe(f.frame)
      const o = await openers[dir].open(wire)
      expect(o.kind).toBe(f.kind)
      expect(toHex(o.payload)).toBe(f.payload)
    }
  })

  it("rejects tampered, replayed and reordered relay frames", async () => {
    const key = fromHex(v.sessionKey)
    const tx = await FrameCipher.create(key, 1)
    const a = await tx.seal(0, new Uint8Array([1])),
      b = await tx.seal(0, new Uint8Array([2]))
    const bad = a.slice()
    bad[0]! ^= 1
    await expect((await FrameCipher.create(key, 1)).open(bad)).rejects.toThrow()
    await expect((await FrameCipher.create(key, 1)).open(b)).rejects.toThrow() // gap
    const rx = await FrameCipher.create(key, 1)
    await rx.open(a)
    await expect(rx.open(a)).rejects.toThrow() // replay
    await expect((await FrameCipher.create(key, 2)).open(a)).rejects.toThrow() // wrong direction
  })

  it("matches xxhash64 known answers", async () => {
    for (const c of v.xxhash64) expect(await xxhash64(c.input)).toBe(c.xxhash64)
  })
})

describe("fresh handshake", () => {
  it("agrees on keys; a wrong secret or room gives a different authKey", async () => {
    const [a, b] = [await newEphemeral(), await newEphemeral()]
    const ka = await deriveKeys("amber-otter-kite-moth", "4821", a, b.pub)
    const kb = await deriveKeys("amber-otter-kite-moth", "4821", b, a.pub)
    expect(toHex(ka.authKey)).toBe(toHex(kb.authKey))
    expect(toHex((await deriveKeys("amber-otter-kite-xxxx", "4821", b, a.pub)).authKey)).not.toBe(
      toHex(ka.authKey),
    )
    expect(toHex((await deriveKeys("amber-otter-kite-moth", "4822", b, a.pub)).authKey)).not.toBe(
      toHex(ka.authKey),
    )
    expect(concat(a.pub).length).toBe(32)
  })
})
