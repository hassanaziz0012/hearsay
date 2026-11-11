package main

import (
	"os/exec"
	"strings"
	"syscall"
)

// Only Linux has Wayland, where the desktop's shortcut stands in for the global hotkey.
func (a *app) setupDesktopShortcut(exe string) error { return nil }
func gnomeShortcuts() bool                           { return false }
func desktopShortcut() string                        { return "" }
func setDesktopShortcut(exe, spec string) error      { return nil }

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// autostartState reports whether the Run registry value starts exe's tray at login
// (on), or another hearsay's (other).
func autostartState(exe string) (on, other bool) {
	out, err := hidden(exec.Command("reg", "query", runKey, "/v", "Hearsay")).Output()
	if err != nil {
		return false, false
	}
	on = strings.Contains(string(out), `"`+exe+`" run`)
	return on, !on
}

// setAutostart adds or removes the Run registry value for `hearsay run` (the tray icon).
func setAutostart(exe string, on bool) error {
	if !on {
		if on, other := autostartState(exe); !on && !other {
			return nil
		}
		return hidden(exec.Command("reg", "delete", runKey, "/v", "Hearsay", "/f")).Run()
	}
	return hidden(exec.Command("reg", "add", runKey, "/v", "Hearsay", "/t", "REG_SZ", "/d", `"`+exe+`" run`, "/f")).Run()
}

// hidden keeps a console program from flashing a window when the tray (which has no
// console) runs it.
func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

func startDetached(exe string, args ...string) error {
	if err := startProcess(exe, args...); err != nil {
		return err
	}
	done("started `hearsay run`")
	return nil
}

// startProcess starts exe without waiting for it.
func startProcess(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	// DETACHED_PROCESS: no console window for the tray.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
