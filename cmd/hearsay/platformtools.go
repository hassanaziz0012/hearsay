package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var errNoAdb = errors.New("adb not found. Run `hearsay setup`, or install Android platform-tools and put adb on PATH")

// dataDir holds what setup downloads (platform-tools).
func dataDir() string {
	var d string
	switch runtime.GOOS {
	case "windows":
		d = os.Getenv("LocalAppData")
	case "darwin":
		d, _ = os.UserConfigDir() // ~/Library/Application Support
	default:
		d = os.Getenv("XDG_DATA_HOME")
		if d == "" {
			if h, err := os.UserHomeDir(); err == nil {
				d = filepath.Join(h, ".local", "share")
			}
		}
	}
	if d == "" {
		d = "."
	}
	return filepath.Join(d, "hearsay")
}

const sdkTerms = "https://developer.android.com/studio/terms"

// downloadPlatformTools fetches Google's platform-tools for this OS into dataDir() and
// returns the path of adb in it.
func downloadPlatformTools() (string, error) {
	osName := map[string]string{"windows": "windows", "darwin": "darwin", "linux": "linux"}[runtime.GOOS]
	if osName == "" || (osName == "linux" && runtime.GOARCH != "amd64") {
		return "", fmt.Errorf("Google has no platform-tools for %s/%s. Install adb from your package manager", runtime.GOOS, runtime.GOARCH)
	}
	url := "https://dl.google.com/android/repository/platform-tools-latest-" + osName + ".zip"
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dataDir(), "platform-tools.zip")
	defer os.Remove(zipPath)
	fmt.Println("Downloading", url, "…")
	// No limit on the whole download (it's large and the line may be slow), but give up
	// once nothing has arrived for downloadIdle.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := time.AfterFunc(downloadIdle, cancel)
	defer idle.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the download server didn't answer")
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, idleReader{resp.Body, idle})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the download stalled")
		}
		return "", err
	}

	// Unpack next to the old copy and swap, so a failed update leaves a working adb: the
	// old copy is moved aside, not deleted, until the new one is in place.
	dest := filepath.Join(dataDir(), "platform-tools")
	tmp, old := dest+".new", dest+".old"
	os.RemoveAll(tmp)
	os.RemoveAll(old)
	defer os.RemoveAll(tmp)
	if err := unzip(zipPath, tmp); err != nil {
		return "", err
	}
	hadOld := os.Rename(dest, old) == nil
	// The zip has everything under platform-tools/.
	if err := os.Rename(filepath.Join(tmp, "platform-tools"), dest); err != nil {
		if hadOld {
			os.Rename(old, dest)
		}
		return "", err
	}
	os.RemoveAll(old)
	adb := filepath.Join(dest, adbName())
	if _, err := os.Stat(adb); err != nil {
		return "", errors.New("the download has no adb in it")
	}
	return adb, nil
}

// downloadIdle: a download that gets no data for this long has stalled.
const downloadIdle = time.Minute

// idleReader pushes back the idle timer on every read that gets data.
type idleReader struct {
	r    io.Reader
	idle *time.Timer
}

func (r idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.idle.Reset(downloadIdle)
	}
	return n, err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, zf := range r.File {
		p := filepath.Join(dest, zf.Name)
		if !strings.HasPrefix(p, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("bad path in zip: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		in, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode().Perm()|0o600)
		if err == nil {
			_, err = io.Copy(out, in)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		in.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
