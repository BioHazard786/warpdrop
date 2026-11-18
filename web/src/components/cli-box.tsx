import {
  CodeBlock,
  CodeBlockActions,
  CodeBlockContent,
  CodeBlockCopyButton,
  CodeBlockLine,
} from "@/components/ui/code-block"

export const INSTALL_CMD = "curl -fsSL install-drop.warplabs.info | bash"

/** One-line terminal command with a copy button. */
export function CliBox({
  command = INSTALL_CMD,
  prompt = true,
}: {
  command?: string
  /** show the leading `$`; off for things that aren't shell commands, like a link */
  prompt?: boolean
}) {
  return (
    <CodeBlock size="sm">
      <CodeBlockContent>
        <CodeBlockLine>
          {prompt && <span className="text-muted-foreground select-none">$ </span>}
          {command}
        </CodeBlockLine>
      </CodeBlockContent>
      <CodeBlockActions>
        <CodeBlockCopyButton value={command} />
      </CodeBlockActions>
    </CodeBlock>
  )
}
