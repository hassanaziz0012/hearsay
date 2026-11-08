package main

import (
	"syscall"
	"unsafe"
)

var (
	createMutex = kernel32.NewProc("CreateMutexW")
	closeHandle = kernel32.NewProc("CloseHandle")
	messageBox  = user32.NewProc("MessageBoxW")
)

var trayMutex uintptr // held for as long as the tray runs; Windows drops it when we exit

// holdTray takes the named mutex that only one tray holds; false when another tray has it.
func holdTray() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\hearsay-tray`)
	h, _, err := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true // can't tell: better two trays than none
	}
	if err == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
		closeHandle.Call(h)
		return false
	}
	trayMutex = h
	return true
}

func trayRunning() bool {
	if !holdTray() {
		return true
	}
	if trayMutex != 0 {
		closeHandle.Call(trayMutex)
		trayMutex = 0
	}
	return false
}

// ownConsole: this console window is ours alone (opened by a double-click in Explorer),
// so it closes the moment we exit.
func ownConsole() bool {
	var ids [2]uint32
	n, _, _ := consoleProcs.Call(uintptr(unsafe.Pointer(&ids[0])), 2)
	return n == 1
}

// alert shows a message box: Windows has no notifications here, and these messages
// (no hotkey, already running) must not go unseen.
func (a *app) alert(msg string) {
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("Hearsay")
	const mbIconInformation, mbSetForeground = 0x40, 0x10000
	messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)),
		mbIconInformation|mbSetForeground)
}
