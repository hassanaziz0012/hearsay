package main

import (
	"html"
	"os"
	"path/filepath"
	"strings"
)

// Only Linux has Wayland, where the desktop's shortcut stands in for the global hotkey.
func (a *app) setupDesktopShortcut(exe string) error { return nil }
func gnomeShortcuts() bool                           { return false }
func desktopShortcut() string                        { return "" }
func setDesktopShortcut(exe, spec string) error      { return nil }

func launchAgent() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", pkg+".plist")
}

// autostartState reports whether a LaunchAgent starts exe's menu bar icon at login (on),
// or another hearsay's (other).
func autostartState(exe string) (on, other bool) {
	b, err := os.ReadFile(launchAgent())
	if err != nil {
		return false, false
	}
	on = strings.Contains(string(b), "<string>"+html.EscapeString(exe)+"</string>")
	return on, !on
}

// setAutostart adds or removes the LaunchAgent for `hearsay run` (the menu bar icon).
func setAutostart(exe string, on bool) error {
	p := launchAgent()
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + pkg + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + html.EscapeString(exe) + `</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(plist), 0o644)
}
