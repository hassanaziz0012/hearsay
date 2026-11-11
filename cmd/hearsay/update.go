package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Updates: the tray asks GitHub for the latest release once a day and, when it's newer,
// says so in the window, the tray menu and (once per version) a notification. Where the
// release has a build for this system, "Update now" replaces this program with it and
// restarts; elsewhere the window links to the release page.

const (
	releasesAPI = "https://api.github.com/repos/hassanaziz0012/hearsay/releases/latest"
	updateEvery = 24 * time.Hour
	maxArchive  = 200 << 20
)

// restartEnv tells the new Hearsay that the old one is still quitting, so it waits for
// the tray to be free instead of handing over to the old one and exiting.
const restartEnv = "HEARSAY_RESTARTED"

type release struct {
	Version string // "v0.4.0"
	Page    string // the release's page on GitHub
	archive string // this system's download, "" if the release has none
	sums    string // SHA256SUMS
}

func (c config) updatesOn() bool { return c.Updates == nil || *c.Updates }

// releaseBuild: only release builds have a version to compare (a `go build` is "dev").
func releaseBuild() bool { _, ok := parseVersion(version); return ok }

// parseVersion reads "v1.2.3".
func parseVersion(v string) (n [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 || !strings.HasPrefix(v, "v") {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// newer reports whether version v is later than cur.
func newer(v, cur string) bool {
	a, ok1 := parseVersion(v)
	b, ok2 := parseVersion(cur)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// archiveName is the release file with this system's build (scripts/release.sh).
func archiveName() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "hearsay-linux-amd64.tar.gz"
	case "windows/amd64":
		return "hearsay-windows-amd64.zip"
	}
	return ""
}

var updateHTTP = &http.Client{Timeout: 5 * time.Minute}

func fetch(ctx context.Context, url string, limit int64, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hearsay/"+version)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := updateHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("%s: larger than expected", url)
	}
	return b, err
}

// latestRelease asks GitHub for the newest published release.
func latestRelease(ctx context.Context) (release, error) {
	b, err := fetch(ctx, releasesAPI, 1<<20, "application/vnd.github+json")
	if err != nil {
		return release{}, err
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return release{}, err
	}
	if _, ok := parseVersion(r.Tag); !ok {
		return release{}, fmt.Errorf("the latest release has an odd version %q", r.Tag)
	}
	rel := release{Version: r.Tag, Page: r.Page}
	for _, a := range r.Assets {
		switch a.Name {
		case archiveName():
			rel.archive = a.URL
		case "SHA256SUMS":
			rel.sums = a.URL
		}
	}
	if rel.sums == "" || packageManaged() {
		rel.archive = "" // nothing to check it against, or not ours to replace
	}
	return rel, nil
}

// packageManaged reports whether this program was installed by the system's package
// manager (it lives under /usr, not /usr/local). Replacing it would go behind the package
// manager's back, so updates come through it instead.
func packageManaged() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	exe, err := executable()
	return err == nil && strings.HasPrefix(exe, "/usr/") && !strings.HasPrefix(exe, "/usr/local/")
}

// downloadBinary fetches the release's build for this system, checks it against the
// release's SHA256SUMS and returns the program from inside it.
func downloadBinary(ctx context.Context, rel release) ([]byte, error) {
	if rel.archive == "" {
		return nil, errors.New("this release has no build for this system. Download it from the release page")
	}
	sums, err := fetch(ctx, rel.sums, 64<<10, "")
	if err != nil {
		return nil, err
	}
	want := checksum(string(sums), archiveName())
	if want == "" {
		return nil, fmt.Errorf("SHA256SUMS doesn't list %s", archiveName())
	}
	data, err := fetch(ctx, rel.archive, maxArchive, "")
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return nil, errors.New("the download doesn't match the release's checksum. Try again later")
	}
	return extractBinary(archiveName(), data)
}

