import { createFileRoute } from "@tanstack/react-router"

export const Route = createFileRoute("/")({
  component: () => <p className="p-6 font-mono text-sm">warpdrop // send</p>,
})
