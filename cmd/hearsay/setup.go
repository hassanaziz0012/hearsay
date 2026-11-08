package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The app, copied into apk/ before the release build (see apk/README.md). Builds without
// it can still set up a phone that has the app, or take --apk.
//
//go:embed apk
var apkDir embed.FS

const (
	service      = pkg + "/" + pkg + ".KeyPressService"
	serviceShort = pkg + "/.KeyPressService"
	gboardPkg    = "com.google.android.inputmethod.latin"
	latinIME     = "/com.android.inputmethod.latin.LatinIME" // class of Gboard and its builds
)

// setup walks a new user from nothing to a working dictation. Every step first checks
// whether it's already done, so it can be run again at any time. There are two ways to
// set up the phone: over adb, which does almost everything itself, or by hand (setup_net.go).
// Dictation goes over the network either way, so adb is only needed during setup.
func (a *app) setup(args []string) error {
	var apkFile string
	path := "" // "adb" or "manual"; asked when empty
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--apk" && i+1 < len(args):
			i++
			apkFile = args[i]
		case strings.HasPrefix(args[i], "--apk="):
			apkFile = strings.TrimPrefix(args[i], "--apk=")
		case args[i] == "--adb":
			path = "adb"
		case args[i] == "--no-adb":
			path = "manual"
		default:
			return fmt.Errorf("setup: unknown argument %q (usage: hearsay setup [--adb | --no-adb] [--apk FILE])", args[i])
		}
	}
	if path == "" {
		fmt.Println()
		options := []string{
			"With adb (USB or wireless debugging): setup installs the app and turns everything on\n" +
				"       for you. Needs Developer options on the phone; you can turn debugging off afterwards.",
			"Without adb: you install the app from GitHub and turn a few things on by hand,\n" +
				"       following the steps shown here.",
		}
		path = []string{"adb", "manual"}[choose("How should setup reach the phone?", options)]
	}
	type step struct {
		name string
		fn   func() error
	}
	var steps []step
	if path == "adb" {
		steps = []step{
			{"adb", a.setupAdb},
			{"Connect the phone", a.setupConnect},
			{"Hearsay app", func() error { return a.setupInstall(apkFile) }},
			{"Key presser (accessibility service)", func() error {
				if err := a.setupService(); err != nil {
					return err
				}
				return a.setupLocalNetwork()
			}},
			{"Keyboard", a.setupKeyboard},
			{"Pair with the phone", func() error { return a.setupPair(true, "") }},
		}
	} else {
		if apkFile != "" {
			return errors.New("setup: --apk needs adb (setup --adb); without adb, install the app on the phone yourself")
		}
		var up bool     // the phone answered on the network already
		var addr string // typed by the user if mDNS didn't find the phone
		steps = []step{
			{"Hearsay app", func() (err error) {
				up, err = a.setupInstallManual()
				return err
			}},
			{"Key presser (accessibility service)", func() (err error) {
				if up {
					done("on")
					return nil
				}
				addr, err = a.setupServiceManual()
				return err
			}},
			{"Pair with the phone", func() error { return a.setupPair(false, addr) }},
			{"Keyboard", a.setupKeyboardNet},
		}
	}
	steps = append(steps,
		step{"Test", a.setupTest},
		step{"Shortcut and start at login", a.setupDesktop})
	for i, s := range steps {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(steps), s.name)
		if err := s.fn(); err != nil {
			return err
		}
	}
	fmt.Println("\nAll set.")
	if path == "adb" {
		fmt.Println("Dictation doesn't use adb, so you can turn USB or wireless debugging off again.")
	}
	return nil
}

func done(format string, args ...any) { fmt.Printf("  ✓ "+format+"\n", args...) }
func say(format string, args ...any)  { fmt.Printf("  "+format+"\n", args...) }

// stdin is read by one goroutine for the whole run, so a prompt can also wait on other
// things (the phone showing up) while the user may type.
var stdinLines = sync.OnceValue(func() <-chan string {
	ch := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			ch <- strings.TrimSpace(sc.Text())
		}
		close(ch)
	}()
	return ch
})

