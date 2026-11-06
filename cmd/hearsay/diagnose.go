package main

import (
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	uiNode = regexp.MustCompile(`<node [^>]*>`)
	uiAttr = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
)

// diagnose writes what Hearsay needs to know about a keyboard it hasn't been tried with:
// the keyboard's keys (resource ids, descriptions, positions) before and while Rambler
// listens, and how the recording shows up in dumpsys audio. Only the keyboard's named keys
// are kept, without their text, so nothing typed or dictated ends up in the file.
func (a *app) diagnose() error {
	if err := a.requireKeyboard(); err != nil {
		return err
	}
	if err := a.requireDevice(); err != nil {
		return err
	}
	kbPkg, _, _ := strings.Cut(a.cfg.Keyboard, "/")
	var b strings.Builder
	info, _ := a.shell(10*time.Second, "getprop ro.product.model; getprop ro.build.version.release; "+
		"dumpsys package "+kbPkg+" | grep -m1 versionName")
	fmt.Fprintf(&b, "keyboard: %s\n%s\n", a.cfg.Keyboard, info)

	prev, _ := a.shell(8*time.Second, "settings get secure default_input_method")
	if prev == a.cfg.Keyboard {
		prev = ""
	}
	writeState(state{prev, "1", "calibrating"})
	defer a.restore()
	if _, rc := a.phone(calibratePhone, 10*time.Second); rc != 0 {
		return a.fail("keyboard didn't come up")
	}
	dump := func(title string) {
		fmt.Println("Reading the keyboard (takes a few seconds)…")
		const f = "/sdcard/hearsay-ui.xml"
		a.adb(30*time.Second, "shell", "uiautomator dump --windows "+f+" >/dev/null")
		xml, _ := a.adb(10*time.Second, "exec-out", "cat "+f)
		a.adb(5*time.Second, "shell", "rm -f "+f)
		fmt.Fprintf(&b, "\n== %s\n", title)
		n := 0
		for _, node := range uiNode.FindAllString(xml, -1) {
			attrs := map[string]string{}
			for _, m := range uiAttr.FindAllStringSubmatch(node, -1) {
				attrs[m[1]] = html.UnescapeString(m[2])
			}
			// Header keys, and other keys with a name longer than a letter (Delete, Space…).
			id, desc := attrs["resource-id"], attrs["content-desc"]
			if attrs["package"] != kbPkg || !(strings.Contains(id, "header") || len([]rune(desc)) > 1) {
				continue
			}
			n++
			fmt.Fprintf(&b, "%s | %q | %s | %s\n", attrs["resource-id"], attrs["content-desc"],
				attrs["class"], attrs["bounds"])
		}
		if n == 0 {
			fmt.Fprintf(&b, "(no keyboard nodes; dump was %d bytes)\n", len(xml))
		}
	}
	dump("keyboard up")
	ask("On the phone, tap the mic key so Rambler starts listening, then press Enter here.")
	audio, _ := a.shell(10*time.Second, "d=$(dumpsys audio | grep VOICE_RECOGNITION); echo \"$d\" | grep client=; echo \"$d\" | grep \" rec \" | tail -2")
	fmt.Fprintf(&b, "\n== dumpsys audio (VOICE_RECOGNITION)\n%s\n", audio)
	dump("Rambler listening")
	ask("Now close Rambler on the phone (any way), then press Enter here.")

	out := "hearsay-diagnose.txt"
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved %s. Look it over, then attach it to the issue.\n", out)
	return nil
}