// checksum finds name's hash in a sha256sum listing.
func checksum(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// extractBinary takes hearsay (hearsay.exe) out of a release archive.
func extractBinary(name string, data []byte) ([]byte, error) {
	want := "hearsay"
	if strings.HasSuffix(name, ".zip") {
		want = "hearsay.exe"
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if path.Base(f.Name) == want && !f.FileInfo().IsDir() {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer r.Close()
				return io.ReadAll(io.LimitReader(r, maxArchive))
			}
		}
		return nil, fmt.Errorf("%s has no %s", name, want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s has no %s", name, want)
		}
		if err != nil {
			return nil, err
		}
		if path.Base(h.Name) == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// replaceExe puts bin in place of the program at exe. A press that starts meanwhile runs
// either the old program or the new one, never a half-written file.
func replaceExe(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".hearsay-update-*")
	if err != nil {
		return fmt.Errorf("Hearsay can't write to %s, so it can't update itself. Download the new version from the release page instead", dir)
	}
	tmp := f.Name()
	_, err = f.Write(bin)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o755)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	// Windows won't replace a running program, but it lets it be renamed out of the way.
	old := exe + ".old"
	if runtime.GOOS == "windows" {
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		if runtime.GOOS == "windows" {
			os.Rename(old, exe)
		}
		os.Remove(tmp)
		return err
	}
	return nil
}

// removeOldExe deletes what replaceExe left on Windows, once the old program has quit.
func removeOldExe() {
	if exe, err := executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

// waitForOldTray: after an update the old tray is still quitting; wait until it's gone.
func waitForOldTray() {
	if os.Getenv(restartEnv) == "" {
		return
	}
	os.Unsetenv(restartEnv)
	for i := 0; i < 100 && trayRunning(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	removeOldExe()
}

// The tray's side

// watchUpdates checks for a new release now and then, while checks are on. The clock is
// the wall clock, so a computer that sleeps at night still checks once a day.
func (g *gui) watchUpdates() {
	if !releaseBuild() {
		return
	}
	time.Sleep(20 * time.Second) // not while Hearsay is starting
	var last time.Time
	for {
		if g.a.cfg.updatesOn() && time.Since(last) >= updateEvery {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if _, err := g.checkUpdate(ctx, true); err != nil {
				fmt.Fprintln(g.a.log, "hearsay: update check:", err)
			} else {
				last = time.Now().Round(0)
			}
			cancel()
		}
		time.Sleep(time.Hour)
	}
}

// checkUpdate asks for the latest release and, if it's newer, shows it everywhere, with a
// notification the first time (notify) for each version.
func (g *gui) checkUpdate(ctx context.Context, notify bool) (release, error) {
	if !releaseBuild() {
		return release{}, errors.New("this is a development build, so it doesn't check for updates")
	}
	rel, err := latestRelease(ctx)
	if err != nil {
		return rel, err
	}
	if !newer(rel.Version, version) {
		g.setUpdate(nil)
		return rel, nil
	}
	g.setUpdate(&rel)
	fmt.Fprintf(g.a.log, "hearsay: %s is out (this is %s)\n", rel.Version, version)
	if notify && g.a.cfg.UpdateSeen != rel.Version {
		g.a.notify(fmt.Sprintf("Hearsay %s is out. Open Hearsay to update.", rel.Version), 10000, true)
		g.updateConfig(func(c *config) { c.UpdateSeen = rel.Version })
	}
	return rel, nil
}

func (g *gui) setUpdate(rel *release) {
	g.mu.Lock()
	g.update = rel
	g.mu.Unlock()
	if rel != nil {
		g.tray.setUpdate("Update to " + rel.Version + "…")
	} else {
		g.tray.setUpdate("")
	}
	g.wails.Event.Emit("state")
}

// installUpdate replaces this program with the new release and restarts it. It waits for
// no press to be running, and keeps the next one from starting until it's done.
func (g *gui) installUpdate() error {
	g.mu.Lock()
	rel := g.update
	g.mu.Unlock()
	if rel == nil {
		return errors.New("there's no update to install")
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if strings.Contains(exe, "go-build") {
		return errors.New("this is a `go run` build: there's nothing to replace")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	bin, err := downloadBinary(ctx, *rel)
	if err != nil {
		return err
	}
	err = g.a.action(func() error { return replaceExe(exe, bin) })
	if errors.Is(err, errBusy) {
		return errors.New("a dictation is running. Update when it's done")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(g.a.log, "hearsay: updated %s from %s to %s, restarting\n", exe, version, rel.Version)
	args := []string{"run"}
	if g.win.IsVisible() {
		args = []string{"gui"}
	}
	os.Setenv(restartEnv, "1")
	if err := startProcess(exe, args...); err != nil {
		return fmt.Errorf("updated, but Hearsay couldn't start again (%v). Start it yourself", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // let the window get the reply first
		g.wails.Quit()
	}()
	return nil
}
