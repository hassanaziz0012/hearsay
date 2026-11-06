package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The scripts that run on the phone; see phone/lib.sh.
var (
	//go:embed phone/lib.sh
	phoneLib string
	//go:embed phone/start.sh
	startPhone string
	//go:embed phone/stop.sh
	stopPhone string
	//go:embed phone/restore.sh
	restorePhone string
	//go:embed phone/calibrate.sh
	calibratePhone string
)

func adbName() string {
	if runtime.GOOS == "windows" {
		return "adb.exe"
	}
	return "adb"
}

// findAdb looks on PATH, in the usual SDK places, then in the copy setup downloads.
func findAdb() (string, error) {
	name := adbName()
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	var dirs []string
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if d := os.Getenv(env); d != "" {
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs, "/opt/android-sdk", dataDir())
	for _, d := range dirs {
		p := filepath.Join(d, "platform-tools", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errNoAdb
}

// adb runs adb with a timeout and returns its stdout and exit code. The code is -1 when
// adb couldn't run or timed out. stderr goes to the log.
func (a *app) adb(timeout time.Duration, args ...string) (string, int) {
	return a.adbTo(a.log, timeout, args...)
}

// adbQuiet is adb without stderr (get-state's "no devices" isn't worth logging).
func (a *app) adbQuiet(timeout time.Duration, args ...string) (string, int) {
	return a.adbTo(io.Discard, timeout, args...)
}

func (a *app) adbTo(stderr io.Writer, timeout time.Duration, args ...string) (string, int) {
	if a.serial != "" {
		args = append([]string{"-s", a.serial}, args...)
	}
	return a.adbAny(stderr, timeout, args...)
}

// adbAny is adb without picking the device, for commands like devices, pair and connect.
func (a *app) adbAny(stderr io.Writer, timeout time.Duration, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.adbPath, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, stderr
	// A freshly started adb server may hold on to the client's pipes.
	cmd.WaitDelay = 500 * time.Millisecond
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil:
		return out.String(), cmd.ProcessState.ExitCode()
	case err == nil:
		return out.String(), 0
	case ctx.Err() == nil && errors.As(err, &ee):
		return out.String(), ee.ExitCode()
	default:
		return out.String(), -1
	}
}

func (a *app) connected() bool {
	out, _ := a.adbQuiet(3*time.Second, "get-state")
	return strings.TrimSpace(out) == "device"
}

func (a *app) requireDevice() error {
	if a.connected() {
		a.logf("connected")
		return nil
	}
	// Wireless adb usually comes back by itself within a few seconds (mDNS).
	a.logf("not connected, waiting")
	a.adbQuiet(4*time.Second, "wait-for-device")
	if a.connected() {
		return nil
	}
	if _, ok := readState(); ok {
		return a.fail("phone not connected over adb. Reconnect, then press again")
	}
	return a.fail("phone not connected over adb")
}

// phone runs script on the phone (after phone/lib.sh) with the common variables and the
// given name/value pairs set, and returns its stdout and exit code.
func (a *app) phone(script string, timeout time.Duration, vars ...string) (string, int) {
	x, y := "", ""
	if m := a.cfg.Mic; m != nil {
		x, y = strconv.Itoa(m[0]), strconv.Itoa(m[1])
	}
	d := "0"
	if a.debug {
		d = "1"
	}
	vars = append([]string{"IME", a.cfg.Keyboard, "PKG", pkg, "ACT", activity, "X", x, "Y", y, "D", d}, vars...)
	var sb strings.Builder
	for i := 0; i+1 < len(vars); i += 2 {
		sb.WriteString(vars[i] + "=" + shellQuote(vars[i+1]) + " ")
	}
	sb.WriteString(";\n" + phoneLib + script)
	return a.adb(timeout, "shell", sb.String())
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// adbFailed handles a failed phone script (exit code rc): nil if the script itself failed
// (codes 10-19) and the phone is still there, so the caller reports what went wrong.
// Otherwise adb failed: report msg and keep the state file, so the next press can finish
// the job once the phone is back.
func (a *app) adbFailed(rc int, msg string) error {
	if rc >= 10 && rc < 20 && a.connected() {
		return nil
	}
	return a.fail(msg + ". Reconnect, then press again")
}
