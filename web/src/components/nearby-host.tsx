import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useNearby } from "@/store/nearby"
import type { Invite } from "@/transfer/nearby"
import { useNavigate } from "@tanstack/react-router"
import { useEffect, useRef } from "react"

/** Keeps this tab in the nearby list, and asks before opening a transfer someone started toward it. */
export function NearbyHost() {
  const { incoming, dismiss, decline } = useNearby()
  const navigate = useNavigate()
  const last = useRef<Invite | null>(null)
  if (incoming) last.current = incoming // keep the name on screen while the dialog fades out

  useEffect(() => {
    useNearby.getState().connect()
    return () => useNearby.getState().disconnect()
  }, [])

  const shown = incoming ?? last.current
  return (
    <AlertDialog open={!!incoming} onOpenChange={(open) => !open && dismiss()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{shown?.from.name} wants to send you files</AlertDialogTitle>
          <AlertDialogDescription>
            Nothing is saved yet. You'll see the file list and a 6-digit code to check with them
            first.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={decline}>Decline</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (!incoming) return
              void navigate({
                to: "/r/$roomId",
                params: { roomId: incoming.roomId },
                hash: `n-${incoming.ticket}`,
              })
              dismiss()
            }}
          >
            Review
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
