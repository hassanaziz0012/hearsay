package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"rsc.io/qr"
)

// API is what the window's front end calls (main.API.<Method> in /wails/runtime.js).
// Errors come back as sentences to show as they are.
type API struct{ g *gui }

// State is everything the window shows about the setup.
type State struct {
	OS               string     `json:"os"`
	Wayland          bool       `json:"wayland"`
	DesktopShortcuts bool       `json:"desktopShortcuts"` // Hearsay can set the desktop's shortcut
	Phone            *PhoneInfo `json:"phone"`
	Keyboard         string     `json:"keyboard"`
	KeyboardID       string     `json:"keyboardId"`
	Hotkey           string     `json:"hotkey"` // spec, "none" for no hotkey
	HotkeyName       string     `json:"hotkeyName"`
	HotkeyLine       string     `json:"hotkeyLine"`
	HotkeyError      string     `json:"hotkeyError"`
	Paste            bool       `json:"paste"`
	PasteForcedOff   bool       `json:"pasteForcedOff"` // HEARSAY_PASTE=0
	Autostart        bool       `json:"autostart"`
	AutostartOther   bool       `json:"autostartOther"` // another hearsay starts at login
	Status           Status     `json:"status"`
	ConfigPath       string     `json:"configPath"`
	LogPath          string     `json:"logPath"`
	ReleaseURL       string     `json:"releaseUrl"`
	ToggleCommand    string     `json:"toggleCommand"` // for a desktop shortcut
	Version          string     `json:"version"`
}

type PhoneInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Status is the last press's state, as the tray shows it.
type Status struct {
	State     string `json:"state"` // idle, busy, listening, error, retry
	Label     string `json:"label"`
	Detail    string `json:"detail"`
	LastError string `json:"lastError"` // in plain language; "" if the last press worked
}

type KeyboardInfo struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Enabled  bool   `json:"enabled"`
	Mic      bool   `json:"mic"`
	Selected bool   `json:"selected"`
}

func (api *API) State() State {
	g, a := api.g, api.g.a
	exe, _ := executable()
	on, other := autostartState(exe)
	spec := a.cfg.Hotkey
	if spec == "" {
		spec = defaultHotkey()
	}
	if wayland() {
		if bind := desktopShortcut(); bind != "" {
			spec = bind
		}
	}
	g.mu.Lock()
	s := State{
		OS:               runtime.GOOS,
		Wayland:          wayland(),
		DesktopShortcuts: gnomeShortcuts(),
		KeyboardID:       a.cfg.Keyboard,
		Hotkey:           spec,
		HotkeyLine:       g.hotkeyLine,
		Paste:            a.paste,
		PasteForcedOff:   os.Getenv("HEARSAY_PASTE") == "0",
		Autostart:        on,
		AutostartOther:   other,
		Status:           currentStatus(),
		ConfigPath:       configPath(),
		LogPath:          runFile("hearsay.log"),
		ReleaseURL:       releaseAPK,
		ToggleCommand:    quoteArg(exe) + " toggle",
		Version:          version,
	}
	if g.hotkeyErr != nil {
		s.HotkeyError = g.hotkeyErr.Error()
	}
	g.mu.Unlock()
	if spec != "none" {
		s.HotkeyName = hotkeyName(spec)
	}
	if p := a.cfg.Phone; p != nil {
		s.Phone = &PhoneInfo{p.Name, p.Addr}
	}
	if a.cfg.Keyboard != "" {
		s.Keyboard = keyboardLabel(a.cfg.Keyboard)
	}
	return s
}

func currentStatus() Status {
	st, msg := trayDisplay()
	s := Status{State: st, Label: trayStates[st].label, Detail: msg}
	if st == "busy" && msg != "" {
		s.Label, s.Detail = msg, ""
	}
	if raw, rawMsg, _ := readStatus(); raw == "error" || raw == "retry" {
		s.LastError = plainError(rawMsg)
		if st == "idle" {
			s.Detail = ""
		}
	}
	return s
}

