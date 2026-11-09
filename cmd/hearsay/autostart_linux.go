package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const gnomeKeys = "org.gnome.settings-daemon.plugins.media-keys"

// setupDesktopShortcut binds a desktop shortcut to `hearsay toggle`, since Wayland doesn't
// let apps grab keys. Only GNOME can be set up from here; other desktops get the steps.
func (a *app) setupDesktopShortcut(exe string) error {
	cmd := quoteArg(exe) + " toggle"
	if !gnomeShortcuts() {
		say("Wayland doesn't let apps set global hotkeys. Add a keyboard shortcut in your")
		say("desktop's settings that runs:")
		say("    %s", cmd)
		if strings.Contains(os.Getenv("XDG_CURRENT_DESKTOP"), "KDE") {
			say("(KDE: System Settings > Keyboard > Shortcuts > Add New > Command or Script.)")
		}
		return nil
	}
	spec := a.cfg.Hotkey
	if spec == "" {
		spec = defaultHotkey()
	}
	if spec == "none" {
		done("no shortcut (\"hotkey\": \"none\" in %s)", configPath())
		return nil
	}
	if p, c, bind := findGnomeShortcut(); p != "" {
		if c == cmd {
			done("GNOME shortcut %s runs %s", bind, cmd)
			return nil
		}
		say("GNOME shortcut %s runs %s.", bind, c)
		if !yes("Point it at this hearsay ("+exe+")?", true) {
			return nil
		}
	} else {
		say("Wayland doesn't let apps set global hotkeys, so this needs a GNOME shortcut.")
		if !yes(fmt.Sprintf("Add a GNOME shortcut %s that runs `%s`?", hotkeyName(spec), cmd), true) {
			say("Add one yourself in Settings > Keyboard > Custom Shortcuts, running:")
			say("    %s", cmd)
			return nil
		}
	}
	if err := setDesktopShortcut(exe, spec); err != nil {
		return err
	}
	done("GNOME shortcut %s runs %s (change it in Settings > Keyboard)", hotkeyName(spec), cmd)
	return nil
}

// gnomeShortcuts reports whether Hearsay can set the desktop's shortcut itself (GNOME).
func gnomeShortcuts() bool {
	_, err := exec.LookPath("gsettings")
	return err == nil && strings.Contains(os.Getenv("XDG_CURRENT_DESKTOP"), "GNOME")
}

// desktopShortcut is the keys of the desktop's shortcut for `hearsay toggle` as a hotkey
// spec ("" if there is none or it can't be read).
func desktopShortcut() string {
	if !gnomeShortcuts() {
		return ""
	}
	_, _, bind := findGnomeShortcut()
	return specFromAccel(bind)
}

// findGnomeShortcut finds the GNOME custom shortcut that runs `hearsay toggle`.
func findGnomeShortcut() (path, cmd, bind string) {
	for _, p := range gnomeCustomPaths() {
		c := gsettingsGet(gnomeKeys+".custom-keybinding:"+p, "command")
		if strings.Contains(c, "hearsay") && strings.HasSuffix(c, " toggle") {
			return p, c, gsettingsGet(gnomeKeys+".custom-keybinding:"+p, "binding")
		}
	}
	return "", "", ""
}

// setDesktopShortcut makes the GNOME shortcut for `exe toggle` use the hotkey spec,
// adding the shortcut if there is none; "none" removes it.
func setDesktopShortcut(exe, spec string) error {
	if !gnomeShortcuts() {
		return errors.New("this desktop's shortcuts can't be set from Hearsay")
	}
	paths := gnomeCustomPaths()
	p, _, _ := findGnomeShortcut()
	if spec == "none" {
		if p == "" {
			return nil
		}
		return setGnomeCustomPaths(slices.DeleteFunc(paths, func(q string) bool { return q == p }))
	}
	if _, _, err := splitHotkey(spec); err != nil {
		return err
	}
	isNew := p == ""
	for i := 0; p == "" || (isNew && slices.Contains(paths, p)); i++ {
		p = fmt.Sprintf("/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/hearsay%d/", i)
	}
	schema := gnomeKeys + ".custom-keybinding:" + p
	for _, kv := range [][2]string{{"name", "Hearsay dictation"}, {"command", quoteArg(exe) + " toggle"}, {"binding", gnomeAccel(spec)}} {
		if err := gsettingsSet(schema, kv[0], kv[1]); err != nil {
			return err
		}
	}
	if !isNew {
		return nil
	}
	return setGnomeCustomPaths(append(paths, p))
}

