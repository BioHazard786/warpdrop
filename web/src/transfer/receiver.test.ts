import { expect, it, vi } from "vitest"

const joined = vi.fn()
vi.mock("./download", () => ({ DownloadSink: class {} })) // touches the DOM, which this test doesn't need
vi.mock("./signaling", async (orig) => ({
  ...(await orig<typeof import("./signaling")>()),
  fetchConfig: () => Promise.resolve({ iceServers: [] }),
  Signaling: class {
    constructor() {
      joined()
    }
    connect() {}
    send() {}
    close() {}
  },
}))

it("a client cancelled while fetching config never opens a signaling socket", async () => {
  const { ReceiverClient } = await import("./receiver")
  const c = new ReceiverClient("1234", "amber-otter-kite-moth", () => {})
  const started = c.start()
  c.cancel() // what StrictMode's mount/cleanup/mount does to the first client
  await started
  expect(joined).not.toHaveBeenCalled()
})
