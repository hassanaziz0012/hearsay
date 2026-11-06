package main

import (
	"os/exec"
	"strings"
	"syscall"
)

// Only Linux has Wayland.
func (a *app) setupDesktopShortcut(exe string) error { return nil }

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// setupAutostart offers a Run registry value for `hearsay run` (the tray icon).
func (a *app) setupAutostart(exe string) error {
	value := `"` + exe + `" run`
	out, err := exec.Command("reg", "query", runKey, "/v", "Hearsay").Output()
	if err == nil {
		if strings.Contains(string(out), value) {
			done("the tray icon starts at login")
			return nil
		}
		if !yes("Another hearsay starts at login. Point it at this one ("+exe+")?", true) {
			return nil
		}
	} else if !yes("Start the Hearsay tray icon at login?", true) {
		return nil
	}
	if err := exec.Command("reg", "add", runKey, "/v", "Hearsay", "/t", "REG_SZ", "/d", value, "/f").Run(); err != nil {
		return err
	}
	done("the tray icon starts at login")
	return startDetached(exe, "run")
}

func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	// DETACHED_PROCESS: no console window for the tray.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
	if err := cmd.Start(); err != nil {
		return err
	}
	done("started `hearsay run`")
	return cmd.Process.Release()
}
