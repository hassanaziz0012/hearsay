// Command hearsay dictates on an Android phone with Rambler (Gboard's AI voice typing)
// and pastes the text on the PC. The phone sits on the desk; the helper app in app/
// provides the text field, presses the keyboard's keys and talks to the PC over the local
// network (net.go). adb is only used by setup and diagnose.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const usage = `hearsay: dictate with Rambler on the phone, paste on the PC.

  hearsay [toggle]   first call starts listening, second stops and pastes
  hearsay start|stop force one half of the toggle
  hearsay cancel     close Rambler without pasting
  hearsay run        tray icon and global hotkey (keeps running)
  hearsay setup      guided setup: app, phone, keyboard, test, hotkey. Asks whether to
                     use adb (--adb, does most of it) or not (--no-adb, by hand);
                     --apk FILE installs that APK over adb instead of the bundled one
  hearsay diagnose   save the keyboard's key ids to hearsay-diagnose.txt (for bug reports)
  hearsay pair [ADDRESS]  pair with the phone (no adb needed)
  hearsay pair --forget   forget the paired phone

Env: HEARSAY_SERIAL (pick an adb device), HEARSAY_PASTE=0 (copy only, no Ctrl+V),
     HEARSAY_DEBUG=1 (timings on stderr).
`

const (
	pkg      = "me.hassandev.hearsay"
	activity = pkg + "/.DictateActivity"

	settleMs        = 700 // text must stay unchanged this long after stopping
	settleTimeoutMs = 8000
)

type app struct {
	cfg     config
	adbPath string
	serial  string
	paste   bool
	debug   bool
	log     io.Writer // debug lines and errors: stderr, or the log file without a terminal
	stdout  bool      // print the dictated text (a CLI run, not the tray)

	mu sync.Mutex // one action at a time within this process (the tray)
	t0 time.Time  // start of the current action, for log timings
}

// reported is an error the user has already been told about (notification, tray, log).
type reported struct{ msg string }

func (r reported) Error() string { return r.msg }

func main() {
	cmd := "toggle"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "toggle", "start", "stop", "cancel", "run", "setup", "diagnose", "pair":
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if cmd == "run" {
		detachConsole()
	}
	a, err := newApp(cmd)
	if err == nil {
		switch cmd {
		case "toggle":
			err = a.action(a.toggle)
		case "start":
			err = a.action(a.start)
		case "stop":
			err = a.action(func() error { return a.stop(true) })
		case "cancel":
			err = a.action(func() error { return a.stop(false) })
		case "diagnose":
			err = a.action(a.diagnose)
		case "run":
			err = a.runTray()
		case "setup":
			err = a.setup(os.Args[2:])
		case "pair":
			err = a.pairPhone(os.Args[2:])
		}
	}
	if err != nil {
		var r reported
		if !errors.As(err, &r) {
			w := io.Writer(os.Stderr)
			if a != nil {
				w = a.log
			}
			fmt.Fprintln(w, "hearsay:", err)
		}
		os.Exit(1)
	}
}

func newApp(cmd string) (*app, error) {
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return nil, err
	}
	a := &app{log: os.Stderr, stdout: cmd != "run", t0: time.Now()}
	a.debug = os.Getenv("HEARSAY_DEBUG") == "1"
	// Run from a hotkey or at login there's no terminal: keep a log, with timings.
	if !isTerminal(os.Stderr) {
		if f, err := openLog(); err == nil {
			a.log, a.debug = f, true
			fmt.Fprintf(f, "--- %s %s\n", time.Now().Format("2006-01-02 15:04:05"), cmd)
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		return a, err
	}
	a.cfg = cfg
	a.serial = cfg.Serial
	if s := os.Getenv("HEARSAY_SERIAL"); s != "" {
		a.serial = s
	}
	a.paste = cfg.Paste == nil || *cfg.Paste
	if os.Getenv("HEARSAY_PASTE") == "0" {
		a.paste = false
	}
	a.adbPath, err = findAdb()
	// Only diagnose needs it; setup finds or downloads it if the user picks the adb path.
	if cmd != "diagnose" && errors.Is(err, errNoAdb) {
		err = nil
	}
	return a, err
}

// action runs one dictation step, unless another one (in this or another process) is
// still running: a press while busy is ignored rather than racing it.
func (a *app) action(fn func() error) error {
	if !a.mu.TryLock() {
		return errors.New("still busy with the last press")
	}
	defer a.mu.Unlock()
	unlock, err := lockRun()
	if err != nil {
		return err
	}
	defer unlock()
	a.t0 = time.Now()
	return fn()
}

func (a *app) logf(format string, args ...any) {
	if a.debug {
		fmt.Fprintf(a.log, "[%dms] "+format+"\n", append([]any{time.Since(a.t0).Milliseconds()}, args...)...)
	}
}

// fail tells the user about an error and returns it as reported.
func (a *app) fail(msg string) error { return a.failAs(msg, false) }

// failAs is fail with a "retry" status in the tray when retry is set: the next press can
// finish what this one couldn't.
func (a *app) failAs(msg string, retry bool) error {
	fmt.Fprintln(a.log, "hearsay:", msg)
	a.notify("Error: "+msg, 5000, true)
	if retry {
		a.status("retry", msg)
	} else {
		a.status("error", msg)
	}
	return reported{msg}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
