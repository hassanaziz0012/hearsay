package main

import (
	"fmt"
	"strings"
	"time"
)

// Dictation runs over the network through the app's PhoneServer: toggle, start and stop
// are in net.go.

func (a *app) requireKeyboard() error {
	if a.cfg.Keyboard == "" {
		return a.fail(fmt.Sprintf("no keyboard set. Put the IME component of the keyboard with Rambler in %s (\"keyboard\")", configPath()))
	}
	return nil
}

func (a *app) listening() {
	a.logf("listening")
	a.status("listening", "")
	a.notify("Listening… press again to paste", 2000, false)
}

// handOver gives the user the text a stop brought back (or says why there is none).
func (a *app) handOver(text string, paste bool) {
	a.heard = text
	a.copied = false
	switch {
	case !paste:
		a.notify("Cancelled", 2000, false)
	case strings.TrimSpace(text) == "":
		a.notify("Nothing heard", 2000, false)
	default:
		if a.stdout {
			fmt.Println(text)
		}
		a.deliver(text)
		a.logf("pasted")
	}
}

// deliver puts the text on the clipboard and, if enabled, pastes it into the focused field.
func (a *app) deliver(text string) {
	if err := copyText(text); err != nil {
		a.logf("copy: %v", err)
		a.notify("Done, but couldn't copy: "+err.Error(), 5000, true)
		return
	}
	a.copied = true
	if a.paste {
		time.Sleep(50 * time.Millisecond)
		err := sendPaste()
		if err == nil {
			a.notify("Pasted", 2000, false)
			return
		}
		a.logf("paste: %v", err)
	}
	a.notify("Copied to clipboard", 2000, false)
}
