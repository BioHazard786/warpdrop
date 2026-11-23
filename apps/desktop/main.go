package main

import (
	"embed"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/BioHazard786/Warpdrop/apps/desktop/core"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// appID names the app to the desktop: Linux matches it to the .desktop entry (StartupWMClass) for the icon and name.
const appID = "com.biohazard786.warpdrop"

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

// PickFolder asks where received files should go; "" when cancelled.
func (d *Dialogs) PickFolder() (string, error) {
	return d.app.Dialog.OpenFile().SetTitle("Choose where received files go").CanChooseFiles(false).
		CanChooseDirectories(true).CanCreateDirectories(true).AttachToWindow(d.win).PromptForSingleSelection()
}

// Reveal opens a folder in the file manager, or a file in its default app.
func (d *Dialogs) Reveal(path string) error { return d.app.Browser.OpenFile(path) }

// ShowFile opens the file manager with the file selected, or just its folder where that isn't possible.
func (d *Dialogs) ShowFile(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", "/select,", path).Start() // explorer exits 1 even when it worked, so don't wait
	case "darwin":
		return exec.Command("open", "-R", path).Run()
	}
	// the freedesktop file manager interface (Dolphin, Nautilus, Nemo...); dbus-send splits arrays on commas
	uri := strings.ReplaceAll((&url.URL{Scheme: "file", Path: path}).String(), ",", "%2C")
	err := exec.Command("dbus-send", "--session", "--dest=org.freedesktop.FileManager1", "--type=method_call",
		"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:"+uri, "string:").Run()
	if err != nil {
		return d.Reveal(filepath.Dir(path))
	}
	return nil
}

func main() {
	var app *application.App
	var visibleItem *application.MenuItem
	var notify func(core.Transfer)
	stateDir := ""
	if dir, err := os.UserConfigDir(); err == nil {
		stateDir = filepath.Join(dir, "warpdrop") // beside the CLI's config.json
	}
	engine, err := core.New(core.Config{Server: os.Getenv("WARPDROP_SERVER"), Dir: os.Getenv("WARPDROP_DIR"), StateDir: stateDir}, func(event string, data any) {
		if app == nil {
			return
		}
		if v, ok := data.(bool); ok && event == "visible" && visibleItem != nil {
			visibleItem.SetChecked(v) // the tray follows the switch in the window
		}
		if t, ok := data.(core.Transfer); ok && notify != nil {
			notify(t)
		}
		app.Event.Emit(event, data)
	})
	if err != nil {
		log.Fatal(err)
	}
	dialogs := &Dialogs{}
	notifier := notifications.New()
	quitting := false

	app = application.New(application.Options{
		Name:        "WarpDrop",
		Description: "Send files to nearby devices or with a share code",
		Services: []application.Service{
			application.NewService(engine), application.NewService(dialogs), application.NewService(notifier),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		// one copy only: a second one could not listen on the same names and would confuse the tray
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "com.biohazard786.warpdrop"},
		OnShutdown:     engine.Shutdown,
		Mac:            application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Linux:          application.LinuxOptions{ApplicationID: appID},
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

	// Notifications, for when the window is hidden in the tray or behind others: an offer (answerable right
	// there) and a finished transfer. Each transfer announces each of those once.
	var nmu sync.Mutex
	told := map[string]string{}
	notify = func(t core.Transfer) {
		what := ""
		switch {
		// the core reports "offered" a moment before the file list arrives: wait for it, or the toast reads "0 files"
		case t.Kind == "receive" && t.Status == "offered" && !t.Done && len(t.Files) > 0:
			what = "offer"
		case t.Status == "done":
			what = "done"
		}
		nmu.Lock()
		fresh := what != "" && told[t.ID] != what
		if fresh {
			told[t.ID] = what
		}
		nmu.Unlock()
		if !fresh || (win.IsVisible() && win.IsFocused()) {
			return
		}
		files := fmt.Sprintf("%d files", len(t.Files))
		if len(t.Files) == 1 {
			files = t.Files[0].Name
		}
		peer := t.Peer
		if peer == "" {
			peer = "Someone"
		}
		n := notifications.NotificationOptions{ID: what + ":" + t.ID}
		var err error
		switch {
		case what == "offer":
			n.Title, n.Body, n.CategoryID = peer+" wants to send you "+files, "Accept to save to your downloads folder.", "offer"
			err = notifier.SendNotificationWithActions(n)
		case t.Kind == "receive":
			n.Title, n.Body = "Received "+files, "From "+peer+". Every checksum matched."
			err = notifier.SendNotification(n)
		default:
			n.Title, n.Body = "Sent "+files, "Delivered to "+peer+"."
			err = notifier.SendNotification(n)
		}
		if err != nil {
			app.Logger.Warn("notification", "error", err)
		}
	}
	notifier.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		what, id, _ := strings.Cut(r.Response.ID, ":")
		switch {
		case what == "offer" && r.Response.ActionIdentifier == "accept":
			engine.Respond(id, true)
		case what == "offer" && r.Response.ActionIdentifier == "decline":
			engine.Respond(id, false)
		default:
			show()
		}
	})

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
		err := notifier.RegisterNotificationCategory(notifications.NotificationCategory{ID: "offer", Actions: []notifications.NotificationAction{
			{ID: "accept", Title: "Accept"}, {ID: "decline", Title: "Decline", Destructive: true},
		}})
		if err != nil {
			app.Logger.Warn("notifications", "error", err)
		}
		if err := engine.Start(); err != nil {
			app.Logger.Warn("start", "error", err)
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
