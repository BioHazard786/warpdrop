import { makeZip, predictLength } from "client-zip"
import streamSaver from "streamsaver"
import type { FileEntry } from "./protocol"
import type { FileWriter, Sink } from "./session"

// Self-hosted: the service worker and bridge page live in public/ (sw.js, mitm.html, mitm.js).
streamSaver.mitm = "/mitm.html"

type Named = FileEntry & { safeName: string }

/** "stream" writes straight to disk through the browser's download manager; "memory" buffers a Blob (Safari, no service worker). */
export function downloadMode(): "stream" | "memory" {
  const safari =
    /constructor/i.test(String(window.HTMLElement)) ||
    !!(window as unknown as { safari?: unknown }).safari
  return window.isSecureContext && "serviceWorker" in navigator && !safari ? "stream" : "memory"
}

/** "a.txt", "a.txt" → "a.txt", "a (1).txt": names inside one zip must be unique. */
export function uniqueNames(names: string[]): string[] {
  const seen = new Map<string, number>()
  return names.map((n) => {
    const k = n.toLowerCase()
    const c = seen.get(k) ?? 0
    seen.set(k, c + 1)
    if (c === 0) return n
    const dot = n.lastIndexOf(".")
    const [stem, ext] = dot > 0 ? [n.slice(0, dot), n.slice(dot)] : [n, ""]
    return `${stem} (${c})${ext}`
  })
}

function writerFor(ws: WritableStream<Uint8Array>): FileWriter {
  const w = ws.getWriter()
  return {
    write: async (chunk) => {
      await w.ready // backpressure: a slow disk blocks here, which delays acks, which stalls the sender
      await w.write(chunk)
    },
    close: () => w.close(),
    abort: () => w.abort(),
  }
}

/** Sink for the browser. One file streams directly; several are zipped on the fly (ZIP64, exact Content-Length). */
export class DownloadSink implements Sink {
  private files: () => Named[]
  private zip: {
    writers: Promise<ReadableStream<Uint8Array>>[]
    resolvers: ((r: ReadableStream<Uint8Array>) => void)[]
  } | null = null

  constructor(files: () => Named[]) {
    this.files = files
  }

  async open(file: Named, idx: number): Promise<FileWriter> {
    const files = this.files()
    if (files.length === 1)
      return writerFor(streamSaver.createWriteStream(file.safeName, { size: file.size }))
    if (!this.zip) this.startZip(files)
    const ts = new TransformStream<Uint8Array, Uint8Array>()
    this.zip!.resolvers[idx]!(ts.readable)
    return writerFor(ts.writable)
  }

  private startZip(files: Named[]) {
    const names = uniqueNames(files.map((f) => f.safeName))
    const resolvers: ((r: ReadableStream<Uint8Array>) => void)[] = []
    const writers = files.map(
      (_, i) => new Promise<ReadableStream<Uint8Array>>((res) => (resolvers[i] = res)),
    )
    this.zip = { writers, resolvers }
    async function* entries() {
      for (let i = 0; i < files.length; i++)
        yield { name: names[i]!, input: await writers[i]!, size: files[i]!.size }
    }
    const length = predictLength(files.map((f, i) => ({ name: names[i]!, size: f.size })))
    const out = streamSaver.createWriteStream(`warpdrop-${files.length}-files.zip`, {
      size: Number(length),
    })
    // if any file's stream is aborted, the zip errors and the browser marks the download failed
    makeZip(entries(), { length })
      .pipeTo(out)
      .catch(() => {})
  }
}
