import { defineConfig, devices } from "@playwright/test"

export const PORT = Number(process.env.E2E_PORT ?? 18095)

export default defineConfig({
  testDir: "./tests",
  timeout: 120_000,
  expect: { timeout: 20_000 },
  // one worker: the tests share one server and several create many browser contexts
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["html", { open: "never" }]],
  globalSetup: "./global-setup.ts",
  use: { baseURL: `http://127.0.0.1:${PORT}`, trace: "retain-on-failure" },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        // PW_CHANNEL=chrome uses the system Chrome instead of Playwright's Chromium
        channel: process.env.PW_CHANNEL || undefined,
        // expose real host candidates so two local peers can connect directly instead of via mDNS names
        launchOptions: { args: ["--disable-features=WebRtcHideLocalIpsWithMdns"] },
      },
    },
    {
      name: "firefox",
      grep: /@firefox/,
      use: {
        ...devices["Desktop Firefox"],
        launchOptions: { firefoxUserPrefs: { "media.peerconnection.ice.obfuscate_host_addresses": false } },
      },
    },
  ],
})
