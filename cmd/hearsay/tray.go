package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"fyne.io/systray"
)

//go:generate sh icons/gen.sh

//go:embed icons/*.png icons/*.ico
var icons embed.FS

const (
	errorIconFor = 10 * time.Second // a plain error goes back to the idle icon after this
	busyStale    = 30 * time.Second // "busy" this old means the process died mid-press
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

// runTray shows the state of the last press (from the status file, so presses from a
// desktop shortcut show up too) and runs presses from its menu and the global hotkey.
func (a *app) runTray() error {
	systray.Run(a.trayReady, nil)
	return nil
}

func (a *app) trayReady() {
	systray.SetTitle("Hearsay")
	label := systray.AddMenuItem("Ready", "")
	label.Disable()
	detail := systray.AddMenuItem("", "")
	detail.Disable()
	detail.Hide()
	systray.AddSeparator()
	toggle := systray.AddMenuItem("Start dictation", "")
	cancel := systray.AddMenuItem("Cancel", "")
	cancel.Hide()
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Hearsay", "")

	go func() {
		for {
			select {
			case <-toggle.ClickedCh:
				go a.trayAction(a.toggle)
			case <-cancel.ClickedCh:
				go a.trayAction(func() error { return a.stop(false) })
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
	a.startHotkey()

	show := func(st, msg string) {
		s := trayStates[st]
		lbl := s.label
		if st == "busy" && msg != "" { // "Starting…" / "Finishing…"
			lbl, msg = msg, ""
		}
		setTrayIcon(s.icon)
		systray.SetTooltip("Hearsay: " + lbl)
		label.SetTitle(lbl)
		detail.SetTitle(msg)
		if msg != "" {
			detail.Show()
		} else {
			detail.Hide()
		}
		if s.toggle != "" {
			toggle.SetTitle(s.toggle)
			toggle.Enable()
		} else {
			toggle.SetTitle("Start dictation")
			toggle.Disable()
		}
		if s.cancel {
			cancel.Show()
		} else {
			cancel.Hide()
		}
	}
	go func() {
		shown := ""
		for {
			st, msg := trayDisplay()
			if st+"\t"+msg != shown {
				shown = st + "\t" + msg
				show(st, msg)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
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

func setTrayIcon(name string) {
	read := func(f string) []byte {
		b, err := icons.ReadFile("icons/" + f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hearsay:", err)
		}
		return b
	}
	switch {
	case runtime.GOOS == "windows":
		systray.SetIcon(read(name + ".ico"))
	case runtime.GOOS == "darwin" && name != "listening":
		t := read(name + "-template.png")
		systray.SetTemplateIcon(t, t)
	default:
		systray.SetIcon(read(name + ".png"))
	}
}
