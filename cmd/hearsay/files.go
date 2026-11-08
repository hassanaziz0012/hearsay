package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// config is config.json in configDir(). Written by setup and pair; people may edit it.
type config struct {
	// IME component of the keyboard that has Rambler, e.g.
	// com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME.
	Keyboard string `json:"keyboard"`
	// Global hotkey for `hearsay run`, e.g. "ctrl+alt+space"; "none" for no hotkey.
	Hotkey string `json:"hotkey,omitempty"`
	// adb serial of the phone, when more than one device is connected (setup, diagnose).
	Serial string `json:"serial,omitempty"`
	// false: copy to the clipboard only, don't send Ctrl+V.
	Paste *bool `json:"paste,omitempty"`
	// The paired phone, written by `hearsay pair` (and setup).
	Phone *pairedPhone `json:"phone,omitempty"`
}

type pairedPhone struct {
	Name string `json:"name"`
	// Last known address, host:port. Found again over mDNS when it stops working.
	Addr string `json:"addr"`
	// Hex SHA-256 of the phone's certificate; any other certificate is refused.
	Cert string `json:"cert"`
}

// The keyboard the bash prototype (pc/rambler-pc) used when it had no keyboard file.
const pixelboard = "com.akshaykadam.pixelboard/com.android.inputmethod.latin.LatinIME"

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "hearsay")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

// runDir holds the tray status, the lock and the log: $HEARSAY_RUN_DIR if set (to run a
// second, separate Hearsay), on Linux $XDG_RUNTIME_DIR (cleared at logout), elsewhere the
// cache dir.
func runDir() string {
	if d := os.Getenv("HEARSAY_RUN_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "hearsay")
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "hearsay")
}

func runFile(name string) string { return filepath.Join(runDir(), name) }

func loadConfig() (config, error) {
	var c config
	b, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) {
		if c, ok := migrateConfig(); ok {
			return c, saveConfig(c)
		}
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %v", configPath(), err)
	}
	return c, nil
}

func saveConfig(c config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	return writeAtomic(configPath(), append(b, '\n'))
}

// migrateConfig reads the bash prototype's config (~/.config/rambler-pc): "keyboard" holds
// the IME component (Pixelboard when absent).
func migrateConfig() (config, bool) {
	d, err := os.UserConfigDir()
	if err != nil {
		return config{}, false
	}
	old := filepath.Join(d, "rambler-pc")
	if _, err := os.Stat(old); err != nil {
		return config{}, false
	}
	c := config{Keyboard: pixelboard}
	if b, err := os.ReadFile(filepath.Join(old, "keyboard")); err == nil {
		if k := strings.TrimSpace(string(b)); k != "" {
			c.Keyboard = k
		}
	}
	return c, true
}

// status tells the tray what the last press is doing: "idle", "busy", "listening",
// "error", or "retry" (an error the next press recovers from), a tab, and a message.
func (a *app) status(st, msg string) {
	writeAtomic(runFile("status"), []byte(st+"\t"+msg+"\n"))
}

func readStatus() (st, msg string, age time.Duration) {
	p := runFile("status")
	b, err := os.ReadFile(p)
	fi, err2 := os.Stat(p)
	if err != nil || err2 != nil {
		return "idle", "", 0
	}
	st, msg, _ = strings.Cut(strings.TrimRight(string(b), "\n"), "\t")
	return st, msg, time.Since(fi.ModTime())
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lockRun takes the lock that keeps two presses from driving the phone at once. A lock
// older than a minute was left by a process that died.
func lockRun() (unlock func(), err error) {
	p := runFile("lock")
	for range 2 {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintln(f, os.Getpid())
			f.Close()
			// Ctrl+C in a terminal shouldn't leave the lock behind.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			go func() {
				if _, ok := <-sig; ok {
					os.Remove(p)
					os.Exit(130)
				}
			}()
			return func() { signal.Stop(sig); close(sig); os.Remove(p) }, nil
		}
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > time.Minute {
			os.Remove(p)
			continue
		}
		break
	}
	return nil, errors.New("still busy with the last press")
}

// openLog opens the log for runs without a terminal, starting a new one past 1 MB.
func openLog() (*os.File, error) {
	p := runFile("hearsay.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		os.Rename(p, p+".1")
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
