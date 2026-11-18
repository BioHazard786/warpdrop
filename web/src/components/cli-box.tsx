import { IconTerminal2 } from "@tabler/icons-react"
import { CopyButton } from "@/components/copy-button"

export const INSTALL_CMD = "curl -fsSL install.warpdrop.qzz.io | bash"

/** Quick-command box: the CLI can send to and receive from browsers. */
export function CliBox({ command = INSTALL_CMD, hint }: { command?: string; hint?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-muted/40 py-1.5 pr-1.5 pl-3">
      <IconTerminal2 className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <code className="min-w-0 flex-1 truncate font-mono text-xs select-all" title={hint}>
        {command}
      </code>
      <CopyButton value={command} />
    </div>
  )
}
