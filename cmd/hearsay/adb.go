package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	return a.fail("phone not connected over adb")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
