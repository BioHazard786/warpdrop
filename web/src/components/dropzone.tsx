import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Kbd } from "@/components/ui/kbd"
import { toast } from "@/components/ui/toast"
import { cn } from "@/lib/utils"
import { IconArrowUp } from "@tabler/icons-react"
import { useRef, useState } from "react"

/** Dashed drop target; the whole thing is a <label> so it works with keyboard and screen readers too. */
export function Dropzone({ onFiles }: { onFiles: (files: File[]) => void }) {
  const [over, setOver] = useState(false)
  const depth = useRef(0)
  return (
    <label
      data-over={over || undefined}
      className={cn(
        "group/drop relative flex min-h-64 cursor-pointer rounded-2xl border-2 border-dashed border-input transition-colors",
        "hover:border-ring hover:bg-muted/40 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
        "data-over:border-foreground data-over:bg-muted/60",
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
          toast.add({
            type: "error",
            title: "Folders aren't supported",
            description: "Zip the folder first, then drop the zip.",
          })
          return
        }
        const files = [...e.dataTransfer.files]
        if (files.length) onFiles(files)
      }}
    >
      <Empty>
        <EmptyHeader>
          <EmptyMedia
            variant="icon"
            className="size-11 rounded-xl transition-transform group-hover/drop:-translate-y-0.5 group-data-over/drop:-translate-y-1 group-data-over/drop:bg-foreground group-data-over/drop:text-background"
          >
            <IconArrowUp className="size-5" />
          </EmptyMedia>
          <EmptyTitle className="text-base">
            {over ? "Release to send" : "Drop files to send"}
          </EmptyTitle>
          <EmptyDescription>
            or click to browse. Files stream straight to the other device; nothing is uploaded.
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent className="flex-row justify-center font-mono text-xs text-muted-foreground">
          <Kbd>any size</Kbd>
          <Kbd>any type</Kbd>
          <Kbd>many at once</Kbd>
        </EmptyContent>
      </Empty>
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
