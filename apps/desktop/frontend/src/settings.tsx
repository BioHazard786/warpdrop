import { IconBrandGithub, IconFolder } from "@tabler/icons-react"
import { useEffect, useState } from "react"
import { Section } from "@/components/section"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Kbd, KbdGroup } from "@/components/ui/kbd"
import { toast } from "@/components/ui/toast"
import { actions, useApp } from "~/store"

const MOD = /Mac/.test(navigator.platform) ? "⌘" : "Ctrl"

export const SHORTCUTS: [string[], string][] = [
  [[MOD, "1–4"], "Nearby, Link, Receive, Activity"],
  [[MOD, ","], "Settings"],
  [[MOD, "O"], "Choose files to send"],
  [[MOD, "V"], "Receive a code or link you copied"],
  [[MOD, "B"], "Collapse the sidebar"],
  [["D"], "Light or dark theme"],
]

/** Device name, download folder and server, saved by the engine; then shortcuts and the version. */
export function SettingsPage() {
  const saved = useApp((s) => s.settings)
  const version = useApp((s) => s.version)
  const [form, setForm] = useState(saved)
  useEffect(() => setForm(saved), [saved]) // the engine's answer may arrive after this page opens
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const changed = form.device !== saved.device || form.dir !== saved.dir || form.server !== saved.server
  const edit = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setForm({ ...form, [k]: e.target.value })
    setError("")
  }

  return (
    <div className="flex flex-col gap-10">
      <Section title="This device">
        <Card>
          <CardContent>
            <form
              onSubmit={async (e) => {
                e.preventDefault()
                setBusy(true)
                try {
                  await actions.saveSettings(form)
                  setForm(useApp.getState().settings) // what the engine kept: trimmed, defaults filled in
                  toast.add({ type: "success", title: "Settings saved" })
                } catch (err) {
                  setError(err instanceof Error ? err.message : String(err))
                } finally {
                  setBusy(false)
                }
              }}
            >
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="device">Device name</FieldLabel>
                  <Input id="device" value={form.device} onChange={edit("device")} maxLength={64} autoComplete="off" />
                  <FieldDescription>What nearby devices and senders see. Leave empty for the default.</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="dir">Save received files to</FieldLabel>
                  <div className="flex gap-2">
                    <Input id="dir" value={form.dir} onChange={edit("dir")} className="font-mono" spellCheck={false} />
                    <Button
                      type="button"
                      variant="outline"
                      onClick={async () => {
                        const dir = await actions.pickFolder().catch(() => "")
                        if (dir) setForm({ ...form, dir })
                      }}
                    >
                      <IconFolder data-icon="inline-start" /> Choose…
                    </Button>
                  </div>
                  <FieldDescription>Files never overwrite what is already there.</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="server">Server</FieldLabel>
                  <Input
                    id="server"
                    value={form.server}
                    onChange={edit("server")}
                    placeholder="Hosted server (default)"
                    className="font-mono"
                    spellCheck={false}
                    autoComplete="off"
                  />
                  <FieldDescription>
                    Only for a self-hosted WarpDrop. Devices only see each other on the same server.
                  </FieldDescription>
                </Field>
                {error && <FieldError>{error}</FieldError>}
                <div className="flex gap-2">
                  <Button type="submit" disabled={!changed || busy}>
                    Save
                  </Button>
                  {changed && (
                    <Button type="button" variant="ghost" onClick={() => setForm(saved)}>
                      Discard
                    </Button>
                  )}
                </div>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      </Section>

      <Section title="Keyboard shortcuts">
        <ul className="flex flex-col divide-y rounded-xl border">
          {SHORTCUTS.map(([keys, what]) => (
            <li key={what} className="flex items-center justify-between gap-4 px-4 py-2.5 text-sm">
              <span>{what}</span>
              <KbdGroup>
                {keys.map((k) => (
                  <Kbd key={k}>{k}</Kbd>
                ))}
              </KbdGroup>
            </li>
          ))}
        </ul>
      </Section>

      <Section title="About">
        <div className="flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground">
          <span>
            WarpDrop <span className="font-mono text-foreground">{version || "dev"}</span> · files go straight from
            device to device.
          </span>
          <Button
            variant="outline"
            size="sm"
            render={<a href="https://github.com/BioHazard786/Warpdrop" target="_blank" rel="noreferrer" />}
          >
            <IconBrandGithub data-icon="inline-start" /> Source
          </Button>
        </div>
      </Section>
    </div>
  )
}
