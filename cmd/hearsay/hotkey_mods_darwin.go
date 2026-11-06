//go:build cgo

package main

import "golang.design/x/hotkey"

var modifiers = map[string]hotkey.Modifier{
	"ctrl": hotkey.ModCtrl, "control": hotkey.ModCtrl, "option": hotkey.ModOption,
	"alt": hotkey.ModOption, "shift": hotkey.ModShift, "cmd": hotkey.ModCmd,
	"command": hotkey.ModCmd, "super": hotkey.ModCmd,
}
