# Hearsay

Dictate on the computer with Rambler (Gboard's AI voice typing) on an Android phone. The
README covers what users see; this file covers working on the code.

## Layout

- `cmd/hearsay/`: the computer program (Go, one binary for Windows/Linux/macOS).
  - `net.go` (TLS connection to the phone, requests), `pair.go`, `mdns.go`
  - `dictate.go` (clipboard and paste), `desktop_*.go` (per OS), `hotkey*.go`, `tray.go`
  - `setup.go`, `setup_net.go` (terminal setup), `gui.go`, `gui_api.go` + `frontend/`
    (the Wails v3 window; plain HTML/CSS/JS, no build step; `mock.js` for a browser preview)
  - `stats.go` (one line of numbers per dictation in `stats.jsonl`, and the Stats page's
    figures worked out from them; never the text)
  - `files.go` (config, status file, run lock, log), `autostart_*.go`, `diagnose.go` (adb)
  - `apk/hearsay.apk`: the phone app, embedded for `setup --adb` (gitignored)
- `app/src/main/java/me/hassandev/hearsay/`: the phone app (Kotlin, no AndroidX).
  - `PhoneServer.kt` (TLS server, protocol documented at the top), `Session.kt` (one
    dictation: switch keyboard, press mic, wait for text, restore), `KeyPressService.kt`
    (accessibility service that taps Gboard's keys), `DictateActivity.kt` (the text
    field), `HomeActivity.kt` (launcher screen, pairing dialog)

## Commands

- `scripts/check.sh`: gofmt, go vet (Linux + Windows), go test, Kotlin compile. Prints only
  failures. `scripts/check.sh go` / `app` for one side. Run it after every change.
  A darwin `go vet` always fails here (Wails needs cgo for macOS); that's expected.
- `scripts/release.sh vX.Y.Z [--publish]`: the whole release (see the script's header).
- `scripts/package-linux.sh [--install]`: a distro package of the working tree (Arch only
  so far: `bin/pkg-arch/hearsay-*.pkg.tar.zst`). `scripts/aur.sh vX.Y.Z [--publish]`:
  after a release, points the AUR's `hearsay-bin` (`packaging/arch/PKGBUILD`) at it.
  A program under `/usr` doesn't update itself (`packageManaged` in `update.go`).
- Local build for the user's own machine:
  `CGO_ENABLED=1 go build -trimpath -ldflags "-X main.version=vX.Y.Z -X main.phoneApp=N" -o bin/hearsay ./cmd/hearsay`
  (N: `versionCode` in `app/build.gradle.kts`; without it the computer doesn't compare
  the phone app's version).
- Updates (`update.go`, `phoneapp.go`): the tray checks GitHub's latest release daily and
  can replace its own binary with it (linux/windows amd64 builds, checked against
  `SHA256SUMS`). The phone puts its versionCode in every reply; requests carry the one this
  build came with (`want`), so each side can say when the phone app is out of date.

## The user's machine (Arch, GNOME Wayland, Pixel 9 with Pixelboard)

- Their Ctrl+Super+Space GNOME shortcut runs `bin/hearsay toggle`. The tray
  doesn't start at login (they turned that off). To put the working tree on their computer and phone:
  `scripts/install-local.sh` (`pc` or `phone` for one side). It builds, installs over adb,
  checks the key presser, and restarts the tray. Don't `pkill -f "bin/hearsay run"` in a
  compound command: it matches its own shell and kills it.
- They dictate through this many times a day: don't leave it broken.
- Phone over wireless debugging: `adb mdns services`, then `adb devices` (it usually
  auto-connects). Install: `adb install -r bin/release-vX.Y.Z/hearsay.apk`, then check
  `adb shell settings get secure enabled_accessibility_services` still lists
  `me.hassandev.hearsay/.KeyPressService`.

## Logs (for "it didn't work" reports)

- Computer: `$XDG_RUNTIME_DIR/hearsay/hearsay.log` when run without a terminal (tray,
  shortcut). It includes the phone's per-request timings (`phone +NNNms ...`), so most
  questions are answered there: `grep -E "^---|phone \+" $XDG_RUNTIME_DIR/hearsay/hearsay.log`.
  Normal stop: `pressed mic` ~+70ms, `text arrived` ~+0.8-1.1s, `got N chars`.
- Phone: `adb logcat -d -s Hearsay` (short buffer: only recent requests).
- Config: `~/.config/hearsay/config.json`. Stats: `~/.config/hearsay/stats.jsonl`.

## Conventions

- Commit or push only when the user asks. No Claude attribution lines in commits or PRs.
- `*HANDOFF.md` (gitignored) are old planning notes; mostly history, don't read them
  unless asked. Review/issue notes (`ISSUES-*.md`) stay uncommitted.
- Code reviews: cite functions and files, not line numbers (they go stale fast).
- When the phone app changes, bump `versionCode` and `versionName` in
  `app/build.gradle.kts`. Signing key: `~/.config/hearsay-signing/hearsay-release.jks`
  (alias `hearsay`, passwords in the gitignored `keystore.properties`). A different key
  would force users to uninstall.
- Keep "Rambler", "Pixel" and "Gboard" out of names (Google trademarks; "works with
  Rambler" is fine). Never bundle or link to Pixelboard (a modified Google APK).
- Comments explain why, in plain sentences; keep the existing style.
