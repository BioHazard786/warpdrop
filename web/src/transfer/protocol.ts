import { z } from "zod"

export const PROTOCOL_V = 2
export const MiB = 1024 * 1024
export const CHUNK_SIZE = 256 * 1024
export const ACK_EVERY = 4 * MiB
export const MAX_UNACKED = 32 * MiB
export const HIGH_WATER = 16 * MiB
export const LOW_WATER = 4 * MiB
export const MAX_FILES = 10_000
export const MAX_NAME_BYTES = 255
/** The manifest is one text frame; 200 kB stays under the 256 KiB message size both implementations advertise (about 2,300 files). */
export const MAX_MANIFEST_BYTES = 200_000
export const HANDSHAKE_TIMEOUT_MS = 20_000

export const CANCEL_REASONS = [
  "declined",
  "user",
  "auth_failed",
  "hash_mismatch",
  "protocol_error",
] as const
export type CancelReason = (typeof CANCEL_REASONS)[number]

const hex = (len: number) => z.string().regex(new RegExp(`^[0-9a-f]{${len}}$`))
const id = z.number().int().min(0).max(MAX_FILES)

export const fileEntry = z.object({
  id,
  name: z
    .string()
    .min(1)
    .refine((n) => new TextEncoder().encode(n).length <= MAX_NAME_BYTES),
  size: z.number().int().min(0).max(Number.MAX_SAFE_INTEGER),
  type: z.string().max(255),
})
export type FileEntry = z.infer<typeof fileEntry>

export const frameSchema = z.discriminatedUnion("t", [
  z.object({
    t: z.literal("hello"),
    v: z.number().int(),
    client: z.string().max(64),
    device: z.string().max(128),
    pub: hex(64),
  }),
  z.object({ t: z.literal("auth"), mac: hex(64) }),
  z.object({
    t: z.literal("manifest"),
    files: z.array(fileEntry).min(1).max(MAX_FILES),
  }),
  z.object({ t: z.literal("request"), id, offset: z.number().int().min(0) }),
  z.object({ t: z.literal("file_start"), id, offset: z.number().int().min(0) }),
  z.object({ t: z.literal("file_end"), id, hash: hex(16) }),
  z.object({ t: z.literal("file_done"), id }),
  z.object({ t: z.literal("ack"), id, bytes: z.number().int().min(0) }),
  z.object({ t: z.literal("cancel"), reason: z.enum(CANCEL_REASONS) }),
])
export type Frame = z.infer<typeof frameSchema>

/** Parse a text frame; null means "protocol error". Manifest ids must be unique. */
export function parseFrame(text: string): Frame | null {
  let raw: unknown
  try {
    raw = JSON.parse(text)
  } catch {
    return null
  }
  const r = frameSchema.safeParse(raw)
  if (!r.success) return null
  if (
    r.data.t === "manifest" &&
    new Set(r.data.files.map((f) => f.id)).size !== r.data.files.length
  )
    return null
  return r.data
}

export const encodeFrame = (f: Frame): string => JSON.stringify(f)

const WINDOWS_RESERVED = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i
// C0/C1 controls, DEL, and bidi overrides that can disguise a file extension
// eslint-disable-next-line no-control-regex
const UNSAFE_CHARS = /[\u0000-\u001f\u007f-\u009f‎‏‪-‮⁦-⁩]/g

/** Make a peer-supplied name safe to use as a single file name on any OS. */
export function sanitizeName(name: string): string {
  let n = name.slice(name.search(/[^/\\]*$/)) // last path segment
  n = n
    .replace(UNSAFE_CHARS, "")
    .replace(/[<>:"|?*]/g, "_")
    .replace(/[. ]+$/, "")
  if (n === "") n = "file"
  if (WINDOWS_RESERVED.test(n.split(".")[0]!)) n = "_" + n
  const enc = new TextEncoder()
  while (enc.encode(n).length > MAX_NAME_BYTES) n = [...n].slice(0, -1).join("")
  return n
}
