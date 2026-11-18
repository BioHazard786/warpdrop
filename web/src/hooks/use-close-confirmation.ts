import { useEffect } from "react"

/** Ask the browser to confirm before the tab closes while a transfer is running. */
export function useCloseConfirmation(active: boolean) {
  useEffect(() => {
    if (!active) return
    const h = (e: BeforeUnloadEvent) => e.preventDefault()
    addEventListener("beforeunload", h)
    return () => removeEventListener("beforeunload", h)
  }, [active])
}
