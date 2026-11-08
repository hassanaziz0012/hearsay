//go:build windows || (cgo && (linux || darwin))

package main

import (
	"fmt"

	"golang.design/x/hotkey"
)

var registered []*hotkey.Hotkey // unregistered when garbage collected, so keep them

// unregisterHotkeys drops the hotkeys registered so far (before registering a new one).
func unregisterHotkeys() {
	for _, hk := range registered {
		hk.Unregister()
	}
	registered = nil
}

func registerHotkey(mods []string, key string, fn func()) error {
	var ms []hotkey.Modifier
	for _, m := range mods {
		hm, ok := modifiers[m]
		if !ok {
			return fmt.Errorf("unknown modifier %q", m)
		}
		ms = append(ms, hm)
	}
	k, ok := keys[key]
	if !ok {
		return fmt.Errorf("unknown key %q", key)
	}
	hk := hotkey.New(ms, k)
	if err := hk.Register(); err != nil {
		return err
	}
	registered = append(registered, hk)
	go func() {
		for range hk.Keydown() {
			fn()
		}
	}()
	return nil
}

var keys = map[string]hotkey.Key{
	"space": hotkey.KeySpace, "return": hotkey.KeyReturn, "enter": hotkey.KeyReturn,
	"tab": hotkey.KeyTab, "escape": hotkey.KeyEscape, "esc": hotkey.KeyEscape,
	"a": hotkey.KeyA, "b": hotkey.KeyB, "c": hotkey.KeyC, "d": hotkey.KeyD, "e": hotkey.KeyE,
	"f": hotkey.KeyF, "g": hotkey.KeyG, "h": hotkey.KeyH, "i": hotkey.KeyI, "j": hotkey.KeyJ,
	"k": hotkey.KeyK, "l": hotkey.KeyL, "m": hotkey.KeyM, "n": hotkey.KeyN, "o": hotkey.KeyO,
	"p": hotkey.KeyP, "q": hotkey.KeyQ, "r": hotkey.KeyR, "s": hotkey.KeyS, "t": hotkey.KeyT,
	"u": hotkey.KeyU, "v": hotkey.KeyV, "w": hotkey.KeyW, "x": hotkey.KeyX, "y": hotkey.KeyY,
	"z":  hotkey.KeyZ,
	"f1": hotkey.KeyF1, "f2": hotkey.KeyF2, "f3": hotkey.KeyF3, "f4": hotkey.KeyF4,
	"f5": hotkey.KeyF5, "f6": hotkey.KeyF6, "f7": hotkey.KeyF7, "f8": hotkey.KeyF8,
	"f9": hotkey.KeyF9, "f10": hotkey.KeyF10, "f11": hotkey.KeyF11, "f12": hotkey.KeyF12,
}
