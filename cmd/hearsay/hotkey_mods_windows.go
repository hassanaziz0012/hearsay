package main

import "golang.design/x/hotkey"

var modifiers = map[string]hotkey.Modifier{
	"ctrl": hotkey.ModCtrl, "control": hotkey.ModCtrl, "alt": hotkey.ModAlt,
	"shift": hotkey.ModShift, "win": hotkey.ModWin, "super": hotkey.ModWin,
}
