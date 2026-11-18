// Pure transfer-state machines. No IO: each step returns the next state plus effects for the driver to run.
import { type CancelReason, type FileEntry, type Frame, sanitizeName } from "./protocol"

// ---------------------------------------------------------------- receiver
export type RState =
  | { s: "idle" }
  | { s: "offered"; files: FileEntry[] }
  | { s: "await_start"; files: FileEntry[]; idx: number; offset: number }
  | { s: "streaming"; files: FileEntry[]; idx: number; offset: number; bytes: number }
  | { s: "verifying"; files: FileEntry[]; idx: number }
  | { s: "done" }
  | { s: "failed"; reason: CancelReason | "closed"; byPeer: boolean }

export type REvent =
  | { e: "frame"; f: Frame }
  | { e: "chunk"; n: number }
  | { e: "accept"; resume?: { idx: number; offset: number } }
  | { e: "decline" }
  | { e: "cancel" } // local user
  | { e: "verified"; ok: boolean }
  | { e: "closed" }
  | { e: "bad" } // unparseable or schema-invalid frame

export type REffect =
  | { k: "send"; f: Frame }
  | { k: "offer"; files: (FileEntry & { safeName: string })[] }
  | { k: "begin_file"; idx: number; offset: number }
  | { k: "verify"; idx: number; hash: string }
  | { k: "finish_file"; idx: number }
  | { k: "abort"; reason: CancelReason | "closed"; byPeer: boolean }
  | { k: "complete" }

export const receiverInit: RState = { s: "idle" }
type R = { state: RState; fx: REffect[] }

const fail = (reason: CancelReason, tell: boolean): R => ({
  state: { s: "failed", reason, byPeer: false },
  fx: [
    ...(tell ? [{ k: "send", f: { t: "cancel", reason } } as REffect] : []),
    { k: "abort", reason, byPeer: false },
  ],
})
const protocolError = (): R => fail("protocol_error", true)

export function receiverStep(st: RState, ev: REvent): R {
  if (st.s === "done" || st.s === "failed") return { state: st, fx: [] }
  if (ev.e === "closed")
    return {
      state: { s: "failed", reason: "closed", byPeer: true },
      fx: [{ k: "abort", reason: "closed", byPeer: true }],
    }
  if (ev.e === "cancel") return fail("user", true)
  if (ev.e === "bad") return protocolError()
  if (ev.e === "frame" && ev.f.t === "cancel") {
    return {
      state: { s: "failed", reason: ev.f.reason, byPeer: true },
      fx: [{ k: "abort", reason: ev.f.reason, byPeer: true }],
    }
  }

  switch (st.s) {
    case "idle":
      if (ev.e === "frame" && ev.f.t === "manifest") {
        const files = ev.f.files
        return {
          state: { s: "offered", files },
          fx: [{ k: "offer", files: files.map((f) => ({ ...f, safeName: sanitizeName(f.name) })) }],
        }
      }
      return protocolError()
    case "offered":
      if (ev.e === "decline") return fail("declined", true)
      if (ev.e === "accept") {
        const { idx, offset } = ev.resume ?? { idx: 0, offset: 0 }
        if (idx >= st.files.length || offset > st.files[idx]!.size) return protocolError()
        return {
          state: { s: "await_start", files: st.files, idx, offset },
          fx: [{ k: "send", f: { t: "request", id: st.files[idx]!.id, offset } }],
        }
      }
      if (ev.e === "frame" && ev.f.t === "manifest") return { state: st, fx: [] } // duplicate while the user decides
      return protocolError()
    case "await_start": {
      const file = st.files[st.idx]!
      if (
        ev.e === "frame" &&
        ev.f.t === "file_start" &&
        ev.f.id === file.id &&
        ev.f.offset === st.offset
      ) {
        return {
          state: {
            s: "streaming",
            files: st.files,
            idx: st.idx,
            offset: st.offset,
            bytes: st.offset,
          },
          fx: [{ k: "begin_file", idx: st.idx, offset: st.offset }],
        }
      }
      return protocolError()
    }
    case "streaming": {
      const file = st.files[st.idx]!
      if (ev.e === "chunk") {
        const bytes = st.bytes + ev.n
        return bytes > file.size ? protocolError() : { state: { ...st, bytes }, fx: [] }
      }
      if (
        ev.e === "frame" &&
        ev.f.t === "file_end" &&
        ev.f.id === file.id &&
        st.bytes === file.size
      ) {
        return {
          state: { s: "verifying", files: st.files, idx: st.idx },
          fx: [{ k: "verify", idx: st.idx, hash: ev.f.hash }],
        }
      }
      return protocolError()
    }
    case "verifying": {
      if (ev.e !== "verified") return protocolError()
      if (!ev.ok) return fail("hash_mismatch", true)
      const file = st.files[st.idx]!
      const fx: REffect[] = [
        { k: "finish_file", idx: st.idx },
        { k: "send", f: { t: "file_done", id: file.id } },
      ]
      const next = st.idx + 1
      if (next >= st.files.length) return { state: { s: "done" }, fx: [...fx, { k: "complete" }] }
      return {
        state: { s: "await_start", files: st.files, idx: next, offset: 0 },
        fx: [...fx, { k: "send", f: { t: "request", id: st.files[next]!.id, offset: 0 } }],
      }
    }
  }
}

