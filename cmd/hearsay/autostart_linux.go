package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

const gnomeKeys = "org.gnome.settings-daemon.plugins.media-keys"

// setupDesktopShortcut binds a desktop shortcut to `hearsay toggle`, since Wayland doesn't
// let apps grab keys. Only GNOME can be set up from here; other desktops get the steps.
func (a *app) setupDesktopShortcut(exe string) error {
	cmd := quoteArg(exe) + " toggle"
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	_, err := exec.LookPath("gsettings")
	if !strings.Contains(desktop, "GNOME") || err != nil {
		say("Wayland doesn't let apps set global hotkeys. Add a keyboard shortcut in your")
		say("desktop's settings that runs:")
		say("    %s", cmd)
		if strings.Contains(desktop, "KDE") {
			say("(KDE: System Settings > Keyboard > Shortcuts > Add New > Command or Script.)")
		}
		return nil
	}

	paths := gnomeCustomPaths()
	for _, p := range paths {
		c := gsettingsGet(gnomeKeys+".custom-keybinding:"+p, "command")
		if !strings.Contains(c, "hearsay") || !strings.HasSuffix(c, " toggle") {
			continue
		}
		bind := gsettingsGet(gnomeKeys+".custom-keybinding:"+p, "binding")
		if c == cmd {
			done("GNOME shortcut %s runs %s", bind, cmd)
			return nil
		}
		say("GNOME shortcut %s runs %s.", bind, c)
		if yes("Point it at this hearsay ("+exe+")?", true) {
			return gsettingsSet(gnomeKeys+".custom-keybinding:"+p, "command", cmd)
		}
		return nil
	}

	bind := gnomeAccel(defaultHotkey())
	say("Wayland doesn't let apps set global hotkeys, so this needs a GNOME shortcut.")
	if !yes(fmt.Sprintf("Add a GNOME shortcut %s that runs `%s`?", bind, cmd), true) {
		say("Add one yourself in Settings > Keyboard > Custom Shortcuts, running:")
		say("    %s", cmd)
		return nil
	}
	p := ""
	for i := 0; p == "" || slices.Contains(paths, p); i++ {
		p = fmt.Sprintf("/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/hearsay%d/", i)
	}
	schema := gnomeKeys + ".custom-keybinding:" + p
	for _, kv := range [][2]string{{"name", "Hearsay dictation"}, {"command", cmd}, {"binding", bind}} {
		if err := gsettingsSet(schema, kv[0], kv[1]); err != nil {
			return err
		}
	}
	list := make([]string, 0, len(paths)+1)
	for _, q := range append(paths, p) {
		list = append(list, "'"+q+"'")
	}
	if err := exec.Command("gsettings", "set", gnomeKeys, "custom-keybindings", "["+strings.Join(list, ", ")+"]").Run(); err != nil {
		return fmt.Errorf("gsettings: %v", err)
	}
	done("GNOME shortcut %s runs %s (change it in Settings > Keyboard)", bind, cmd)
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
	return b.String() + key
}

// quoteArg quotes a path for the shell-like parsing of desktop files and GNOME commands.
func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s) + `"`
}

func autostartFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "autostart", "hearsay.desktop")
}

// setupAutostart offers an XDG autostart entry for `hearsay run` (the tray).
func (a *app) setupAutostart(exe string) error {
	p := autostartFile()
	execLine := "Exec=" + quoteArg(exe) + " run"
	if b, err := os.ReadFile(p); err == nil {
		if strings.Contains(string(b), execLine+"\n") {
			done("the tray starts at login (%s)", p)
			return nil
		}
		say("%s starts something else at login.", p)
		if !yes("Replace it with this hearsay ("+exe+")?", true) {
			return nil
		}
	} else if !yes("Start the Hearsay tray icon at login?", true) {
		return nil
	}
	desktop := "[Desktop Entry]\nType=Application\nName=Hearsay\nComment=Tray icon for Hearsay dictation\n" +
		execLine + "\nIcon=audio-input-microphone-symbolic\nX-GNOME-Autostart-enabled=true\nNoDisplay=true\n"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(desktop), 0o644); err != nil {
		return err
	}
	done("the tray starts at login (%s)", p)
	return startDetached(exe, "run")
}

// startDetached starts the tray now, in its own session so closing the terminal doesn't
// end it.
func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done("started `hearsay run`")
	return cmd.Process.Release()
}
