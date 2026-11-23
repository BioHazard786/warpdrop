import { NearbyDevices } from "@/components/nearby-devices"
import { peerFor, tileStatus } from "@/lib/nearby-status"
import { useNearby } from "@/store/nearby"
import { useSend } from "@/store/send"

/** Choose files. The input is created on click, so the page keeps a single file input (the dropzone's). */
export function pickFiles(onFiles: (files: File[]) => void) {
  const input = document.createElement("input")
  input.type = "file"
  input.multiple = true
  input.onchange = () => {
    const files = [...(input.files ?? [])]
    if (files.length) onFiles(files)
  }
  input.click()
}

/** Devices on the same network. Tap one to send it the selected files (picking them first if there are none). */
export function NearbyList() {
  const { devices, visible, connected, name, rename, setVisible, sendTo } = useNearby()
  const invites = useSend((s) => (s.mode === "nearby" ? s.invites : undefined))
  const peers = useSend((s) => s.peers)
  return (
    <NearbyDevices
      devices={devices}
      visible={visible}
      connected={connected}
      self={name}
      onRename={rename}
      setVisible={setVisible}
      status={(d) => tileStatus(invites?.[d.id], peers)}
      onPick={(d) => {
        const s = useSend.getState()
        const inv = s.invites[d.id]
        if (tileStatus(inv, s.peers)?.tone === "live") {
          const p = peerFor(inv!, s.peers)
          return p ? s.cancelPeer(p.id) : s.cancelInvite(d.id)
        }
        if (s.mode === "nearby" && s.phase !== "idle" && s.files.length) sendTo(d)
        else pickFiles((files) => sendTo(d, files))
      }}
    />
  )
}