func readLine() string {
	l, ok := <-stdinLines()
	if !ok {
		fmt.Println("\nhearsay: setup needs answers on the terminal")
		os.Exit(1)
	}
	return l
}

func ask(q string) string {
	fmt.Print("  " + q + " ")
	return readLine()
}

func yes(q string, def bool) bool {
	hint := " [Y/n]"
	if !def {
		hint = " [y/N]"
	}
	for {
		switch strings.ToLower(ask(q + hint)) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

// choose asks for one of the options by number and returns its index.
func choose(q string, options []string) int {
	say(q)
	for i, o := range options {
		say("  %d) %s", i+1, o)
	}
	for {
		n, err := strconv.Atoi(ask(fmt.Sprintf("Number (1-%d):", len(options))))
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
	}
}

// shell runs a command on the phone and returns its output without CRs and trailing space.
func (a *app) shell(timeout time.Duration, cmd string) (string, int) {
	out, rc := a.adb(timeout, "shell", cmd)
	return strings.TrimSpace(strings.ReplaceAll(out, "\r", "")), rc
}

// 1. adb

func (a *app) setupAdb() error {
	if a.adbPath != "" {
		if out, rc := a.adbAny(nil, 15*time.Second, "version"); rc == 0 {
			v, _, _ := strings.Cut(out, "\n")
			done("%s (%s)", a.adbPath, strings.TrimSpace(v))
			return nil
		}
		say("%s doesn't run.", a.adbPath)
	} else {
		say("adb (Android's debugging tool, which Hearsay drives the phone with) isn't installed.")
	}
	say("Hearsay can download Google's Android SDK Platform-Tools into %s.", filepath.Join(dataDir(), "platform-tools"))
	say("By downloading them you agree to the Android SDK terms: %s", sdkTerms)
	if !yes("Download them?", true) {
		return errors.New("setup needs adb. Install Android platform-tools, then run setup again")
	}
	p, err := downloadPlatformTools()
	if err != nil {
		return err
	}
	a.adbPath = p
	done("installed %s", p)
	return nil
}

// 2. Connect

type device struct{ serial, state string }

func (a *app) devices() []device {
	out, _ := a.adbAny(nil, 10*time.Second, "devices")
	var ds []device
	for _, l := range strings.Split(out, "\n")[1:] {
		if f := strings.Fields(l); len(f) >= 2 {
			ds = append(ds, device{f[0], f[1]})
		}
	}
	return ds
}

type mdnsService struct{ name, kind, addr string }

// mdns lists the phones adb sees on the network: kind "pairing" while the phone shows the
// pairing code dialog, "connect" while Wireless debugging is on.
func (a *app) mdns() []mdnsService {
	out, _ := a.adbAny(nil, 5*time.Second, "mdns", "services")
	var ss []mdnsService
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 3 {
			continue
		}
		switch {
		case strings.HasPrefix(f[1], "_adb-tls-pairing."):
			ss = append(ss, mdnsService{f[0], "pairing", f[2]})
		case strings.HasPrefix(f[1], "_adb-tls-connect."):
			ss = append(ss, mdnsService{f[0], "connect", f[2]})
		}
	}
	return ss
}

// pickDevice sets a.serial from the connected devices and reports whether there is one.
func (a *app) pickDevice() bool {
	var online []string
	for _, d := range a.devices() {
		if d.state == "device" {
			online = append(online, d.serial)
		}
	}
	switch {
	case len(online) == 0:
		return false
	case a.serial != "" && slices.Contains(online, a.serial):
	case len(online) == 1:
		a.serial = online[0]
		a.cfg.Serial = "" // the only device needs no serial
	default:
		a.serial = online[choose("Several devices are connected. Which one is the phone with Rambler?", online)]
		a.cfg.Serial = a.serial
	}
	return true
}

// connectKnown tries the phones adb can see on the network (already paired ones connect).
func (a *app) connectKnown(ip string) {
	for _, s := range a.mdns() {
		if s.kind == "connect" && (ip == "" || strings.HasPrefix(s.addr, ip+":")) {
			a.adbAny(nil, 10*time.Second, "connect", s.addr)
		}
	}
}

func (a *app) setupConnect() error {
	if !a.pickDevice() {
		for _, d := range a.devices() {
			if d.state == "unauthorized" {
				say("%s is waiting: allow USB debugging in the prompt on the phone.", d.serial)
			}
		}
		// Recent adb reconnects paired phones by itself within a few seconds.
		say("Looking for the phone…")
		a.adbAny(nil, 5*time.Second, "wait-for-device")
		if !a.pickDevice() {
			a.connectKnown("")
		}
		if !a.pickDevice() {
			if err := a.adbPair(); err != nil {
				return err
			}
		}
	}
	model, _ := a.shell(5*time.Second, "getprop ro.product.model")
	sdkOut, _ := a.shell(5*time.Second, "getprop ro.build.version.sdk")
	release, _ := a.shell(5*time.Second, "getprop ro.build.version.release")
	if sdk, err := strconv.Atoi(sdkOut); err == nil && sdk < 34 {
		return fmt.Errorf("%s runs Android %s; Hearsay needs Android 14 or newer", model, release)
	}
	done("%s, Android %s (%s)", model, release, a.serial)
	return saveConfig(a.cfg)
}

// adbPair walks through Wireless debugging pairing, finding the phone with mDNS so the user
// only types the code.
func (a *app) adbPair() error {
	say("Connect the phone over Wi-Fi. It must be on the same network as this computer.")
	say("  1. On the phone, open Settings > About phone and tap Build number 7 times")
	say("     (this turns on Developer options; skip if done).")
	say("  2. Settings > System > Developer options > Wireless debugging: turn it on.")
	say("  3. Tap Wireless debugging, then Pair device with pairing code.")
	say("Waiting for the phone… (or type the IP address & port the phone shows,")
	say("or \"usb\" to use a USB cable instead)")
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var addr string
	for addr == "" {
		select {
		case l, ok := <-stdinLines():
			switch {
			case !ok:
				return errors.New("setup needs answers on the terminal")
			case strings.EqualFold(l, "usb"):
				return a.connectUSB()
			case strings.Contains(l, ":"):
				addr = l
			case l != "":
				say("That isn't an IP address & port (like 192.168.1.20:37123).")
			}
		case <-tick.C:
			if a.pickDevice() {
				return nil
			}
			for _, s := range a.mdns() {
				if s.kind == "pairing" {
					addr = s.addr
					say("Found the phone at %s.", addr)
				}
			}
		}
	}
	for {
		code := ask("Pairing code shown on the phone:")
		var errb bytes.Buffer
		out, _ := a.adbAny(&errb, 30*time.Second, "pair", addr, code)
		if strings.Contains(out, "Successfully paired") {
			break
		}
		say("Pairing failed: %s", strings.TrimSpace(out+" "+errb.String()))
		if !yes("Try again? (open the pairing dialog again if it closed)", true) {
			return errors.New("phone not paired")
		}
	}
	say("Paired. Connecting…")
	ip, _, _ := strings.Cut(addr, ":")
	for range 20 {
		if a.pickDevice() {
			return nil
		}
		a.connectKnown(ip)
		time.Sleep(time.Second)
	}
	return errors.New("paired, but the phone didn't connect. Check that Wireless debugging is on, then run setup again")
}

func (a *app) connectUSB() error {
	say("  1. Settings > System > Developer options > USB debugging: turn it on.")
	say("  2. Plug the phone in and allow USB debugging in the prompt on the phone.")
	if runtime.GOOS == "windows" {
		say("  Windows needs Google's USB driver for this:")
		say("  https://developer.android.com/studio/run/win-usb")
	}
	for {
		ask("Press Enter when it's plugged in.")
		if a.pickDevice() {
			return nil
		}
		for _, d := range a.devices() {
			say("%s: %s", d.serial, d.state)
		}
		say("No phone ready yet.")
	}
}

// 3. App

func bundledAPK(file string) ([]byte, string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		return b, file, err
	}
	b, err := apkDir.ReadFile("apk/hearsay.apk")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	return b, "the bundled app", err
}

