package main

import (
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	keybdEvent       = user32.NewProc("keybd_event")
	getAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalFree       = kernel32.NewProc("GlobalFree")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	moveMemory       = kernel32.NewProc("RtlMoveMemory")
	consoleProcs     = kernel32.NewProc("GetConsoleProcessList")
	freeConsole      = kernel32.NewProc("FreeConsole")
)

const (
	cfUnicodeText  = 13
	gmemMoveable   = 0x0002
	vkControl      = 0x11
	vkV            = 0x56
	keyeventfKeyup = 0x0002
)

func copyText(text string) error {
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Another program may have the clipboard open for a moment.
	for i := 0; ; i++ {
		r, _, err := openClipboard.Call(0)
		if r != 0 {
			break
		}
		if i == 10 {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer closeClipboard.Call()
	emptyClipboard.Call()

	size := uintptr(len(u) * 2)
	h, _, err := globalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return err
	}
	p, _, err := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return err
	}
	moveMemory.Call(p, uintptr(unsafe.Pointer(&u[0])), size)
	globalUnlock.Call(h)
	// On success the clipboard owns the memory.
	if r, _, err := setClipboardData.Call(cfUnicodeText, h); r == 0 {
		globalFree.Call(h)
		return err
	}
	return nil
}

// sendPaste presses Ctrl+V in the focused window, once the hotkey's modifiers are up:
// with Alt or Shift still held, the window would get Ctrl+Alt+V.
func sendPaste() error {
	for end := time.Now().Add(2 * time.Second); modifiersDown() && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	for _, k := range [][2]uintptr{{vkControl, 0}, {vkV, 0}, {vkV, keyeventfKeyup}, {vkControl, keyeventfKeyup}} {
		keybdEvent.Call(k[0], 0, k[1], 0)
	}
	return nil
}

// modifiersDown reports whether Shift, Ctrl, Alt or a Windows key is held.
func modifiersDown() bool {
	for _, vk := range []uintptr{0x10, 0x11, 0x12, 0x5B, 0x5C} { // VK_SHIFT, VK_CONTROL, VK_MENU, VK_LWIN, VK_RWIN
		if r, _, _ := getAsyncKeyState.Call(vk); r&0x8000 != 0 {
			return true
		}
	}
	return false
}

// notify: Windows gets no notifications here, the tray icon shows the state. Without the
// tray an error (keep) would go unseen, so main shows it in a message box afterwards.
func (a *app) notify(msg string, ms int, keep bool) {
	if keep && a.alertErrors && !trayRunning() {
		a.unseen = append(a.unseen, msg)
	}
}

// detachConsole closes the console window Windows opens for `hearsay run` at login (or
// from Explorer), but not the terminal it was started from: that one has other processes.
func detachConsole() {
	if ownConsole() {
		freeConsole.Call()
		// The old handles are dead; nil makes newApp log to the file instead.
		os.Stdout, os.Stderr = nil, nil
	}
}
