// X25519 + HKDF + HMAC handshake and relay AES-GCM framing (see protocol/PROTOCOL.md sections 3 and 4).
const enc = new TextEncoder()
const subtle = crypto.subtle
type Buf = Uint8Array<ArrayBuffer>
const buf = (b: Uint8Array): Buf => b as Buf // WebCrypto's typings reject ArrayBufferLike; ours are never shared

export const toHex = (b: Uint8Array): string =>
  Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("")
export const fromHex = (h: string): Uint8Array<ArrayBuffer> => {
  const out = new Uint8Array(h.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.slice(i * 2, i * 2 + 2), 16)
  return out
}
export const concat = (...parts: Uint8Array[]): Uint8Array<ArrayBuffer> => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let o = 0
  for (const p of parts) {
    out.set(p, o)
    o += p.length
  }
  return out
}

export interface Ephemeral {
  priv: CryptoKey
  pub: Uint8Array
}

export async function newEphemeral(): Promise<Ephemeral> {
  const k = (await subtle.generateKey({ name: "X25519" }, false, ["deriveBits"])) as CryptoKeyPair
  // the public key must be exportable even though the private one is not
  return { priv: k.privateKey, pub: new Uint8Array(await subtle.exportKey("raw", k.publicKey)) }
}

/** Test helper: build an ephemeral from a fixed 32-byte seed (PKCS8 wrapper around the raw key). */
export async function ephemeralFromSeed(seed: Uint8Array): Promise<Ephemeral> {
  const pkcs8 = concat(fromHex("302e020100300506032b656e04220420"), seed)
  const priv = await subtle.importKey("pkcs8", pkcs8, { name: "X25519" }, true, ["deriveBits"])
  // derive the public key via JWK, which carries it as "x"
  const jwk = await subtle.exportKey("jwk", priv)
  const pub = Uint8Array.from(atob(jwk.x!.replace(/-/g, "+").replace(/_/g, "/")), (c) =>
    c.charCodeAt(0),
  )
  return { priv, pub }
}

async function hkdf(
  ikm: Uint8Array<ArrayBuffer>,
  salt: Uint8Array<ArrayBuffer>,
  info: string,
  bytes: number,
) {
  const key = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"])
  return new Uint8Array(
    await subtle.deriveBits(
      { name: "HKDF", hash: "SHA-256", salt: buf(salt), info: enc.encode(info) },
      key,
      bytes * 8,
    ),
  )
}

export interface Keys {
  rootKey: Uint8Array
  authKey: Uint8Array
  sessionKey: Uint8Array
}

export async function deriveKeys(
  secret: string,
  roomId: string,
  own: Ephemeral,
  peerPub: Uint8Array,
): Promise<Keys> {
  const peer = await subtle.importKey("raw", buf(peerPub), { name: "X25519" }, true, [])
  const shared = new Uint8Array(
    await subtle.deriveBits({ name: "X25519", public: peer }, own.priv, 256),
  )
  const rootKey = await hkdf(enc.encode(secret), enc.encode(roomId), "warpdrop/2/root", 32)
  const keys = await hkdf(shared, rootKey, "warpdrop/2", 64)
  return { rootKey, authKey: keys.slice(0, 32), sessionKey: keys.slice(32) }
}

/** "sha-256 AA:BB:.." from an SDP a=fingerprint line to raw digest bytes; null if absent. */
export function dtlsFingerprint(sdp: string | undefined): Uint8Array | null {
  const m = /^a=fingerprint:sha-256 ([0-9A-Fa-f:]+)/m.exec(sdp ?? "")
  return m ? fromHex(m[1]!.replace(/:/g, "").toLowerCase()) : null
}

/** transcript = "warpdrop/2" ‖ senderPub ‖ receiverPub ‖ senderFp ‖ receiverFp (fingerprints omitted on the relay). */
export function transcript(
  senderPub: Uint8Array,
  receiverPub: Uint8Array,
  senderFp?: Uint8Array | null,
  receiverFp?: Uint8Array | null,
): Uint8Array {
  return concat(
    enc.encode("warpdrop/2"),
    senderPub,
    receiverPub,
    senderFp ?? new Uint8Array(),
    receiverFp ?? new Uint8Array(),
  )
}

async function hmacKey(k: Uint8Array, usage: "sign" | "verify") {
  return subtle.importKey("raw", buf(k), { name: "HMAC", hash: "SHA-256" }, false, [usage])
}

export async function mac(
  authKey: Uint8Array,
  label: "recv" | "send",
  t: Uint8Array,
): Promise<string> {
  const sig = await subtle.sign(
    "HMAC",
    await hmacKey(authKey, "sign"),
    concat(enc.encode(label), t),
  )
  return toHex(new Uint8Array(sig))
}

/** Constant-time verify (WebCrypto's verify compares in constant time). */
export async function verifyMac(
  authKey: Uint8Array,
  label: "recv" | "send",
  t: Uint8Array,
  macHex: string,
): Promise<boolean> {
  if (!/^[0-9a-f]{64}$/.test(macHex)) return false
  return subtle.verify(
    "HMAC",
    await hmacKey(authKey, "verify"),
    fromHex(macHex),
    concat(enc.encode(label), t),
  )
}

/** Relay frame cipher for one direction. dir: 1 = sender→receiver, 2 = receiver→sender. */
export class FrameCipher {
  private counter = 0n
  private key: CryptoKey
  private dir: 1 | 2
  constructor(key: CryptoKey, dir: 1 | 2) {
    this.key = key
    this.dir = dir
  }

  static async create(sessionKey: Uint8Array, dir: 1 | 2) {
    return new FrameCipher(
      await subtle.importKey("raw", buf(sessionKey), "AES-GCM", false, ["encrypt", "decrypt"]),
      dir,
    )
  }

  private nonce(counter: bigint): Uint8Array<ArrayBuffer> {
    const n = new Uint8Array(12)
    const v = new DataView(n.buffer)
    v.setUint32(0, this.dir)
    v.setBigUint64(4, counter)
    return n
  }

  /** kind 0 = text, 1 = binary */
  async seal(kind: 0 | 1, payload: Uint8Array): Promise<Uint8Array> {
    const pt = concat(Uint8Array.of(kind), payload)
    const ct = await subtle.encrypt(
      { name: "AES-GCM", iv: this.nonce(this.counter++) },
      this.key,
      buf(pt),
    )
    return new Uint8Array(ct)
  }

  /** Throws on a bad tag, replay or reorder (the counter must match exactly). */
  async open(wire: Uint8Array): Promise<{ kind: 0 | 1; payload: Uint8Array }> {
    const pt = new Uint8Array(
      await subtle.decrypt({ name: "AES-GCM", iv: this.nonce(this.counter) }, this.key, buf(wire)),
    )
    this.counter++
    if (pt.length < 1 || pt[0]! > 1) throw new Error("bad frame kind")
    return { kind: pt[0] as 0 | 1, payload: pt.subarray(1) }
  }
}
