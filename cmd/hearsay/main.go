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
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const usage = `hearsay: dictate with Rambler on the phone, paste on the PC.

  hearsay            open the Hearsay window (setup the first time), with the tray icon
                     and global hotkey (what a double-click does)
  hearsay run        tray icon and global hotkey without the window (at login)
  hearsay toggle     first call starts listening, second stops and pastes
  hearsay start|stop force one half of the toggle
  hearsay cancel     close Rambler without pasting
  hearsay setup      guided setup on the terminal: app, phone, keyboard, test, hotkey.
                     Asks whether to use adb (--adb, does most of it) or not (--no-adb,
                     by hand); --apk FILE installs that APK over adb instead of the bundled one
  hearsay diagnose   save the keyboard's key ids to hearsay-diagnose.txt (needs adb)
  hearsay pair [ADDRESS]  pair with the phone (no adb needed)
  hearsay pair --forget   forget the paired phone

Env: HEARSAY_SERIAL (pick an adb device), HEARSAY_PASTE=0 (copy only, no Ctrl+V),
     HEARSAY_DEBUG=1 (timings on stderr), HEARSAY_RUN_DIR (status, lock and log files
     elsewhere, with XDG_CONFIG_HOME to run a second, separate Hearsay).
`

// version is set by release builds (-ldflags "-X main.version=v1.2.3").
var version = "dev"

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
	sdk     int // the phone's API level, once setup has asked (sdkLevel)
	paste   bool
	debug   bool
	log     io.Writer // debug lines and errors: stderr, or the log file without a terminal
	stdout  bool      // print the dictated text (a CLI run, not the tray)
	heard   string    // the text the last stop brought back
	copied  bool      // whether that text got onto the clipboard

	// Windows has no notifications here: errors from a press nobody can see (no terminal,
	// no tray) are kept in unseen and shown in a message box when the press is over.
	alertErrors bool
	unseen      []string

	mu sync.Mutex // one action at a time within this process (the tray)
	t0 time.Time  // start of the current action, for log timings
}

// reported is an error the user has already been told about (notification, tray, log).
type reported struct{ msg string }

func (r reported) Error() string { return r.msg }

func main() {
	cmd, args := "", []string(nil)
	if len(os.Args) > 1 {
		cmd, args = os.Args[1], os.Args[2:]
	}
	// A double-click opens the window.
	if cmd == "" {
		cmd = "gui"
	}
	switch cmd {
	case "toggle", "start", "stop", "cancel", "run", "gui", "setup", "diagnose", "pair":
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if cmd == "run" || cmd == "gui" {
		detachConsole()
	}
	a, err := newApp(cmd)
	if err == nil {
		err = runCmd(a, cmd, args)
	}
	if a != nil {
		for _, msg := range a.unseen {
			a.alert(msg)
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

// runCmd runs one command. A question nobody can answer (readLine) ends it with an error.
func runCmd(a *app, cmd string, args []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if r != errNoAnswers {
				panic(r)
			}
			err = errNoAnswers
		}
	}()
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
	case "run", "gui":
		err = a.runGUI(cmd == "gui")
	case "setup":
		err = a.setup(args)
		if ownConsole() {
			// Explorer's console window would vanish before the last lines are read.
			if err != nil {
				fmt.Println("hearsay:", err)
			}
			ask("\nPress Enter to close this window.")
		}
	case "pair":
		err = a.pairPhone(args)
	}
	return err
}

func newApp(cmd string) (*app, error) {
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return nil, err
	}
	a := &app{log: os.Stderr, stdout: cmd != "run" && cmd != "gui", t0: time.Now()}
	a.debug = os.Getenv("HEARSAY_DEBUG") == "1"
	// Run from a hotkey or at login there's no terminal: keep a log, with timings.
	if !isTerminal(os.Stderr) {
		if f, err := openLog(); err == nil {
			a.log, a.debug = f, true
			fmt.Fprintf(f, "--- %s %s\n", time.Now().Format("2006-01-02 15:04:05"), cmd)
		}
	}

	switch cmd {
	case "toggle", "start", "stop", "cancel":
		// A desktop shortcut's console window closes before anyone reads it.
		a.alertErrors = !isTerminal(os.Stderr) || ownConsole()
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
		return errBusy
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

// sentence ends msg with a full stop unless it already ends a sentence, so another
// sentence can follow it.
func sentence(msg string) string {
	msg = strings.TrimRight(msg, " ")
	if msg == "" || strings.ContainsAny(msg[len(msg)-1:], ".?!") {
		return msg
	}
	return msg + "."
}

func isTerminal(f *os.File) bool { return f != nil && term.IsTerminal(int(f.Fd())) }
