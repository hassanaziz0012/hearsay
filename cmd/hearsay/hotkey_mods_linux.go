//go:build cgo

package main

import "golang.design/x/hotkey"

// X11: Mod1 is Alt and Mod4 Super on usual keymaps.
var modifiers = map[string]hotkey.Modifier{
	"ctrl": hotkey.ModCtrl, "control": hotkey.ModCtrl, "alt": hotkey.Mod1,
	"shift": hotkey.ModShift, "super": hotkey.Mod4, "win": hotkey.Mod4,
}
