package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"rsc.io/qr"
)

// The setup steps without adb: the user installs the app and turns things on by hand,
// and everything setup checks, it asks the app over the network (PhoneServer).

// releaseAPK always points at the newest release's app.
const releaseAPK = "https://github.com/hassanaziz0012/hearsay/releases/latest/download/hearsay.apk"

// phoneKeyboard is one entry of the app's "keyboards" reply.
type phoneKeyboard struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Mic     bool   `json:"mic"`
}

// 1. App

// phoneUp returns the name of a phone with the app and its key presser on, if one answers
// on the network: the paired one at its address, else any that mDNS finds.
func (a *app) phoneUp() string {
	if a.cfg.Phone != nil && a.pingPhone() == nil {
		return a.cfg.Phone.Name
	}
	// A dozing phone often misses the first queries (browse asks every second).
	if found := browse(5*time.Second, nil); len(found) > 0 {
		return found[0].name
	}
	return ""
}

// setupInstallManual reports whether the phone already answers on the network, which
// means the next step is done too.
func (a *app) setupInstallManual() (bool, error) {
	// The app answers on the network only once its key presser is on.
	if name := a.phoneUp(); name != "" {
		done("installed (%s answers on the network)", name)
		return true, nil
	}
	say("Install the Hearsay app on the phone (skip this if it's installed already):")
	say("  1. Turn Play Protect off for now. It blocks apps it doesn't know, with no way")
	say("     past it: open the Play Store, tap your profile picture > Play Protect > ⚙ and")
	say("     turn off Scan apps with Play Protect.")
	say("  2. Open this link on the phone (or scan the code) and download the app:")
	say("     %s", releaseAPK)
	printQR(releaseAPK)
	say("  3. Open the downloaded file and tap Install (allow your browser to install apps")
	say("     if the phone asks).")
	say("  4. Turn Play Protect back on.")
	ask("Press Enter when the app is installed.")
	done("installed")
	return false, nil
}

// 2. Key presser

// setupServiceManual guides the user through turning on the key presser and waits until
// the phone shows up on the network. It returns the address the user typed, if mDNS never
// found the phone ("" otherwise).
func (a *app) setupServiceManual() (string, error) {
	say("  1. Open the Hearsay app and tap Open settings next to Key presser. Turn on")
	say("     Hearsay key presser.")
	say("  2. If Android says it's a restricted setting: go back to the app, tap App info,")
	say("     then ⋮ at the top > Allow restricted settings, and try step 1 again.")
	say("  3. On Android 17, allow the app on the local network when it asks (or tap Allow")
	say("     next to Local network in the app).")
	say("The phone must be on the same Wi-Fi as this computer.")
	say("Waiting for the phone… (if it never shows up, type the address the app shows")
	say("under This phone)")

	found := make(chan []phoneService, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if f := browse(2*time.Second, nil); len(f) > 0 {
				found <- f
				return
			}
		}
	}()
	for {
		select {
		case f := <-found:
			done("on (%s found on the network)", f[0].name)
			return "", nil
		case l, ok := <-stdinLines():
			switch {
			case !ok:
				return "", errors.New("setup needs answers on the terminal")
			case l == "":
			default:
				addr := l
				if _, _, err := net.SplitHostPort(addr); err != nil {
					addr = net.JoinHostPort(addr, defaultPort)
				}
				if host, _, _ := net.SplitHostPort(addr); net.ParseIP(host) == nil {
					say("That isn't an address like 192.168.1.20.")
					continue
				}
				c, err := net.DialTimeout("tcp", addr, 3*time.Second)
				if err != nil {
					say("Nothing answers at %s (%v). Is the key presser on? Try again, or keep waiting.", addr, err)
					continue
				}
				c.Close()
				done("on (the phone answers at %s)", addr)
				return addr, nil
			}
		}
	}
}

// 3. Pair (both paths)

// setupPair pairs with the phone for dictation over the network, unless it already is,
// and switches dictation to it. With adb, setup opens the app on the phone itself and
// knows the phone's address in case mDNS doesn't find it.
func (a *app) setupPair(viaAdb bool, addr string) error {
	if p := a.cfg.Phone; p != nil {
		err := a.pingPhone()
		if err == nil {
			done("paired with %s", p.Name)
			return a.useNet()
		}
		say("Paired with %s before, but that doesn't work now (%v). Pairing again.", p.Name, err)
	}
	guess := ""
	if viaAdb {
		a.shell(5*time.Second, "input keyevent KEYCODE_WAKEUP; am start -n "+pkg+"/.HomeActivity")
		say("Setup opened the Hearsay app on the phone. Unlock the phone if it's locked.")
		guess = a.phoneIP()
	}
	if err := a.pairNet(addr, guess); err != nil {
		return err
	}
	return a.useNet()
}

