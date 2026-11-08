package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The window's front end: plain HTML, CSS and JS, no build step. It calls the API's
// methods with the runtime Wails serves at /wails/runtime.js.
//
//go:embed frontend
var frontend embed.FS

// gui is the tray icon, the global hotkey and the window (setup, status, settings,
// troubleshooting), all in the one process that `hearsay` and `hearsay run` start.
type gui struct {
	a     *app
	wails *application.App
	win   *application.WebviewWindow
	tray  *tray

	mu          sync.Mutex
	hotkeyLine  string // the tray's line about the hotkey
	hotkeyErr   error  // why the hotkey doesn't work
	hotkeyPause bool   // unregistered while the window records a new one
	pairing     *pairing
	pairCancel  func()
}

// runGUI runs the tray and the hotkey, with the window shown or (show false: `hearsay
// run`, at login) hidden until the user opens it. Only one runs: a second launch brings
// the first one's window forward and exits.
func (a *app) runGUI(show bool) error {
	if a.cfg.Phone == nil {
		show = true // nothing works before setup: show it even at login
	}
	g := &gui{a: a}
	a.debug = true
	if a.log == os.Stderr {
		if f, err := openLog(); err == nil {
			a.log = io.MultiWriter(os.Stderr, f)
			fmt.Fprintf(f, "--- %s gui\n", time.Now().Format("2006-01-02 15:04:05"))
		}
	}

	assets, _ := fs.Sub(frontend, "frontend")
	icon, _ := icons.ReadFile("icons/app.png")
	g.wails = application.New(application.Options{
		Name:        "Hearsay",
		Description: "Dictate on the PC with Rambler on your phone",
		Icon:        icon,
		Services:    []application.Service{application.NewService(&API{g})},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Logger:      slog.New(slog.NewTextHandler(a.log, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Mac:         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		Windows:     application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:       application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ProgramName: "hearsay"},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: pkg,
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if !slices.Contains(d.Args, "run") {
					g.show()
				}
			},
		},
	})
	// The lock older versions' trays took, and which `hearsay setup` checks.
	if !holdTray() {
		a.alert("Another Hearsay is already running. Quit it from its tray icon and start Hearsay again.")
		return nil
	}

	bg := application.NewRGB(0xf6, 0xf5, 0xf2)
	if g.wails.Env.IsDarkMode() {
		bg = application.NewRGB(0x15, 0x15, 0x17)
	}
	g.win = g.wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Hearsay",
		Width:            980,
		Height:           680,
		MinWidth:         780,
		MinHeight:        580,
		Hidden:           !show,
		URL:              "/",
		BackgroundColour: bg,
		InitialPosition:  application.WindowCentered,
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInsetUnified,
			InvisibleTitleBarHeight: 52,
		},
	})
	// Closing the window keeps Hearsay running in the tray.
	g.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		g.win.Hide()
		e.Cancel()
	})

	g.tray = g.newTray()
	line, err := a.startHotkey()
	g.setHotkey(line, err)
	if err != nil && !show {
		a.alert(err.Error())
	}
	go g.watchStatus()
	return g.wails.Run()
}

func (g *gui) show() {
	g.win.Show()
	g.win.UnMinimise()
	g.win.Focus()
	if runtime.GOOS == "darwin" {
		g.wails.Show() // a menu bar app has to be brought forward itself
	}
}

func (g *gui) setHotkey(line string, err error) {
	g.mu.Lock()
	g.hotkeyLine, g.hotkeyErr = line, err
	g.mu.Unlock()
	g.tray.setHotkey(line)
	g.wails.Event.Emit("state")
}

// watchStatus follows the status file for the tray and the window.
func (g *gui) watchStatus() {
	shown := ""
	for {
		st, msg := trayDisplay()
		if st+"\t"+msg != shown {
			shown = st + "\t" + msg
			g.tray.show(st, msg)
			g.wails.Event.Emit("status", currentStatus())
		}
		time.Sleep(200 * time.Millisecond)
	}
}
