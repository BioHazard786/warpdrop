import { IconUpload } from "@tabler/icons-react"
import { useRef, useState } from "react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

/** Dashed drop target; the whole thing is a <label> so it works with keyboard and screen readers too. */
export function Dropzone({ onFiles }: { onFiles: (files: File[]) => void }) {
  const [over, setOver] = useState(false)
  const depth = useRef(0)
  return (
    <label
      className={cn(
        "flex cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border px-6 py-16 text-center transition-colors",
        "focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 hover:bg-muted/40",
        over && "border-foreground bg-muted/60",
      )}
      onDragEnter={(e) => {
        e.preventDefault()
        depth.current++
        setOver(true)
      }}
      onDragLeave={() => {
        if (--depth.current <= 0) setOver(false)
      }}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault()
        depth.current = 0
        setOver(false)
        const items = [...e.dataTransfer.items]
        if (items.some((i) => i.webkitGetAsEntry?.()?.isDirectory)) {
          toast.error("Folders aren't supported. Zip the folder first.")
          return
        }
        const files = [...e.dataTransfer.files]
        if (files.length) onFiles(files)
      }}
    >
      <IconUpload className="size-6 text-muted-foreground" aria-hidden />
      <span className="font-mono text-xs tracking-widest uppercase">
        Drop files here or click to choose
      </span>
      <span className="text-xs text-muted-foreground">
        Sent straight to the other device. Nothing is uploaded.
      </span>
      <input
        type="file"
        multiple
        className="sr-only"
        onChange={(e) => {
          const files = [...(e.target.files ?? [])]
          e.target.value = "" // allow picking the same file again later
          if (files.length) onFiles(files)
        }}
      />
    </label>
  )
}