func (a *app) setupInstall(apkFile string) error {
	apk, src, err := bundledAPK(apkFile)
	if err != nil {
		return err
	}
	out, _ := a.shell(15*time.Second, "p=$(pm path "+pkg+" 2>/dev/null); p=${p#package:}; [ -n \"$p\" ] && sha256sum \"$p\"")
	onPhone, _, _ := strings.Cut(out, " ")
	if apk == nil {
		if onPhone != "" {
			done("installed (this build of hearsay has no app bundled to compare with)")
			return nil
		}
		return errors.New("the Hearsay app isn't on the phone and this build has none bundled. Pass --apk FILE")
	}
	sum := sha256.Sum256(apk)
	if onPhone == hex.EncodeToString(sum[:]) {
		done("installed, same version as %s", src)
		return nil
	}
	tmp, err := os.CreateTemp("", "hearsay-*.apk")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(apk)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	install := func() string {
		var errb bytes.Buffer
		out, _ := a.adbTo(&errb, 2*time.Minute, "install", "-r", tmp.Name())
		return out + errb.String()
	}
	if onPhone == "" {
		say("Installing %s…", src)
	} else {
		say("Updating the app on the phone from %s…", src)
	}
	res := install()
	switch {
	case strings.Contains(res, "Success"):
	case strings.Contains(res, "INSTALL_FAILED_VERSION_DOWNGRADE"):
		done("the phone has a newer version of the app; keeping it")
		return nil
	case strings.Contains(res, "INSTALL_FAILED_UPDATE_INCOMPATIBLE"):
		say("The Hearsay app on the phone was signed by someone else (another build).")
		if !yes("Uninstall it and install this one?", true) {
			return errors.New("the app on the phone can't be updated")
		}
		a.adb(30*time.Second, "uninstall", pkg)
		if res = install(); !strings.Contains(res, "Success") {
			return fmt.Errorf("install failed: %s", strings.TrimSpace(res))
		}
	default:
		return fmt.Errorf("install failed: %s", strings.TrimSpace(res))
	}
	done("installed")
	return nil
}

