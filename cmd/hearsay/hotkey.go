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

// startHotkey (re)registers the global hotkey for the tray and returns the line about it
// for the tray and the window, and why it doesn't work if it doesn't. Wayland doesn't let
// apps grab keys: there a desktop shortcut runs `hearsay toggle` instead.
func (a *app) startHotkey() (string, error) {
	unregisterHotkeys()
	spec := a.cfg.Hotkey
	if spec == "" {
		spec = defaultHotkey()
	}
	if spec == "none" {
		return "No hotkey (use the tray menu)", nil
	}
	if wayland() {
		if bind := desktopShortcut(); bind != "" {
			return "Hotkey: " + hotkeyName(bind) + " (desktop shortcut)", nil
		}
		a.logf("Wayland: no global hotkey, bind a desktop shortcut to `hearsay toggle`")
		return "Hotkey: your desktop's shortcut", nil
	}
	mods, key, err := splitHotkey(spec)
	if err == nil {
		err = registerHotkey(mods, key, func() { go a.trayAction(a.toggle) })
	}
	if err != nil {
		fmt.Fprintf(a.log, "hearsay: hotkey %s: %v\n", spec, err)
		return "Hotkey " + hotkeyName(spec) + " isn't working", fmt.Errorf(
			"the hotkey %s doesn't work: %v. Another app may be using it: pick another one in "+
				"Hearsay's settings. Until then, use the tray icon's menu", hotkeyName(spec), err)
	}
	return "Hotkey: " + hotkeyName(spec), nil
}

// hotkeyName turns "ctrl+alt+space" into "Ctrl+Alt+Space".
func hotkeyName(spec string) string {
	mods, key, _ := splitHotkey(spec)
	parts := append(mods, key)
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "+")
}

// hotkeyHint says how to dictate with the configured hotkey.
func hotkeyHint(spec string) string {
	if spec == "" {
		spec = defaultHotkey()
	}
	switch {
	case spec == "none":
		return "dictate from its tray icon's menu."
	case wayland():
		return "dictate with the keyboard shortcut you set up for it."
	}
	return "press " + hotkeyName(spec) + " to dictate, and again to paste."
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

// quoteArg quotes a path for the shell-like parsing of desktop files and GNOME commands
// (and the command shown for a desktop shortcut).
func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
}