func setGnomeCustomPaths(paths []string) error {
	list := make([]string, 0, len(paths))
	for _, q := range paths {
		list = append(list, "'"+q+"'")
	}
	v := "[" + strings.Join(list, ", ") + "]"
	if len(list) == 0 {
		v = "@as []"
	}
	if out, err := exec.Command("gsettings", "set", gnomeKeys, "custom-keybindings", v).CombinedOutput(); err != nil {
		return fmt.Errorf("gsettings: %v %s", err, out)
	}
	return nil
}

var gvariantString = regexp.MustCompile(`'([^']*)'`)

func gnomeCustomPaths() []string {
	out, _ := exec.Command("gsettings", "get", gnomeKeys, "custom-keybindings").Output()
	var ps []string
	for _, m := range gvariantString.FindAllStringSubmatch(string(out), -1) {
		ps = append(ps, m[1])
	}
	return ps
}

// gsettingsGet returns a string key's value without GVariant's quotes.
func gsettingsGet(schema, key string) string {
	out, _ := exec.Command("gsettings", "get", schema, key).Output()
	s := strings.TrimSpace(string(out))
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') {
		s = s[1 : len(s)-1]
		s = strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`).Replace(s)
	}
	return s
}

func gsettingsSet(schema, key, value string) error {
	// gsettings parses the value as GVariant text, and a plain string isn't one.
	v := "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
	if out, err := exec.Command("gsettings", "set", schema, key, v).CombinedOutput(); err != nil {
		return fmt.Errorf("gsettings: %v %s", err, out)
	}
	return nil
}

// gnomeAccel turns "ctrl+alt+space" into "<Control><Alt>space".
func gnomeAccel(spec string) string {
	mods, key, _ := splitHotkey(spec)
	names := map[string]string{"ctrl": "Control", "control": "Control", "alt": "Alt",
		"shift": "Shift", "super": "Super", "win": "Super"}
	var b strings.Builder
	for _, m := range mods {
		if n, ok := names[m]; ok {
			b.WriteString("<" + n + ">")
		}
	}
	switch {
	case len(key) > 1 && key[0] == 'f' && key[1] >= '0' && key[1] <= '9':
		key = strings.ToUpper(key)
	case key == "return" || key == "enter":
		key = "Return"
	case key == "tab":
		key = "Tab"
	case key == "escape" || key == "esc":
		key = "Escape"
	}
	return b.String() + key
}

// specFromAccel turns "<Control><Alt>space" into "ctrl+alt+space".
func specFromAccel(accel string) string {
	names := map[string]string{"control": "ctrl", "primary": "ctrl", "alt": "alt",
		"shift": "shift", "super": "super"}
	var parts []string
	for strings.HasPrefix(accel, "<") {
		end := strings.Index(accel, ">")
		if end < 0 {
			return ""
		}
		if n, ok := names[strings.ToLower(accel[1:end])]; ok {
			parts = append(parts, n)
		}
		accel = accel[end+1:]
	}
	if accel == "" {
		return ""
	}
	return strings.Join(append(parts, strings.ToLower(accel)), "+")
}

func autostartFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "autostart", "hearsay.desktop")
}

// autostartState reports whether an XDG autostart entry starts exe's tray at login (on),
// or another hearsay's (other).
func autostartState(exe string) (on, other bool) {
	b, err := os.ReadFile(autostartFile())
	if err != nil {
		return false, false
	}
	on = strings.Contains(string(b), "Exec="+quoteArg(exe)+" run\n")
	return on, !on
}

// setAutostart adds or removes the XDG autostart entry for `hearsay run` (the tray).
func setAutostart(exe string, on bool) error {
	p := autostartFile()
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	desktop := "[Desktop Entry]\nType=Application\nName=Hearsay\nComment=Tray icon for Hearsay dictation\n" +
		"Exec=" + quoteArg(exe) + " run\nIcon=audio-input-microphone-symbolic\nX-GNOME-Autostart-enabled=true\nNoDisplay=true\n"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktop), 0o644)
}
