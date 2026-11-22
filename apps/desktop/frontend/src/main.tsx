import React from "react"
import ReactDOM from "react-dom/client"
import "~/index.css"
import { ThemeProvider } from "@/components/theme-provider"
import { App } from "~/app"
import { init } from "~/store"

init()
ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ThemeProvider>
      <App />
    </ThemeProvider>
  </React.StrictMode>,
)
