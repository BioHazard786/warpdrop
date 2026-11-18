import { IconLoader2 } from "@tabler/icons-react"
import { cn } from "cn"

function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <IconLoader2
      data-slot="spinner"
      role="status"
      aria-label="Loading"
      className={cn("size-4 animate-spin", className)}
      {...props}
    />
  )
}

export { Spinner }
