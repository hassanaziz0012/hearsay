package main

import (
	"embed"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:generate sh icons/gen.sh

//go:embed icons/*.png icons/*.ico
var icons embed.FS

const (
	errorIconFor = 10 * time.Second // a plain error goes back to the idle icon after this
	// "busy" this old means the process died mid-press. The slowest press takes about 31 s
	// (3 s dial, 5 s mDNS, 3 s dial, then up to settleTimeoutMs+12 s for the reply).
	busyStale = 45 * time.Second
)

type trayState struct {
	icon, label string
	toggle      string // label of the start/stop item; "" disables it
	cancel      bool
}

var trayStates = map[string]trayState{
	"idle":      {"mic", "Ready", "Start dictation", false},
	"busy":      {"busy", "Working…", "", false},
	"listening": {"listening", "Listening", "Stop and paste", true},
	"error":     {"error", "Error", "Start dictation", false},
	// The phone was unreachable; the next press finishes the job.
	"retry": {"error", "Reconnect the phone", "Finish (press again)", false},
}

// tray is the tray icon and its menu. It shows the state of the last press (from the
// status file, so presses from a desktop shortcut show up too) and runs presses.
type tray struct {
	icon                          *application.SystemTray
	menu                          *application.Menu
	label, detail, hotkey, toggle *application.MenuItem
	cancel, update, phoneUpdate   *application.MenuItem
}

func (g *gui) newTray() *tray {
	t := &tray{icon: g.wails.SystemTray.New(), menu: application.NewMenu()}
	t.label = t.menu.Add("Ready").SetEnabled(false)
	t.detail = t.menu.Add("").SetEnabled(false).SetHidden(true)
	t.hotkey = t.menu.Add("").SetEnabled(false)
	t.menu.AddSeparator()
	t.toggle = t.menu.Add("Start dictation").OnClick(func(*application.Context) {
		go g.a.trayAction(g.a.toggle)
	})
	t.cancel = t.menu.Add("Cancel").SetHidden(true).OnClick(func(*application.Context) {
		go g.a.trayAction(func() error { return g.a.stop(false) })
	})
	t.menu.AddSeparator()
	// A new release: opens the window, where its banner has "Update now".
	t.update = t.menu.Add("").SetHidden(true).OnClick(func(*application.Context) { g.show() })
	t.phoneUpdate = t.menu.Add("Update the phone app…").SetHidden(true).OnClick(func(*application.Context) { g.show() })
	t.menu.Add("Open Hearsay").OnClick(func(*application.Context) { g.show() })
	t.menu.Add("Quit Hearsay").OnClick(func(*application.Context) { g.wails.Quit() })
	t.icon.SetMenu(t.menu)
	if runtime.GOOS != "darwin" { // macOS shows the label next to the icon; the icon is enough
		t.icon.SetLabel("Hearsay")
	}
	t.icon.OnDoubleClick(g.show)
	t.setIcon("mic")
	return t
}

// show puts the state of the last press (st and msg from trayDisplay) in the tray.
func (t *tray) show(st, msg string) {
	s := trayStates[st]
	lbl := s.label
	if st == "busy" && msg != "" { // "Starting…" / "Finishing…"
		lbl, msg = msg, ""
	}
	t.setIcon(s.icon)
	t.icon.SetTooltip("Hearsay: " + lbl)
	t.label.SetLabel(lbl)
	t.detail.SetLabel(msg).SetHidden(msg == "")
	if s.toggle != "" {
		t.toggle.SetLabel(s.toggle).SetEnabled(true)
	} else {
		t.toggle.SetLabel("Start dictation").SetEnabled(false)
	}
	t.cancel.SetHidden(!s.cancel)
}

// The item setters update the tray's menu themselves: menu.Update() would rebuild it as a
// window menu, and on Linux the tray then no longer sees the changes.
func (t *tray) setHotkey(line string) { t.hotkey.SetLabel(line) }

// setUpdate shows label as the update item, or hides it ("").
func (t *tray) setUpdate(label string) { t.update.SetLabel(label).SetHidden(label == "") }

func (t *tray) setIcon(name string) {
	read := func(f string) []byte {
		b, err := icons.ReadFile("icons/" + f)
		if err != nil {
			panic(err) // embedded: can't happen
		}
		return b
	}
	switch {
	case runtime.GOOS == "windows":
		t.icon.SetIcon(read(name + ".ico"))
	case runtime.GOOS == "darwin" && name != "listening":
		t.icon.SetTemplateIcon(read(name + "-template.png"))
	default:
		t.icon.SetIcon(read(name + ".png"))
	}
}

// trayDisplay is what the tray should show for the status file now.
func trayDisplay() (st, msg string) {
	st, msg, age := readStatus()
	if _, ok := trayStates[st]; !ok || (st == "busy" && age > busyStale) {
		return "idle", ""
	}
	if st == "error" && age > errorIconFor {
		return "idle", "Last error: " + msg
	}
	return st, msg
}

func (a *app) trayAction(fn func() error) {
	if err := a.action(fn); err != nil {
		var r reported
		if !errors.As(err, &r) {
			fmt.Fprintln(a.log, "hearsay:", err)
		}
	}
}
