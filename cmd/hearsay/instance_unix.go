//go:build !windows

package main

import (
	"os"
	"syscall"
)

var trayLock *os.File // held for as long as the tray runs; the OS drops it when we exit

// holdTray takes the lock that only one tray holds; false when another tray has it.
func holdTray() bool {
	f, err := os.OpenFile(runFile("tray.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true // can't tell: better two trays than none
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return false
	}
	trayLock = f
	return true
}

func trayRunning() bool {
	if !holdTray() {
		return true
	}
	if trayLock != nil {
		trayLock.Close()
		trayLock = nil
	}
	return false
}

// ownConsole is about Windows' console windows; elsewhere a terminal outlives us.
func ownConsole() bool { return false }

// alert is a notification that stays until dismissed.
func (a *app) alert(msg string) { a.notify(msg, 0, true) }
