import { Events } from "@wailsio/runtime"
import { create } from "zustand"
import { toast } from "@/components/ui/toast"
import { Dialogs, Engine, type Device, type FileInfo, type Transfer } from "~/api"

export type Tab = "nearby" | "link"

interface App {
  device: string
  dir: string
  visible: boolean
  server: string // "" = connected to the server's device list
  devices: Device[]
  transfers: Transfer[]
  tab: Tab
  /** Files picked on the Nearby tab, sent to each device that gets tapped. */
  staged: { paths: string[]; files: FileInfo[] }
}

export const useApp = create<App>(() => ({
  device: "",
  dir: "",
  visible: true,
  server: "",
  devices: [],
  transfers: [],
  tab: "nearby",
  staged: { paths: [], files: [] },
}))

const set = useApp.setState
const fail = (e: unknown) => toast.add({ type: "error", title: e instanceof Error ? e.message : String(e) })

const upsert = (list: Transfer[], t: Transfer) =>
  list.some((x) => x.id === t.id) ? list.map((x) => (x.id === t.id ? t : x)) : [t, ...list]

/** Start listening to the engine, then load what it already knows. */
export function init() {
  Events.On("devices", (e) => set({ devices: (e.data as Device[]) ?? [] }))
  Events.On("server", (e) => set({ server: e.data as string }))
  Events.On("visible", (e) => set({ visible: e.data as boolean }))
  Events.On("transfer", (e) => set((s) => ({ transfers: upsert(s.transfers, e.data as Transfer) })))
  // files dropped on the window arrive as real paths: staged for nearby devices, or shared with a code
  Events.On("files-dropped", (e) => {
    const paths = e.data as string[]
    if (useApp.getState().tab === "nearby") void actions.stage(paths)
    else Engine.SendWithCode(paths).catch(fail)
  })
  Events.On("dismissed", (e) => set((s) => ({ transfers: s.transfers.filter((t) => t.id !== e.data) })))
  Engine.State()
    .then((st) =>
      set((s) => ({
        device: st.device,
        dir: st.dir,
        visible: st.visible,
        server: st.server,
        devices: st.devices ?? [],
        // events that arrived while this call was in flight are newer
        transfers: [...s.transfers, ...[...(st.transfers ?? [])].reverse().filter((t) => !s.transfers.some((x) => x.id === t.id))],
      })),
    )
    .catch(fail)
}

export const actions = {
  setVisible: (v: boolean) => Engine.SetVisible(v).catch(fail),
  setTab: (tab: Tab) => set({ tab }),
  /** Add files for nearby devices: `paths` from a drop, otherwise the picker. */
  async stage(paths?: string[]) {
    try {
      paths ??= (await Dialogs.PickFiles()) ?? []
      const cur = useApp.getState().staged
      const fresh = paths.filter((p) => !cur.paths.includes(p))
      if (!fresh.length) return
      const files = (await Engine.Describe(fresh)) ?? []
      set((s) => ({ staged: { paths: [...s.staged.paths, ...fresh], files: [...s.staged.files, ...files] } }))
    } catch (e) {
      fail(e)
    }
  },
  unstage: (i: number) =>
    set((s) => ({
      staged: { paths: s.staged.paths.filter((_, j) => j !== i), files: s.staged.files.filter((_, j) => j !== i) },
    })),
  clearStaged: () => set({ staged: { paths: [], files: [] } }),
  /** Send the staged files to a device, picking some first if there are none. Each tap is its own transfer. */
  async sendTo(d: Device) {
    if (!useApp.getState().staged.paths.length) await actions.stage()
    const { paths } = useApp.getState().staged
    if (paths.length) await Engine.SendToDevice(d.id, paths).catch(fail)
  },
  async sendWithCode() {
    try {
      const paths = await Dialogs.PickFiles()
      if (paths?.length) await Engine.SendWithCode(paths)
    } catch (e) {
      fail(e)
    }
  },
  openDownloads: () => Dialogs.Reveal(useApp.getState().dir).catch(fail),
  receiveCode: (code: string) => Engine.ReceiveCode(code.trim()).catch(fail),
  respond: (id: string, accept: boolean) => Engine.Respond(id, accept).catch(fail),
  cancel: (id: string) => Engine.Cancel(id).catch(fail),
  dismiss: (id: string) => Engine.Dismiss(id).catch(fail),
  /** Leave a finished receive: clear every finished one, or the next older result would take its place. */
  dismissReceived: () =>
    useApp
      .getState()
      .transfers.filter((t) => t.kind === "receive" && t.done)
      .forEach((t) => Engine.Dismiss(t.id).catch(fail)),
}
