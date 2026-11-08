//go:build !windows && !(cgo && (linux || darwin))

package main

import "errors"

func registerHotkey(mods []string, key string, fn func()) error {
	return errors.New("this build has no global hotkey support (built without cgo)")
}

func unregisterHotkeys() {}