// pingPhone checks that the paired phone answers and still knows this computer.
func (a *app) pingPhone() error {
	conn, err := a.dialPhone()
	if err != nil {
		return err
	}
	defer conn.Close()
	r, err := exchange(conn, map[string]string{"op": "status"}, 5*time.Second)
	if err != nil {
		return err
	}
	if !r.OK {
		return errors.New(r.Msg)
	}
	return nil
}

func (a *app) useNet() error {
	if a.cfg.Transport == "net" {
		return nil
	}
	a.cfg.Transport = "net"
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	done("dictation goes over the network from now on, without adb")
	return nil
}

// phoneIP is the phone's Wi-Fi address, asked over adb ("" if unknown).
func (a *app) phoneIP() string {
	out, _ := a.shell(5*time.Second, "ip -4 -o addr show wlan0")
	f := strings.Fields(out)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "inet" {
			ip, _, _ := strings.Cut(f[i+1], "/")
			return ip
		}
	}
	return ""
}

// 4. Keyboard

func (a *app) phoneKeyboards() ([]phoneKeyboard, error) {
	conn, err := a.dialPhone()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	r, err := exchange(conn, map[string]string{"op": "keyboards"}, 5*time.Second)
	switch {
	case err != nil:
		return nil, fmt.Errorf("asking the phone for its keyboards: %v", err)
	case r.Error == "bad_request":
		return nil, errors.New("the Hearsay app on the phone is too old for this. Update it and run setup again")
	case !r.OK:
		return nil, errors.New(r.Msg)
	}
	return r.Keyboards, nil
}

func (a *app) setupKeyboardNet() error {
	kbs, err := a.phoneKeyboards()
	if err != nil {
		return err
	}
	find := func(id string) phoneKeyboard {
		for _, k := range kbs {
			if k.ID == id {
				return k
			}
		}
		return phoneKeyboard{}
	}
	ids := make([]string, len(kbs))
	for i, k := range kbs {
		ids[i] = k.ID
	}
	label := func(id string) string {
		if l := find(id).Label; l != "" {
			return l
		}
		return keyboardLabel(id)
	}
	kb, err := a.pickKeyboard(ids, func(id string) bool { return find(id).Enabled }, label)
	if err != nil {
		return err
	}
	a.cfg.Keyboard = kb
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	name := label(kb)
	// Without adb, the user changes these on the phone; ask again after each try.
	recheck := func(ok func(phoneKeyboard) bool, help ...string) error {
		for !ok(find(kb)) {
			for _, h := range help {
				say("%s", h)
			}
			ask("Press Enter when it's done.")
			if kbs, err = a.phoneKeyboards(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := recheck(func(k phoneKeyboard) bool { return k.Enabled },
		name+" isn't turned on as a keyboard. On the phone, open Settings > System > Keyboard >",
		"On-screen keyboard (Manage on-screen keyboards) and turn on "+name+"."); err != nil {
		return err
	}
	done("%s", name)
	if err := recheck(func(k phoneKeyboard) bool { return k.Mic },
		name+" isn't allowed to use the mic. On the phone, open Settings > Apps > "+name+" >",
		"Permissions > Microphone and allow it."); err != nil {
		return err
	}
	done("%s may use the mic", name)
	ramblerHint(kb)
	return nil
}

// printQR shows text as a QR code on the terminal, two modules per character cell, dark on
// a light background (forced with ANSI colours, so a dark terminal theme doesn't invert it).
func printQR(text string) {
	if isTerminal(os.Stdout) {
		fmt.Print(qrBlocks(text))
	}
}

func qrBlocks(text string) string {
	c, err := qr.Encode(text, qr.L)
	if err != nil {
		return ""
	}
	const quiet = 2
	black := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < c.Size+quiet; y += 2 {
		b.WriteString("     \x1b[30;47m")
		for x := -quiet; x < c.Size+quiet; x++ {
			switch top, bottom := black(x, y), black(x, y+1); {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}
