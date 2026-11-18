import {
  IconBinary,
  IconBook,
  IconBraces,
  IconCertificate,
  IconDatabase,
  IconDisc,
  IconFile,
  IconFileCode,
  IconFileMusic,
  IconFileSpreadsheet,
  IconFileText,
  IconFileTypeCss,
  IconFileTypeCsv,
  IconFileTypeDoc,
  IconFileTypeHtml,
  IconFileTypeJs,
  IconFileTypePdf,
  IconFileTypePpt,
  IconFileTypeSql,
  IconFileTypeSvg,
  IconFileTypeTs,
  IconFileTypeTsx,
  IconFileTypeTxt,
  IconFileTypeXls,
  IconFileTypeXml,
  IconFileTypeZip,
  IconFileTypography,
  IconKey,
  IconMarkdown,
  IconMovie,
  IconPackage,
  IconPhoto,
  IconTerminal2,
  type TablerIcon,
} from "@tabler/icons-react"

const EXT_TYPE: [RegExp, string][] = [
  [/\.(svg|png|jpe?g|gif|webp|avif|heic|heif|bmp|ico|tiff?)$/, "image/"],
  [/\.(mp4|mov|mkv|webm|avi|m4v|wmv)$/, "video/"],
  [/\.(mp3|wav|flac|ogg|m4a|aac|opus)$/, "audio/"],
]

/** First match wins. Keyed by extension because the sender only reliably knows names. */
const BY_EXT: [RegExp, TablerIcon][] = [
  [/\.svg$/, IconFileTypeSvg],
  [/\.pdf$/, IconFileTypePdf],
  [/\.docx?$|\.odt$|\.rtf$/, IconFileTypeDoc],
  [/\.csv$/, IconFileTypeCsv],
  [/\.xlsx?$|\.ods$/, IconFileTypeXls],
  [/\.numbers$|\.tsv$/, IconFileSpreadsheet],
  [/\.pptx?$|\.odp$|\.key$/, IconFileTypePpt],
  [/\.(zip|rar|7z|tar|gz|tgz|xz|bz2|zst)$/, IconFileTypeZip],
  [/\.(iso|dmg|img)$/, IconDisc],
  [/\.(apk|deb|rpm|pkg|msi|appimage|exe)$/, IconPackage],
  [/\.txt$|\.log$/, IconFileTypeTxt],
  [/\.mdx?$/, IconMarkdown],
  [/\.(epub|mobi|azw3?)$/, IconBook],
  [/\.tsx$/, IconFileTypeTsx],
  [/\.ts$/, IconFileTypeTs],
  [/\.(jsx?|mjs|cjs)$/, IconFileTypeJs],
  [/\.css$|\.scss$/, IconFileTypeCss],
  [/\.html?$/, IconFileTypeHtml],
  [/\.sql$/, IconFileTypeSql],
  [/\.xml$/, IconFileTypeXml],
  [/\.(json|ya?ml|toml|ini|env)$/, IconBraces],
  [/\.(sh|bash|zsh|fish|bat|ps1)$/, IconTerminal2],
  [/\.(py|go|rs|c|h|cc|cpp|hpp|java|kt|swift|rb|php|lua|vue|svelte|dart|cs)$/, IconFileCode],
  [/\.(db|sqlite3?|sql3)$/, IconDatabase],
  [/\.(pem|crt|cer|csr)$/, IconCertificate],
  [/\.(gpg|asc|pub|ppk)$/, IconKey],
  [/\.(ttf|otf|woff2?)$/, IconFileTypography],
  [/\.(bin|dat|o|so|dll|wasm)$/, IconBinary],
]

const mimeOf = (name: string, type: string) =>
  type || EXT_TYPE.find(([re]) => re.test(name.toLowerCase()))?.[1] || ""

/** Icon by extension, falling back to MIME type. */
export function FileIcon({ name, type = "" }: { name: string; type?: string }) {
  const mime = mimeOf(name, type)
  const Icon =
    BY_EXT.find(([re]) => re.test(name.toLowerCase()))?.[1] ??
    (mime.startsWith("image/")
      ? IconPhoto
      : mime.startsWith("video/")
        ? IconMovie
        : mime.startsWith("audio/")
          ? IconFileMusic
          : mime.startsWith("text/")
            ? IconFileText
            : IconFile)
  return <Icon aria-hidden />
}

export const isImage = (name: string, type = "") => mimeOf(name, type).startsWith("image/")

/** Short document type for the description line: "PDF", "ZIP", "JPG"... */
export function fileKind(name: string, type = ""): string {
  const ext = name.match(/\.([a-z0-9]{1,5})$/i)?.[1]?.toLowerCase()
  const k = ext ?? type.split("/")[1]?.split(/[+;]/)[0] ?? "file"
  return (k === "jpeg" ? "jpg" : k).toUpperCase()
}
