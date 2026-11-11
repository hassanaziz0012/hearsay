package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The phone app's version. Each release's computer program and phone app come out
// together, so the program knows which phone app it came with (phoneApp, the APK's
// versionCode, set by scripts/release.sh). The phone puts its own in every reply, and
// requests carry phoneApp as "want", so either side can say when the phone app needs an
// update. Apps from before this sent no version: they read as 0, older than any.

// phoneApp is the versionCode of the phone app released with this program; "" in builds
// without it (go build), which then don't compare.
var phoneApp = ""

func wantedPhoneApp() int64 {
	n, _ := strconv.ParseInt(phoneApp, 10, 64)
	return n
}

// notePhoneApp records the version the paired phone's app reported, for the tray: each
// press may be a process of its own (the desktop shortcut).
func notePhoneApp(r phoneReply) {
	line := fmt.Sprintf("%d\t%s\n", r.App, r.AppVersion)
	p := runFile("phone-app")
	if b, err := os.ReadFile(p); err == nil && string(b) == line {
		return
	}
	writeAtomic(p, []byte(line))
}

// forgetPhoneApp: a newly paired phone hasn't said yet.
func forgetPhoneApp() { os.Remove(runFile("phone-app")) }

// PhoneApp is what the window shows about the phone app's version.
type PhoneApp struct {
	Version  string `json:"version"`  // "0.3.0", "" if the app is too old to say
	Outdated bool   `json:"outdated"` // older than the one released with this program
	Newer    bool   `json:"newer"`    // newer: this program is the one to update
}

// phoneAppState compares the phone's last reported app with the one this program came
// with; nil before the phone has answered, or in builds that don't know.
func phoneAppState() *PhoneApp {
	want := wantedPhoneApp()
	b, err := os.ReadFile(runFile("phone-app"))
	if want == 0 || err != nil {
		return nil
	}
	code, name, _ := strings.Cut(strings.TrimSpace(string(b)), "\t")
	have, err := strconv.ParseInt(code, 10, 64)
	if err != nil {
		return nil
	}
	return &PhoneApp{Version: name, Outdated: have < want, Newer: have > want}
}

// checkPhoneApp shows in the tray (and the window) whether the phone app needs an update,
// with a notification the first time for each version. The tray calls it when a press
// ends, since presses are what bring the phone's version.
func (g *gui) checkPhoneApp() {
	p := phoneAppState()
	now := PhoneApp{}
	if p != nil {
		now = *p
	}
	outdated := now.Outdated
	g.mu.Lock()
	changed := now != g.phoneApp
	g.phoneApp = now
	g.mu.Unlock()
	if !changed {
		return
	}
	g.tray.phoneUpdate.SetHidden(!outdated)
	g.wails.Event.Emit("state")
	if want := wantedPhoneApp(); outdated && g.a.cfg.PhoneUpdateSeen != want {
		g.a.notify("The Hearsay app on your phone needs an update. Open Hearsay on the phone and tap Download.", 10000, true)
		// Not on the status watcher's time: saving waits for a press to finish.
		go g.updateConfig(func(c *config) { c.PhoneUpdateSeen = want })
	}
}
