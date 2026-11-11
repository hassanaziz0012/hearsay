package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
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
	// Typing speed in words per minute from the window's typing test; the stats compare
	// dictating with it. 0: never measured.
	TypingWPM int `json:"typingWpm,omitempty"`
	// false: dictations aren't counted for the Stats page.
	Stats *bool `json:"stats,omitempty"`
	// false: don't ask GitHub for new releases.
	Updates *bool `json:"updates,omitempty"`
	// The newest release a notification has been shown for (once per version).
	UpdateSeen string `json:"updateSeen,omitempty"`
	// The phone app version (versionCode) a "update the phone app" notification has been
	// shown for.
	PhoneUpdateSeen int64 `json:"phoneUpdateSeen,omitempty"`
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

// writeAtomic replaces path with b (mode 0600). Each write has its own temporary file, so
// two processes writing at once (the tray and a press) don't clash.
func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
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
			// Keep the lock fresh while it's held (diagnose waits on the user for minutes),
			// so only a holder that died leaves a lock old enough to be taken over.
			stop := make(chan struct{})
			go func() {
				t := time.NewTicker(lockRefresh)
				defer t.Stop()
				for {
					select {
					case <-stop:
						return
					case now := <-t.C:
						os.Chtimes(p, now, now)
					}
				}
			}()
			return func() { close(stop); signal.Stop(sig); close(sig); os.Remove(p) }, nil
		}
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > lockStale {
			os.Remove(p)
			continue
		}
		break
	}
	return nil, errBusy
}

// A lock untouched for lockStale was left by a process that died; its holder touches it
// every lockRefresh.
const (
	lockStale   = time.Minute
	lockRefresh = 15 * time.Second
)

// errBusy: a press came while another one (in this or another process) was running.
var errBusy = errors.New("still busy with the last press")

// openLog opens the log for runs without a terminal. Past 1 MB it starts a new one, also
// while it is open (the tray runs for days).
func openLog() (io.Writer, error) {
	l := &rotatingLog{path: runFile("hearsay.log")}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

const logMax = 1 << 20

// rotatingLog is an append-only log file that moves itself to path.1 past logMax.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func (l *rotatingLog) open() error {
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > logMax {
		os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.f, l.size = f, 0
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	return nil
}

func (l *rotatingLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > logMax {
		// Another process may have rotated it already: then only reopen.
		if fi, err := os.Stat(l.path); err == nil {
			if cur, err := l.f.Stat(); err == nil && os.SameFile(fi, cur) {
				os.Rename(l.path, l.path+".1")
			}
		}
		old := l.f
		if err := l.open(); err != nil {
			l.f = old // keep writing where we were
		} else {
			old.Close()
		}
	}
	n, err := l.f.Write(b)
	l.size += int64(n)
	return n, err
}
