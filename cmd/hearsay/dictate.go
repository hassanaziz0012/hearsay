package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// transport drives the phone: over adb with the scripts in phone/ (adbTransport), or over
// the network through the app's PhoneServer (netTransport, net.go). Config "transport".
type transport interface {
	// toggle starts listening, or stops and pastes if a dictation is in progress.
	toggle() error
	start() error
	// stop ends the dictation: paste inserts the text and pastes it on the PC, otherwise
	// Rambler is closed and the text discarded.
	stop(paste bool) error
}

func (a *app) transport() transport {
	if a.transportName() == "net" {
		return netTransport{a}
	}
	return adbTransport{a}
}

// transportName is "adb" or "net": HEARSAY_TRANSPORT, else the config's, else adb.
func (a *app) transportName() string {
	if t := os.Getenv("HEARSAY_TRANSPORT"); t != "" {
		return t
	}
	if a.cfg.Transport == "net" {
		return "net"
	}
	return "adb"
}

func (a *app) toggle() error         { return a.transport().toggle() }
func (a *app) start() error          { return a.transport().start() }
func (a *app) stop(paste bool) error { return a.transport().stop(paste) }

// adbTransport keeps the dictation's state in the state file (files.go) and runs the
// phone side as shell scripts over adb.
type adbTransport struct{ *app }

func (a adbTransport) toggle() error {
	if _, ok := readState(); ok {
		return a.stop(true)
	}
	return a.start()
}

func (a *app) requireKeyboard() error {
	if a.cfg.Keyboard == "" {
		return a.fail(fmt.Sprintf("no keyboard set. Put the IME component of the keyboard with Rambler in %s (\"keyboard\")", configPath()))
	}
	return nil
}

func (a adbTransport) start() error {
	a.status("busy", "Starting…")
	if err := a.requireKeyboard(); err != nil {
		return err
	}
	if err := a.requireDevice(); err != nil {
		return err
	}
	out, rc := a.adb(8*time.Second, "shell",
		"settings get secure default_input_method; dumpsys power 2>/dev/null | grep -q mWakefulness=Awake && echo 1 || echo 0")
	lines := strings.Fields(strings.ReplaceAll(out, "\r", ""))
	if rc != 0 || len(lines) != 2 {
		return a.fail("couldn't read the phone's state")
	}
	prev, awake := lines[0], lines[1]
	// Already on the target keyboard (Rambler in stock Gboard, or a previous run died before
	// restoring): there is nothing to switch to or back from.
	if prev == a.cfg.Keyboard {
		prev = ""
	}
	if err := writeState(state{prev, awake, "starting"}); err != nil {
		return a.fail("couldn't write the state file: " + err.Error())
	}
	a.logf("prev ime=%s awake=%s", prev, awake)

	// The script takes up to ~8.5 s before giving up.
	if _, rc := a.phone(startPhone, 12*time.Second, "PREV", prev); rc != 0 {
		if err := a.adbFailed(rc, "phone disconnected while starting"); err != nil {
			return err
		}
		a.restore()
		switch rc {
		case 10:
			return a.fail("couldn't open the dictation field")
		case 11:
			return a.fail("the keyboard didn't come up")
		case 13:
			return a.fail("the Hearsay key presser is off. Turn it on in the phone's Accessibility settings")
		default:
			return a.fail("Rambler didn't start listening")
		}
	}
	writeState(state{prev, awake, "listening"})
	a.listening()
	return nil
}

func (a *app) listening() {
	a.logf("listening")
	a.status("listening", "")
	a.notify("Listening… press again to paste", 2000, false)
}