// plainError rewords the network errors people can't act on.
func plainError(msg string) string {
	switch {
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "deadline exceeded"):
		return "The phone didn't answer in time. Check that it's on and on the same Wi-Fi as this computer."
	case strings.Contains(msg, "connection refused"):
		return "The phone refused the connection. Is the Hearsay key presser on?"
	case strings.Contains(msg, "no route to host"), strings.Contains(msg, "network is unreachable"):
		return "This computer can't reach the phone. Are both on the same Wi-Fi?"
	case strings.Contains(msg, "certificate has changed"):
		return "The phone's identity has changed (the app was reinstalled?). Pair again under Troubleshoot."
	}
	if msg != "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	return msg
}

// executable is this program's path, for autostart and the desktop shortcut.
func executable() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	return exe, err
}

// Setup: install the app

// InstallQR is the app's download link as a QR code: an SVG path of 1-unit squares on a
// grid of the returned size (quiet zone included).
func (api *API) InstallQR() (qrCode, error) {
	c, err := qr.Encode(releaseAPK, qr.M)
	if err != nil {
		return qrCode{}, err
	}
	const quiet = 2
	var b strings.Builder
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return qrCode{Size: c.Size + 2*quiet, Path: b.String()}, nil
}

type qrCode struct {
	Size int    `json:"size"`
	Path string `json:"path"`
}

// Setup: key presser and pairing

// FindPhones looks for phones with the Hearsay app's key presser on, for up to seconds.
func (api *API) FindPhones(seconds int) []PhoneInfo {
	found := []PhoneInfo{}
	for _, s := range browse(time.Duration(seconds)*time.Second, nil) {
		found = append(found, PhoneInfo{s.name, s.addr})
	}
	return found
}

// CheckAddress checks that something answers at the address the phone's app shows.
func (api *API) CheckAddress(addr string) (PhoneInfo, error) {
	addr = withPort(strings.TrimSpace(addr))
	if host, _, _ := net.SplitHostPort(addr); net.ParseIP(host) == nil {
		return PhoneInfo{}, errors.New("that isn't an address like 192.168.1.20")
	}
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return PhoneInfo{}, fmt.Errorf("nothing answers at %s. Is the key presser on, and the phone on the same Wi-Fi?", addr)
	}
	c.Close()
	return PhoneInfo{"phone", addr}, nil
}

// PairStart asks the phone to pair and returns the code both sides show ("123 456").
// It waits up to a minute for the app to be on screen (emitting "pair:waiting").
func (api *API) PairStart(phone PhoneInfo) (string, error) {
	g := api.g
	api.PairCancel()
	ctx, cancel := context.WithCancel(context.Background())
	g.mu.Lock()
	g.pairCancel = cancel
	g.mu.Unlock()
	p, err := g.a.pairBegin(ctx, phone.Addr, phone.Name, func() { g.wails.Event.Emit("pair:waiting") })
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", errors.New("cancelled")
		}
		return "", err
	}
	g.mu.Lock()
	g.pairing = p
	g.mu.Unlock()
	return p.code[:3] + " " + p.code[3:], nil
}

// PairFinish waits until the user taps Pair on the phone, then saves the phone.
func (api *API) PairFinish() error {
	g := api.g
	g.mu.Lock()
	p := g.pairing
	g.mu.Unlock()
	if p == nil {
		return errors.New("no pairing in progress")
	}
	defer api.PairCancel()
	if err := p.finish(); err != nil {
		return err
	}
	g.wails.Event.Emit("state")
	return nil
}

func (api *API) PairCancel() {
	g := api.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pairCancel != nil {
		g.pairCancel()
		g.pairCancel = nil
	}
	if g.pairing != nil {
		g.pairing.close()
		g.pairing = nil
	}
}

// Setup: keyboard

