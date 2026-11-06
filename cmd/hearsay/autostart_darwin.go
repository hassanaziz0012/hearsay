package main

import (
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Only Linux has Wayland.
func (a *app) setupDesktopShortcut(exe string) error { return nil }

func launchAgent() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", pkg+".plist")
}

// setupAutostart offers a LaunchAgent for `hearsay run` (the menu bar icon).
func (a *app) setupAutostart(exe string) error {
	p := launchAgent()
	arg := "<string>" + html.EscapeString(exe) + "</string>"
	if b, err := os.ReadFile(p); err == nil {
		if strings.Contains(string(b), arg) {
			done("the menu bar icon starts at login (%s)", p)
			return nil
		}
		if !yes(p+" starts another hearsay. Point it at this one ("+exe+")?", true) {
			return nil
		}
	} else if !yes("Start the Hearsay menu bar icon at login?", true) {
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
		` + arg + `
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
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return err
	}
	done("the menu bar icon starts at login (%s)", p)
	return startDetached(exe, "run")
}

func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done("started `hearsay run`")
	return cmd.Process.Release()
}