func (a adbTransport) stop(paste bool) error {
	st, ok := readState()
	if !ok {
		return a.fail("not listening")
	}
	if paste {
		a.status("busy", "Finishing…")
	} else {
		a.status("busy", "Cancelling…")
	}
	if err := a.requireDevice(); err != nil {
		return err
	}
	if st.phase != "listening" {
		// An earlier start was cut off (adb dropped); just put the phone back.
		if err := a.restore(); err != nil {
			return a.fail("phone disconnected before it was restored. Reconnect, then press again")
		}
		a.status("idle", "Phone restored")
		a.notify("Phone restored", 2000, false)
		return nil
	}
	key := "mic"
	if paste {
		a.notify("Finishing…", 2000, false)
	} else {
		key = "close"
	}

	text, rc := a.phone(stopPhone, (settleTimeoutMs+6000)*time.Millisecond,
		"KEY", key, "SETTLE", strconv.Itoa(settleMs), "TIMEOUT", strconv.Itoa(settleTimeoutMs))
	if rc != 0 {
		if err := a.adbFailed(rc, "phone disconnected"); err != nil {
			return err
		}
		a.restore()
		return a.fail("couldn't read text from phone")
	}
	text = strings.TrimRight(text, "\n")
	a.logf("got %d chars", utf8.RuneCountInString(text))

	// Paste before tidying up the phone; restore takes ~0.8 s and nothing on the PC waits on it.
	a.handOver(text, paste)
	if err := a.restore(); err != nil {
		return a.fail("phone disconnected before it was restored. Reconnect, then press again")
	}
	a.status("idle", "")
	return nil
}

var errUnreachable = errors.New("phone unreachable")

// restore clears the field and restores the keyboard and screen state the state file
// recorded. Safe to call twice. Keeps the state file if the phone is unreachable.
func (a *app) restore() error {
	st, ok := readState()
	if !ok {
		return nil
	}
	if !a.connected() {
		a.logf("restore skipped, phone unreachable")
		return errUnreachable
	}
	if _, rc := a.phone(restorePhone, 8*time.Second, "PREV", st.prev, "AWAKE", st.awake); rc != 0 {
		return errUnreachable
	}
	removeState()
	a.logf("restored ime=%s", st.prev)
	return nil
}

// handOver gives the user the text a stop brought back (or says why there is none).
func (a *app) handOver(text string, paste bool) {
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

var micBounds = regexp.MustCompile(`resource-id="[^"]*:id/key_pos_header_power_key"[^>]*bounds="\[(\d+),(\d+)\]\[(\d+),(\d+)\]"`)

// calibrate finds the mic key's position with a (slow) UI dump and saves it. Only needed
// while the accessibility service is off.
func (a *app) calibrate() error {
	if err := a.requireKeyboard(); err != nil {
		return err
	}
	if err := a.requireDevice(); err != nil {
		return err
	}
	out, _ := a.adb(8*time.Second, "shell", "settings get secure default_input_method")
	prev := strings.TrimSpace(strings.ReplaceAll(out, "\r", ""))
	if prev == a.cfg.Keyboard {
		prev = ""
	}
	writeState(state{prev, "1", "calibrating"})
	defer a.restore()
	if _, rc := a.phone(calibratePhone, 10*time.Second); rc != 0 {
		return a.fail("keyboard didn't come up")
	}
	fmt.Println("dumping UI (takes a few seconds)…")
	const dump = "/sdcard/hearsay-ui.xml"
	a.adb(30*time.Second, "shell", "uiautomator dump --windows "+dump+" >/dev/null")
	xml, _ := a.adb(10*time.Second, "exec-out", "cat "+dump)
	a.adb(5*time.Second, "shell", "rm -f "+dump)

	id := a.cfg.Keyboard[:strings.IndexByte(a.cfg.Keyboard+"/", '/')] + ":id/"
	for _, m := range micBounds.FindAllStringSubmatch(xml, -1) {
		if !strings.Contains(m[0], `resource-id="`+id) {
			continue
		}
		n := make([]int, 4)
		for i := range n {
			n[i], _ = strconv.Atoi(m[i+1])
		}
		a.cfg.Mic = &[2]int{(n[0] + n[2]) / 2, (n[1] + n[3]) / 2}
		if err := saveConfig(a.cfg); err != nil {
			return err
		}
		fmt.Printf("mic key at %d,%d (saved to %s)\n", a.cfg.Mic[0], a.cfg.Mic[1], configPath())
		return nil
	}
	return a.fail("mic key not found")
}
