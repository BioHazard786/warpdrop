const key = (f: File) => `${f.name}\0${f.size}\0${f.lastModified}`

/** The files in `added` that aren't in `existing` (or earlier in `added`). Same name, size and mtime means the same file. */
export function newFiles(existing: File[], added: File[]): File[] {
  const seen = new Set(existing.map(key))
  return added.filter((f) => !seen.has(key(f)) && !!seen.add(key(f)))
}
