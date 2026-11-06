package main

import (
	"os/exec"
	"strings"
)

func copyText(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// sendPaste presses Cmd+V in the focused window. macOS asks once for the Accessibility
// permission (for the terminal or for hearsay itself, whichever runs it).
func sendPaste() error {
	return exec.Command("osascript", "-e",
		`tell application "System Events" to keystroke "v" using command down`).Run()
}

func (a *app) notify(msg string, ms int, keep bool) {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	exec.Command("osascript", "-e",
		`display notification "`+q.Replace(msg)+`" with title "Hearsay"`).Run()
}

func detachConsole() {}
