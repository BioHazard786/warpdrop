import { type Browser, type BrowserContext, type Page, expect, test } from "@playwright/test"
import { acceptAndRead, acceptButton, file, sha } from "./helpers"

// Every browser context here reaches the server from 127.0.0.1, so they all land in the same "nearby" list.
const sendButton = (p: Page) => p.getByRole("button", { name: /Send files to/ })

// Contexts from browser.newContext() outlive the test, and a lingering page would stay in the next test's list.
const opened: BrowserContext[] = []
test.afterEach(async () => {
  await Promise.all(opened.splice(0).map((c) => c.close()))
})
async function device(browser: Browser): Promise<Page> {
  const context = await browser.newContext()
  opened.push(context)
  const page = await context.newPage()
  await page.goto("/")
  return page
}

test.describe("nearby", () => {
  test("pick a device, check the code on both screens, receive with a matching checksum", async ({ browser }) => {
    const f = file("nearby.bin", 1_200_000)
    const rx = await device(browser)
    const tx = await device(browser)

    await expect(sendButton(tx)).toHaveCount(1)
    const [chooser] = await Promise.all([tx.waitForEvent("filechooser"), sendButton(tx).click()])
    await chooser.setFiles(f)

    // the receiver is asked first, and nothing is saved yet
    await expect(rx.getByText("wants to send you files")).toBeVisible()
    await rx.getByRole("button", { name: "Review" }).click()
    await expect(acceptButton(rx)).toBeVisible({ timeout: 30_000 })

    // both screens show the same 6-digit code
    const rxCode = (await rx.getByLabel(/Verification code/).textContent())!.replace(/\s/g, "")
    expect(rxCode).toMatch(/^\d{6}$/)
    await expect(tx.getByText(`code ${rxCode.slice(0, 3)} ${rxCode.slice(3)}`)).toBeVisible()

    const { bytes } = await acceptAndRead(rx)
    expect(sha(bytes)).toBe(sha(f.buffer))
    await expect(tx.getByText("Delivered", { exact: true })).toBeVisible({ timeout: 20_000 })
  })

  test("a hidden device is not listed, but can still see others and send", async ({ browser }) => {
    const listed = await device(browser)
    const hidden = await device(browser)
    await expect(sendButton(listed)).toHaveCount(1)

    await hidden.getByRole("button", { name: /see this one/ }).click()
    await expect(hidden.getByRole("button", { name: /see this one/ })).toHaveAttribute("aria-pressed", "false")
    await expect(sendButton(listed)).toHaveCount(0) // it left the list
    await expect(sendButton(hidden)).toHaveCount(1) // but still sees the listed one
  })

  test("declining the invite tells the sender at once", async ({ browser }) => {
    const rx = await device(browser)
    const tx = await device(browser)
    await expect(sendButton(tx)).toHaveCount(1)
    const [chooser] = await Promise.all([tx.waitForEvent("filechooser"), sendButton(tx).click()])
    await chooser.setFiles(file("ignored.bin", 10_000))
    await expect(rx.getByText("wants to send you files")).toBeVisible()
    await rx.getByRole("button", { name: "Decline" }).click()
    await expect(rx.getByText("wants to send you files")).toBeHidden()
    await expect(tx.getByText("Declined", { exact: true })).toBeVisible()
    await expect(tx.getByText("Delivered", { exact: true })).toHaveCount(0)
  })

  test("one set of files goes to two devices at once, each with its own progress", async ({ browser }) => {
    const f = file("both.bin", 800_000)
    const a = await device(browser)
    const b = await device(browser)
    const tx = await device(browser)
    await expect(sendButton(tx)).toHaveCount(2)

    // files first, then tap each device: the list stays on screen while sending
    await tx.getByRole("tabpanel", { name: "Nearby" }).locator('input[type="file"]').setInputFiles(f)
    await sendButton(tx).first().click()
    await expect(sendButton(tx)).toHaveCount(1)
    await sendButton(tx).first().click()
    await expect(tx.getByRole("button", { name: /Cancel sending to/ })).toHaveCount(2)

    for (const rx of [a, b]) {
      await expect(rx.getByText("wants to send you files")).toBeVisible()
      await rx.getByRole("button", { name: "Review" }).click()
      const { bytes } = await acceptAndRead(rx)
      expect(sha(bytes)).toBe(sha(f.buffer))
    }
    await expect(tx.getByText("Delivered", { exact: true })).toHaveCount(2, { timeout: 20_000 })
    await expect(tx.getByText("Sent", { exact: true })).toHaveCount(2)
  })
})