// Keyboards lists the phone's keyboards that may have Rambler (Gboard and its builds).
func (api *API) Keyboards() ([]KeyboardInfo, error) {
	a := api.g.a
	kbs, err := a.phoneKeyboards()
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(kbs))
	for i, k := range kbs {
		ids[i] = k.ID
	}
	ok := gboards(ids)
	list := []KeyboardInfo{}
	for _, k := range kbs {
		if !slices.Contains(ok, k.ID) {
			continue
		}
		label := k.Label
		if label == "" {
			label = keyboardLabel(k.ID)
		}
		list = append(list, KeyboardInfo{k.ID, label, k.Enabled, k.Mic, k.ID == a.cfg.Keyboard})
	}
	if len(list) == 0 {
		return nil, errors.New("there's no Gboard on the phone. Install Gboard from the Play Store, then check again")
	}
	return list, nil
}

func (api *API) SetKeyboard(id string) error {
	return api.g.updateConfig(func(c *config) { c.Keyboard = id })
}

// updateConfig changes the config and saves it, between presses.
func (g *gui) updateConfig(change func(*config)) error {
	g.a.mu.Lock()
	defer g.a.mu.Unlock()
	change(&g.a.cfg)
	if err := saveConfig(g.a.cfg); err != nil {
		return err
	}
	g.wails.Event.Emit("state")
	return nil
}

// Setup: test

func (api *API) TestStart() error { return api.g.a.testStart() }

// TestStop ends the test and returns what Rambler heard.
func (api *API) TestStop() (string, error) { return api.g.a.testStop() }

// Setup: finish

// FinishSetup sets up the desktop's shortcut where there is no global hotkey (GNOME on
// Wayland) and starts the hotkey.
func (api *API) FinishSetup() error {
	g, a := api.g, api.g.a
	if wayland() && gnomeShortcuts() && desktopShortcut() == "" && a.cfg.Hotkey != "none" {
		exe, err := executable()
		if err != nil {
			return err
		}
		spec := a.cfg.Hotkey
		if spec == "" {
			spec = defaultHotkey()
		}
		if err := setDesktopShortcut(exe, spec); err != nil {
			return err
		}
	}
	g.setHotkey(a.startHotkey())
	return nil
}

// Status page

// CheckConnection asks the paired phone whether it's there and still knows this computer.
func (api *API) CheckConnection() (string, error) {
	a := api.g.a
	if a.cfg.Phone == nil {
		return "", errors.New("no phone is paired yet")
	}
	if err := a.pingPhone(); err != nil {
		return "", errors.New(plainError(err.Error()))
	}
	return fmt.Sprintf("%s answers at %s.", a.cfg.Phone.Name, a.cfg.Phone.Addr), nil
}

// Toggle starts or stops a dictation, like the hotkey.
func (api *API) Toggle() { go api.g.a.trayAction(api.g.a.toggle) }

// Cancel closes Rambler without pasting.
func (api *API) Cancel() {
	go api.g.a.trayAction(func() error { return api.g.a.stop(false) })
}

// Settings

// SetHotkey makes spec ("ctrl+alt+d", or "none") the hotkey, if it can be registered;
// else the old one stays.
func (api *API) SetHotkey(spec string) error {
	g, a := api.g, api.g.a
	spec = strings.ToLower(strings.ReplaceAll(spec, " ", ""))
	if spec != "none" {
		mods, key, err := splitHotkey(spec)
		if err != nil {
			return err
		}
		if len(mods) == 0 && !(len(key) > 1 && key[0] == 'f') {
			return errors.New("a hotkey needs Ctrl, Alt, Shift or another modifier, or an F key")
		}
	}
	if wayland() {
		if !gnomeShortcuts() {
			return errors.New("on this desktop, set the shortcut in its keyboard settings")
		}
		exe, err := executable()
		if err != nil {
			return err
		}
		if err := setDesktopShortcut(exe, spec); err != nil {
			return err
		}
		if err := g.updateConfig(func(c *config) { c.Hotkey = spec }); err != nil {
			return err
		}
		g.setHotkey(a.startHotkey())
		return nil
	}
	old := a.cfg.Hotkey
	a.cfg.Hotkey = spec
	line, err := a.startHotkey()
	if err != nil {
		a.cfg.Hotkey = old
		g.setHotkey(a.startHotkey())
		return fmt.Errorf("%s can't be used: another app may have it. Try another one", hotkeyName(spec))
	}
	g.setHotkey(line, nil)
	return g.updateConfig(func(c *config) { c.Hotkey = spec })
}

