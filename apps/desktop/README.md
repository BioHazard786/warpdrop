# WarpDrop for desktop

A [Wails 3](https://v3.wails.io) app (Go + the system webview). The Go side runs the **same core as the CLI and
Android** (`cli/mobile`), so mDNS discovery, direct TCP, the share-code flow and disk writes are native Go, with no
browser limits. The UI is React and reuses the web app's presentational components (`web/src/components`) through
the `@` alias in [frontend/vite.config.ts](frontend/vite.config.ts), so it looks the same as the web app. [frontend/src/pages.tsx](frontend/src/pages.tsx)
rebuilds the web's two pages over the Go engine's state; keep it in step with `web/src/routes/index.tsx` and `r.$roomId.tsx`.
It shares no code with the web app's stores or transfer logic.

```
core/        the app's logic (Engine): drives cli/mobile, keeps state, emits events. No GUI imports, so it has tests.
main.go      Wails glue: window, tray, file dialog, event registration
frontend/    React UI; frontend/bindings is generated from core/ by `wails3 generate bindings`
```

## Build and run

```bash
sudo dnf install gtk4-devel webkitgtk6.0-devel          # Fedora; see v3.wails.io for other systems
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
(cd ../../web && bun install)                           # the UI imports the web app's components and fonts
wails3 dev                                              # hot reload
wails3 build                                            # bin/warpdrop-desktop
go test ./core                                          # the nearby tests need working multicast
```

`WARPDROP_SERVER` picks a self-hosted server and `WARPDROP_DIR` the download folder (default `~/Downloads`).

## Behaviour

- Closing the window keeps the app in the tray so this device stays visible; "Visible to nearby devices" in the tray
  (or the switch in the window) hides it. Quit from the tray.
- Drop files on the window to share them with a code; click the dropzone to browse.
- Devices come from the server's list and from mDNS, merged. Direct ones show a `LAN` badge and work offline.
- Every incoming transfer asks first and shows the 6-digit code to compare with the sender's screen.
- The first listen may trigger a firewall prompt on Windows. On Linux an inbound TCP port in 1025-65535
  must be open (Fedora Workstation allows it).

## Not done

The `warpdrop://` link handler, remembering the visibility choice, a first-run screen
explaining the firewall prompt, and Windows runs (the
Windows build type-checks with `GOOS=windows`, nothing more).
