package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	resp, err := http.Get(url)
	if err != nil {
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
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}

	// Unpack next to the old copy and swap, so a failed update leaves a working adb.
	dest := filepath.Join(dataDir(), "platform-tools")
	tmp := dest + ".new"
	os.RemoveAll(tmp)
	if err := unzip(zipPath, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	os.RemoveAll(dest)
	// The zip has everything under platform-tools/.
	if err := os.Rename(filepath.Join(tmp, "platform-tools"), dest); err != nil {
		return "", err
	}
	os.RemoveAll(tmp)
	adb := filepath.Join(dest, adbName())
	if _, err := os.Stat(adb); err != nil {
		return "", errors.New("the download has no adb in it")
	}
	return adb, nil
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