// PauseHotkey turns the hotkey off while the window records a new one, so pressing the
// current one there doesn't start a dictation.
func (api *API) PauseHotkey(pause bool) {
	g := api.g
	g.mu.Lock()
	changed := g.hotkeyPause != pause
	g.hotkeyPause = pause
	g.mu.Unlock()
	if !changed || wayland() {
		return
	}
	if pause {
		unregisterHotkeys()
	} else {
		g.setHotkey(g.a.startHotkey())
	}
}

func (api *API) SetPaste(on bool) error {
	a := api.g.a
	if err := api.g.updateConfig(func(c *config) { c.Paste = &on }); err != nil {
		return err
	}
	a.paste = on && os.Getenv("HEARSAY_PASTE") != "0"
	return nil
}

func (api *API) SetAutostart(on bool) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if strings.Contains(exe, "go-build") {
		return errors.New("this is a `go run` build: build hearsay first so the login item has a stable path")
	}
	if err := setAutostart(exe, on); err != nil {
		return err
	}
	api.g.wails.Event.Emit("state")
	return nil
}

// Troubleshoot

// Forget unpairs. The message says if the phone couldn't be told.
func (api *API) Forget() (string, error) {
	g := api.g
	g.a.mu.Lock()
	phoneErr, err := g.a.unpair()
	g.a.mu.Unlock()
	if err != nil {
		return "", err
	}
	g.wails.Event.Emit("state")
	if phoneErr != nil {
		return "Forgotten here. The phone couldn't be reached: remove this computer in the Hearsay app on the phone.", nil
	}
	return "Forgotten on both sides.", nil
}

func (api *API) OpenLog() error {
	p := runFile("hearsay.log")
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		f.Close()
	}
	return api.g.wails.Browser.OpenFile(p)
}

// SaveReport writes a report for a bug report where the user picks, and returns its path
// ("" if they cancelled).
func (api *API) SaveReport() (string, error) {
	home, _ := os.UserHomeDir()
	p, err := api.g.wails.Dialog.SaveFile().
		SetMessage("Save a report to attach to a bug report").
		SetDirectory(home).
		SetFilename("hearsay-report.txt").
		AttachToWindow(api.g.win).
		PromptForSingleSelection()
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, []byte(api.report()), 0o644)
}

// report is what SaveReport writes: the setup and the end of the log, nothing dictated
// (the log has lengths and timings, not text).
func (api *API) report() string {
	s := api.State()
	var b strings.Builder
	line := func(k string, v any) { fmt.Fprintf(&b, "%-18s %v\n", k+":", v) }
	line("hearsay", s.Version)
	line("system", runtime.GOOS+"/"+runtime.GOARCH)
	line("desktop", os.Getenv("XDG_CURRENT_DESKTOP")+" wayland="+fmt.Sprint(s.Wayland))
	if s.Phone != nil {
		line("phone", s.Phone.Name+" at "+s.Phone.Addr)
	} else {
		line("phone", "not paired")
	}
	line("keyboard", s.KeyboardID)
	line("hotkey", s.HotkeyLine)
	if s.HotkeyError != "" {
		line("hotkey error", s.HotkeyError)
	}
	line("paste", s.Paste)
	line("start at login", s.Autostart)
	line("status", s.Status.State+" "+s.Status.Detail)
	if s.Status.LastError != "" {
		line("last error", s.Status.LastError)
	}
	if msg, err := api.CheckConnection(); err != nil {
		line("connection", err)
	} else {
		line("connection", msg)
	}
	b.WriteString("\n== log (last 80 lines)\n")
	if f, err := os.Open(runFile("hearsay.log")); err == nil {
		var lines []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if len(lines) > 80 {
				lines = lines[1:]
			}
		}
		f.Close()
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	return b.String()
}
