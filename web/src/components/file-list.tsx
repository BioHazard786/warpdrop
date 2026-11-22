import { FileIcon, fileKind, isImage } from "@/components/file-icon"
import {
  Attachment,
  AttachmentAction,
  AttachmentActions,
  AttachmentContent,
  AttachmentDescription,
  AttachmentGroup,
  AttachmentMedia,
  AttachmentTitle,
} from "@/components/ui/attachment"
import { Spinner } from "@/components/ui/spinner"
import { formatBytes } from "@/lib/format"
import { IconCheck, IconX } from "@tabler/icons-react"
import { useState } from "react"

const SHOWN = 50

export type FileState = "idle" | "uploading" | "done"
export type FileItem = {
  name: string
  size: number
  type?: string
  /** full name when `name` was sanitised */
  title?: string
  /** blob URL thumbnail, sender side only */
  preview?: string
}

const Check = () => (
  <AttachmentActions className="text-muted-foreground">
    <IconCheck className="size-4" aria-label="Received" />
  </AttachmentActions>
)

const Remove = ({ name, onClick }: { name: string; onClick: () => void }) => (
  <AttachmentActions>
    <AttachmentAction
      aria-label={`Remove ${name}`}
      onClick={onClick}
      className="group-data-[orientation=vertical]/attachment:bg-background/80 group-data-[orientation=vertical]/attachment:backdrop-blur-sm"
    >
      <IconX />
    </AttachmentAction>
  </AttachmentActions>
)

function Media({ f, state }: { f: FileItem; state: FileState }) {
  const [broken, setBroken] = useState(false) // e.g. HEIC, which most browsers can't decode
  if (f.preview && !broken)
    return (
      <AttachmentMedia variant="image">
        <img src={f.preview} alt="" onError={() => setBroken(true)} />
      </AttachmentMedia>
    )
  return (
    <AttachmentMedia>
      {state === "uploading" ? <Spinner /> : <FileIcon name={f.name} type={f.type} />}
    </AttachmentMedia>
  )
}

/** Images as a scrolling row of tiles, everything else as rows with "TYPE · size". */
export function FileList({
  files,
  state = () => "done",
  checks = false,
  onRemove,
}: {
  files: FileItem[]
  state?: (i: number) => FileState
  /** show a check on finished files */
  checks?: boolean
  /** show a remove button on every file */
  onRemove?: (file: FileItem) => void
}) {
  const shown = files.slice(0, SHOWN).map((f, i) => ({ f, i, s: state(i) }))
  const images = shown.filter(({ f }) => isImage(f.name, f.type))
  const docs = shown.filter(({ f }) => !isImage(f.name, f.type))
  const meta = (f: FileItem) => `${fileKind(f.name, f.type)} · ${formatBytes(f.size)}`
  return (
    <div className="flex flex-col gap-3">
      {images.length > 0 && (
        <AttachmentGroup>
          {images.map(({ f, i, s }) => (
            <Attachment key={i} orientation="vertical" state={s} title={f.title ?? f.name}>
              <Media f={f} state={s} />
              <AttachmentContent>
                <AttachmentTitle>{f.name}</AttachmentTitle>
                <AttachmentDescription className="font-mono tabular-nums">
                  {meta(f)}
                </AttachmentDescription>
              </AttachmentContent>
              {checks && s === "done" && <Check />}
              {onRemove && <Remove name={f.name} onClick={() => onRemove(f)} />}
            </Attachment>
          ))}
        </AttachmentGroup>
      )}
      {docs.length > 0 && (
        <div className="scroll-fade-y no-scrollbar max-h-72 overflow-y-auto">
          <div className="flex flex-col gap-2">
            {docs.map(({ f, i, s }) => (
              <Attachment key={i} state={s} className="w-full" title={f.title ?? f.name}>
                <Media f={f} state={s} />
                <AttachmentContent>
                  <AttachmentTitle>{f.name}</AttachmentTitle>
                  <AttachmentDescription className="font-mono tabular-nums">
                    {meta(f)}
                  </AttachmentDescription>
                </AttachmentContent>
                {checks && s === "done" && <Check />}
                {onRemove && <Remove name={f.name} onClick={() => onRemove(f)} />}
              </Attachment>
            ))}
          </div>
        </div>
      )}
      {files.length > SHOWN && (
        <p className="px-1 font-mono text-xs text-muted-foreground">
          and {files.length - SHOWN} more
        </p>
      )}
    </div>
  )
}
