import {
  IconFile,
  IconFileMusic,
  IconFileText,
  IconFileZip,
  IconMovie,
  IconPhoto,
} from "@tabler/icons-react"

export function FileIcon({ name, type }: { name: string; type: string }) {
  const n = name.toLowerCase()
  const cls = "size-4 text-muted-foreground"
  if (type.startsWith("image/")) return <IconPhoto className={cls} aria-hidden />
  if (type.startsWith("video/")) return <IconMovie className={cls} aria-hidden />
  if (type.startsWith("audio/")) return <IconFileMusic className={cls} aria-hidden />
  if (/\.(zip|rar|7z|tar|gz|tgz|xz|bz2)$/.test(n))
    return <IconFileZip className={cls} aria-hidden />
  if (type.startsWith("text/") || /\.(pdf|docx?|txt|md|csv|xlsx?)$/.test(n))
    return <IconFileText className={cls} aria-hidden />
  return <IconFile className={cls} aria-hidden />
}