// ---------------------------------------------------------------- sender (one per receiver)
export type SState =
  | { s: "await_request"; files: FileEntry[] }
  | { s: "sending"; files: FileEntry[]; idx: number }
  | { s: "await_done"; files: FileEntry[]; idx: number }
  | { s: "done" }
  | { s: "failed"; reason: CancelReason | "closed"; byPeer: boolean }

export type SEvent =
  | { e: "frame"; f: Frame }
  | { e: "streamed"; idx: number } // driver finished sending file_start..file_end
  | { e: "cancel" }
  | { e: "closed" }
  | { e: "bad" }

export type SEffect =
  | { k: "send"; f: Frame }
  | { k: "stream_file"; idx: number; offset: number }
  | { k: "ack"; id: number; bytes: number }
  | { k: "file_acked"; idx: number }
  | { k: "abort"; reason: CancelReason | "closed"; byPeer: boolean }
  | { k: "complete" }

export const senderInit = (files: FileEntry[]): SState => ({ s: "await_request", files })
type S = { state: SState; fx: SEffect[] }

const sfail = (reason: CancelReason, tell: boolean): S => ({
  state: { s: "failed", reason, byPeer: false },
  fx: [
    ...(tell ? [{ k: "send", f: { t: "cancel", reason } } as SEffect] : []),
    { k: "abort", reason, byPeer: false },
  ],
})

export function senderStep(st: SState, ev: SEvent): S {
  if (st.s === "done" || st.s === "failed") return { state: st, fx: [] }
  if (ev.e === "closed")
    return {
      state: { s: "failed", reason: "closed", byPeer: true },
      fx: [{ k: "abort", reason: "closed", byPeer: true }],
    }
  if (ev.e === "cancel") return sfail("user", true)
  if (ev.e === "bad") return sfail("protocol_error", true)
  if (ev.e === "frame" && ev.f.t === "cancel") {
    return {
      state: { s: "failed", reason: ev.f.reason, byPeer: true },
      fx: [{ k: "abort", reason: ev.f.reason, byPeer: true }],
    }
  }
  if (ev.e === "frame" && ev.f.t === "ack") {
    // acks only make sense while sending the file they name; ignore stale ones from a previous file
    const cur = "idx" in st ? st.files[st.idx] : undefined
    return st.s === "sending" && cur && ev.f.id === cur.id && ev.f.bytes <= cur.size
      ? { state: st, fx: [{ k: "ack", id: ev.f.id, bytes: ev.f.bytes }] }
      : { state: st, fx: [] }
  }
  switch (st.s) {
    case "await_request": {
      if (ev.e === "frame" && ev.f.t === "request") {
        const id = ev.f.id
        const idx = st.files.findIndex((f) => f.id === id)
        if (idx < 0 || ev.f.offset > st.files[idx]!.size) return sfail("protocol_error", true)
        return {
          state: { s: "sending", files: st.files, idx },
          fx: [{ k: "stream_file", idx, offset: ev.f.offset }],
        }
      }
      return sfail("protocol_error", true)
    }
    case "sending":
      if (ev.e === "streamed" && ev.idx === st.idx)
        return { state: { s: "await_done", files: st.files, idx: st.idx }, fx: [] }
      return sfail("protocol_error", true)
    case "await_done": {
      if (ev.e === "frame" && ev.f.t === "file_done" && ev.f.id === st.files[st.idx]!.id) {
        const fx: SEffect[] = [{ k: "file_acked", idx: st.idx }]
        const next = st.idx + 1
        return next >= st.files.length
          ? { state: { s: "done" }, fx: [...fx, { k: "complete" }] }
          : { state: { s: "await_request", files: st.files }, fx }
      }
      // a receiver that lost the connection may re-request the same file from a new offset
      if (
        ev.e === "frame" &&
        ev.f.t === "request" &&
        ev.f.id === st.files[st.idx]!.id &&
        ev.f.offset <= st.files[st.idx]!.size
      ) {
        return {
          state: { s: "sending", files: st.files, idx: st.idx },
          fx: [{ k: "stream_file", idx: st.idx, offset: ev.f.offset }],
        }
      }
      return sfail("protocol_error", true)
    }
  }
}
