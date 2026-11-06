package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// firstTool returns the first of the commands that is installed.
func firstTool(cmds ...[]string) []string {
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err == nil {
			return c
		}
	}
	return nil
}

func copyText(text string) error {
	wl := []string{"wl-copy"}
	x := [][]string{{"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	var c []string
	if wayland() {
		c = firstTool(append([][]string{wl}, x...)...)
	} else {
		c = firstTool(append(x, wl)...)
	}
	if c == nil {
		return errors.New("no clipboard tool (install wl-clipboard, xclip or xsel)")
	}
	// These fork to keep serving the clipboard, so their output must not be a pipe we'd
	// wait on (nil stdout/stderr is /dev/null).
	cmd := exec.Command(c[0], c[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// sendPaste presses Ctrl+V in the focused window.
func sendPaste() error {
	ydotool := []string{"ydotool", "key", "29:1", "47:1", "47:0", "29:0"} // KEY_LEFTCTRL, KEY_V
	wtype := []string{"wtype", "-M", "ctrl", "v", "-m", "ctrl"}
	xdotool := []string{"xdotool", "key", "--clearmodifiers", "ctrl+v"}
	var c []string
	if wayland() {
		c = firstTool(ydotool, wtype)
	} else {
		c = firstTool(xdotool, ydotool)
	}
	if c == nil {
		return errors.New("no key sender (install ydotool, wtype or xdotool)")
	}
	return exec.Command(c[0], c[1:]...).Run()
}

// notify shows msg in one notification that each press updates (its id is kept in a file,
// since each press may be a new process). Status updates are transient so they stay out
// of the notification list; keep=true (errors) leaves them there.
func (a *app) notify(msg string, ms int, keep bool) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	idFile := runFile("notify-id")
	id := "0"
	if b, err := os.ReadFile(idFile); err == nil {
		id = strings.TrimSpace(string(b))
	}
	args := []string{"-a", "Hearsay", "-t", strconv.Itoa(ms), "-p", "-r", id}
	if !keep {
		args = append(args, "-e")
	}
	out, err := exec.Command("notify-send", append(args, "Hearsay", msg)...).Output()
	if err == nil {
		os.WriteFile(idFile, out, 0o600)
	}
}

func detachConsole() {}
