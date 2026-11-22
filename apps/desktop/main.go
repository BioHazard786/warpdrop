package main

import (
	"embed"
	"log"
	"os"

	"github.com/BioHazard786/Warpdrop/apps/desktop/core"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// The events the engine emits; registering them gives the frontend typed listeners.
func init() {
	application.RegisterEvent[[]core.Device]("devices")
	application.RegisterEvent[core.Transfer]("transfer")
	application.RegisterEvent[string]("server")
	application.RegisterEvent[string]("dismissed")
	application.RegisterEvent[bool]("visible")
	application.RegisterEvent[[]string]("files-dropped")
}

// Dialogs is what only the native side can do: pick files by real path (a webview file input hides them).
type Dialogs struct {
	app *application.App
	win *application.WebviewWindow
}

func (d *Dialogs) PickFiles() ([]string, error) {
	return d.app.Dialog.OpenFile().SetTitle("Choose files to send").CanChooseFiles(true).CanChooseDirectories(false).
		AttachToWindow(d.win).PromptForMultipleSelection()
}

// Reveal opens a folder (or shows a file) in the system file manager.
func (d *Dialogs) Reveal(path string) error { return d.app.Browser.OpenFile(path) }

func main() {
	var app *application.App
	var visibleItem *application.MenuItem
	engine, err := core.New(core.Config{Server: os.Getenv("WARPDROP_SERVER"), Dir: os.Getenv("WARPDROP_DIR")}, func(event string, data any) {
		if app == nil {
			return
		}
		if v, ok := data.(bool); ok && event == "visible" && visibleItem != nil {
			visibleItem.SetChecked(v) // the tray follows the switch in the window
		}
		app.Event.Emit(event, data)
	})
	if err != nil {
		log.Fatal(err)
	}
	dialogs := &Dialogs{}
	quitting := false

	app = application.New(application.Options{
		Name:        "WarpDrop",
		Description: "Send files to nearby devices or with a share code",
		Services:    []application.Service{application.NewService(engine), application.NewService(dialogs)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		// one copy only: a second one could not listen on the same names and would confuse the tray
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "com.biohazard786.warpdrop"},
		OnShutdown:     engine.Shutdown,
		Mac:            application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
	})
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "WarpDrop",
		Width:            960,
		Height:           700,
		MinWidth:         560,
		MinHeight:        480,
		EnableFileDrop:   true, // dropped files arrive as real paths, which a webview's File objects hide
		BackgroundColour: application.NewRGB(10, 10, 10),
		URL:              "/",
	})
	dialogs.app, dialogs.win = app, win

	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		app.Event.Emit("files-dropped", e.Context().DroppedFiles())
	})
	// closing the window keeps the app running in the tray so this device stays visible to others
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !quitting {
			win.Hide()
			e.Cancel()
		}
	})
	show := func() { win.Show(); win.UnMinimise(); win.Focus() }

	menu := app.NewMenu()
	menu.Add("Show WarpDrop").OnClick(func(*application.Context) { show() })
	visibleItem = menu.AddCheckbox("Visible to nearby devices", true)
	visibleItem.OnClick(func(*application.Context) {
		if err := engine.SetVisible(visibleItem.Checked()); err != nil {
			app.Logger.Warn("visibility", "error", err)
		}
	})
	menu.AddSeparator()
	menu.Add("WarpDrop " + core.Version).SetEnabled(false) // the About line
	menu.Add("Quit").OnClick(func(*application.Context) { quitting = true; app.Quit() })
	tray := app.SystemTray.New()
	tray.SetIcon(icon)
	tray.SetTooltip("WarpDrop")
	tray.SetMenu(menu)
	tray.OnClick(show)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if err := engine.Start(); err != nil {
			app.Logger.Warn("start", "error", err)
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
