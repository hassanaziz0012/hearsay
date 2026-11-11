//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// startDetached starts the tray now, in its own session so closing the terminal doesn't
// end it.
func startDetached(exe string, args ...string) error {
	if err := startProcess(exe, args...); err != nil {
		return err
	}
	done("started `hearsay run`")
	return nil
}

// startProcess starts exe in its own session, and doesn't wait for it.
func startProcess(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
