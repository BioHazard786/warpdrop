import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/r/$roomId")({
  component: () => {
    const { roomId } = Route.useParams()
    return <p className="p-6 font-mono text-sm">warpdrop // receive {roomId}</p>
  },
})
