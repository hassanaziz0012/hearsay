package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Ctrl+Super+Space would clash with input source switching on some desktops.
func defaultHotkey() string {
	if runtime.GOOS == "darwin" {
		return "ctrl+option+space"
	}
	return "ctrl+alt+space"
}

func wayland() bool { return runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != "" }

// startHotkey registers the global hotkey for `run`. Wayland doesn't let apps grab keys:
// there a desktop shortcut runs `hearsay toggle` instead.
func (a *app) startHotkey() {
	spec := a.cfg.Hotkey
	if spec == "" {
		spec = defaultHotkey()
	}
	if spec == "none" {
		return
	}
	if wayland() {
		a.logf("Wayland: no global hotkey, bind a desktop shortcut to `hearsay toggle`")
		return
	}
	mods, key, err := splitHotkey(spec)
	if err == nil {
		err = registerHotkey(mods, key, func() { go a.trayAction(a.toggle) })
	}
	if err != nil {
		fmt.Fprintf(a.log, "hearsay: hotkey %s: %v\n", spec, err)
		a.notify(fmt.Sprintf("Couldn't set up the hotkey %s: %v", spec, err), 5000, true)
	}
}

// splitHotkey turns "Ctrl+Alt+Space" into ["ctrl" "alt"] and "space".
func splitHotkey(spec string) (mods []string, key string, err error) {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(spec, " ", "")), "+")
	key = parts[len(parts)-1]
	if key == "" {
		return nil, "", fmt.Errorf("no key in %q", spec)
	}
	return parts[:len(parts)-1], key, nil
}