// 4. Service

// serviceOn reports whether the system has the key presser running (bound).
func (a *app) serviceOn() bool {
	out, _ := a.shell(5*time.Second, "dumpsys activity services "+pkg+"/.KeyPressService")
	return strings.Contains(out, "ServiceRecord{")
}

func (a *app) waitService(d time.Duration) bool {
	for end := time.Now().Add(d); ; time.Sleep(500 * time.Millisecond) {
		if a.serviceOn() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

func (a *app) setupService() error {
	list, _ := a.shell(5*time.Second, "settings get secure enabled_accessibility_services")
	if list == "null" {
		list = ""
	}
	entries := strings.Split(list, ":")
	listed := slices.Contains(entries, service) || slices.Contains(entries, serviceShort)
	// Right after an install the service takes a few seconds to come back.
	if a.serviceOn() || (listed && a.waitService(4*time.Second)) {
		done("on")
		return nil
	}
	if !listed {
		// Append: other apps' services are in the same list.
		if list != "" {
			list += ":"
		}
		list += service
		say("Turning on the Hearsay key presser…")
		a.shell(5*time.Second, "settings put secure enabled_accessibility_services "+shellQuote(list)+
			" && settings put secure accessibility_enabled 1")
		if a.waitService(6 * time.Second) {
			done("on")
			return nil
		}
	}
	say("The key presser didn't come on. On the phone, open Settings > Accessibility >")
	say("Hearsay key presser and turn it on.")
	for {
		ask("Press Enter when it's on.")
		if a.waitService(3 * time.Second) {
			done("on")
			return nil
		}
		say("It's still off.")
	}
}

// setupLocalNetwork allows the app on the local network (Android 17 asks the user for it,
// adb can grant it), so the computer can reach the phone without adb.
func (a *app) setupLocalNetwork() error {
	const perm = "android.permission.ACCESS_LOCAL_NETWORK"
	out, _ := a.shell(5*time.Second, "getprop ro.build.version.sdk")
	if sdk, _ := strconv.Atoi(out); sdk < 37 {
		return nil // no such permission before Android 17
	}
	if _, rc := a.shell(10*time.Second, "pm grant "+pkg+" "+perm); rc != 0 {
		say("Couldn't allow the app on the local network from here. Open the Hearsay app on")
		say("the phone and tap Allow next to Local network.")
		return nil
	}
	done("allowed on the local network")
	return nil
}

// 5. Keyboard

func keyboardLabel(ime string) string {
	p, _, _ := strings.Cut(ime, "/")
	if p == gboardPkg {
		return "Gboard"
	}
	return p
}

func (a *app) setupKeyboard() error {
	all, _ := a.shell(10*time.Second, "ime list -a -s")
	enabled, _ := a.shell(10*time.Second, "ime list -s")
	isEnabled := func(ime string) bool { return slices.Contains(strings.Fields(enabled), ime) }
	kb, err := a.pickKeyboard(strings.Fields(all), isEnabled, keyboardLabel)
	if err != nil {
		return err
	}
	if !isEnabled(kb) {
		say("%s isn't enabled as a keyboard.", keyboardLabel(kb))
		if !yes("Enable it?", true) {
			return errors.New("the keyboard with Rambler must be enabled")
		}
		if _, rc := a.shell(5*time.Second, "ime enable "+shellQuote(kb)); rc != 0 {
			return errors.New("couldn't enable it. Enable it in the phone's keyboard settings")
		}
	}
	a.cfg.Keyboard = kb
	if err := saveConfig(a.cfg); err != nil {
		return err
	}
	done("%s", keyboardLabel(kb))
	if err := a.setupMic(kb); err != nil {
		return err
	}
	ramblerHint(kb)
	return nil
}

func ramblerHint(kb string) {
	if strings.HasPrefix(kb, gboardPkg+"/") {
		say("In Gboard's voice typing settings, choose Rambler rather than regular voice")
		say("typing: Hearsay presses the mic key, which starts whichever one is chosen.")
	}
}

// pickKeyboard picks the keyboard with Rambler among the installed ones (IME ids): the
// config's if the user keeps it, else the only Gboard, else the user's choice.
func (a *app) pickKeyboard(all []string, isEnabled func(string) bool, label func(string) string) (string, error) {
	gboards := gboards(all)
	kb := a.cfg.Keyboard
	switch {
	case kb != "" && slices.Contains(all, kb) &&
		yes(fmt.Sprintf("Dictate with %s (from your config)?", label(kb)), true):
		return kb, nil
	case len(gboards) == 0:
		return "", errors.New("no Gboard on the phone. Install Gboard (or a Gboard build with Rambler) and run setup again")
	case len(gboards) == 1:
		return gboards[0], nil
	}
	labels := make([]string, len(gboards))
	for i, g := range gboards {
		labels[i] = label(g)
		if !isEnabled(g) {
			labels[i] += " (not enabled)"
		}
	}
	return gboards[choose("Which keyboard has Rambler?", labels)], nil
}

// gboards are the keyboards (IME ids) that may have Rambler: Gboard and its builds.
func gboards(all []string) []string {
	var gs []string
	for _, ime := range all {
		if strings.HasSuffix(ime, latinIME) {
			gs = append(gs, ime)
		}
	}
	return gs
}

// setupMic makes sure the keyboard may use the mic. Without it Gboard answers the mic key
// with "No permission to enable: Voice typing", and it can't ask for the permission when
// the key is pressed for it.
func (a *app) setupMic(kb string) error {
	p, _, _ := strings.Cut(kb, "/")
	const perm = "android.permission.RECORD_AUDIO"
	granted := func() bool {
		out, _ := a.shell(10*time.Second, "dumpsys package "+shellQuote(p)+" 2>/dev/null | grep -m1 "+shellQuote(perm+": granted="))
		return strings.Contains(out, "granted=true")
	}
	if granted() {
		done("%s may use the mic", keyboardLabel(kb))
		return nil
	}
	if _, rc := a.shell(10*time.Second, "pm grant "+shellQuote(p)+" "+perm); rc != 0 || !granted() {
		return fmt.Errorf("%s isn't allowed to use the mic and it couldn't be allowed from here. "+
			"On the phone, open Settings > Apps > %[1]s > Permissions > Microphone, allow it and run setup again",
			keyboardLabel(kb))
	}
	done("allowed %s to use the mic", keyboardLabel(kb))
	return nil
}

// 6. Test

func (a *app) setupTest() error {
	if !yes("Try a dictation now?", true) {
		return nil
	}
	if err := a.testStart(); err != nil {
		say("The test didn't start (see above). Fix that, then run setup again.")
		return nil
	}
	ask("Rambler is listening. Say something to the phone, then press Enter here.")
	fmt.Println()
	text, err := a.testStop()
	switch {
	case err != nil:
		say("The test failed (see above).")
	case strings.TrimSpace(text) == "":
		say("Rambler heard nothing. Check that Rambler (not regular voice typing) is chosen")
		say("in the keyboard's voice typing settings, then try again with hearsay toggle.")
	default:
		fmt.Println()
		done("that's what Rambler heard (also copied to the clipboard)")
	}
	return nil
}

// testStart starts a test dictation; testStop ends it and returns what Rambler heard,
// copied to the clipboard but not pasted (the focus is on setup, not on a text field).
func (a *app) testStart() error { return a.action(a.start) }

func (a *app) testStop() (string, error) {
	var text string
	err := a.action(func() error {
		paste := a.paste
		a.paste = false
		defer func() { a.paste = paste }()
		a.heard = ""
		err := a.stop(true)
		text = a.heard
		return err
	})
	return text, err
}

// 7. Shortcut and start at login

// trayName is what the tray icon is called on this system.
func trayName() string {
	if runtime.GOOS == "darwin" {
		return "menu bar icon"
	}
	return "tray icon"
}

// setupAutostart offers to start the tray at login.
func (a *app) setupAutostart(exe string) error {
	on, other := autostartState(exe)
	switch {
	case on:
		done("the %s starts at login", trayName())
		return nil
	case other && !yes("Another hearsay starts at login. Point it at this one ("+exe+")?", true),
		!other && !yes("Start the Hearsay "+trayName()+" at login?", true):
		return nil
	}
	if err := setAutostart(exe, true); err != nil {
		return err
	}
	done("the %s starts at login", trayName())
	return nil
}

func (a *app) setupDesktop() error {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	if strings.Contains(exe, "go-build") {
		say("This is a `go run` binary; build hearsay first so the shortcut has a stable path.")
		return nil
	}
	if wayland() {
		if err := a.setupDesktopShortcut(exe); err != nil {
			return err
		}
	} else {
		hk := a.cfg.Hotkey
		if hk == "" {
			hk = defaultHotkey()
		}
		if hk == "none" {
			done("no global hotkey (\"hotkey\": \"none\" in %s)", configPath())
		} else {
			done("%s starts and stops dictation while `hearsay run` is running", hk)
			say("(change it with \"hotkey\" in %s)", configPath())
		}
	}
	if runtime.GOOS == "darwin" {
		say("To paste, macOS asks once for the Accessibility permission for hearsay (or the")
		say("terminal running it): System Settings > Privacy & Security > Accessibility.")
	}
	if err := a.setupAutostart(exe); err != nil {
		return err
	}
	// Start it now even if it won't start at login: the hotkey only works while it runs.
	if trayRunning() {
		done("the tray icon is running (quit and start it again to pick up changes)")
		return nil
	}
	return startDetached(exe, "run")
}
