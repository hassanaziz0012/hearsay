# Hearsay

Dictate on your computer with **Rambler**, the AI voice typing in Gboard, running on your
Android phone. Press a hotkey on the computer, speak to the phone, press it again, and the
cleaned-up text is pasted wherever you're typing on the computer.

**Website:** [hearsay-site-rho.vercel.app](https://hearsay-site-rho.vercel.app)

The phone stays on the desk, locked if you like. The Hearsay app on the phone opens a text
field, presses Gboard's mic key and sends the text back to the computer over your Wi-Fi.
The connection is paired and encrypted: the phone only answers computers you've paired it
with.

Rambler is Google's feature; Hearsay isn't affiliated with Google.

**Status: v0.3, early testing.** Windows and Linux builds. macOS builds from source but
hasn't been tested yet. See [Known limitations](#known-limitations) before you install.

## See it in action

The 24-second launch video:

https://github.com/user-attachments/assets/5773cd01-ecd2-4273-a0aa-e9b75f7ee332

## What you need

- An Android phone (Android 14 or newer) with Rambler in Gboard: Gboard settings > Voice
  typing. Newer Pixels have it. Any Gboard build that has Rambler works too.
- The phone and the computer on the same Wi-Fi. Guest and hotel networks that keep devices
  apart won't work.
- Windows 10/11, Linux or macOS. Windows needs the Microsoft Edge WebView2 runtime, which
  Windows 11 and up-to-date Windows 10 already have. Linux needs GTK 4 and WebKitGTK 6.0
  (`webkitgtk-6.0` on Arch, `libwebkitgtk-6.0-4` on Debian and Ubuntu 24.04+), which
  GNOME desktops usually have.

## Known limitations

- **Only phones with Rambler.** Hearsay doesn't do any speech recognition itself; it uses
  Gboard's. Without Rambler you'd get ordinary Gboard voice typing at best.
- **A Gboard update can break it.** The phone app finds Gboard's mic key by Gboard's
  internal ID for it, so if Google changes the keyboard, dictation may stop starting until
  Hearsay is updated. `hearsay diagnose` collects what's needed to fix it.
- **The phone app isn't on the Play Store.** You install the APK yourself, which means
  turning Play Protect off for the install and allowing a "restricted setting" for the key
  presser.
- **The computer builds aren't signed.** Windows shows an "unknown publisher" warning.
- **Same Wi-Fi only.** There's no relay over the internet, and networks that keep devices
  apart (guest, hotel, many office networks) don't work.
- **No macOS download yet.** It builds from source on a Mac but hasn't been tested. Windows
  and Linux builds are x86-64 only.
- **Wayland has no global hotkey.** On Linux with Wayland the hotkey is a desktop shortcut
  (set up for you on GNOME, by hand elsewhere).
- **One phone per computer.** Pairing a second phone replaces the first.

## Install

1. Download the build for your computer from the
   [latest release](https://github.com/hassanaziz0012/hearsay/releases/latest) and unzip it
   somewhere it can stay (e.g. `Documents\Hearsay`). The tray icon is started from there at
   login, so don't run it from Downloads and then delete it.
2. Windows may warn that `hearsay.exe` is from an unknown publisher (it isn't signed).
   Click **More info**, then **Run anyway**.
3. Double-click `hearsay` (on Windows `hearsay.exe`). The Hearsay window opens and walks
   you through setup:

   1. **Install the phone app:** scan the QR code with the phone and install the app. Play
      Protect blocks apps it doesn't know, so you turn it off for the install and back on
      after.
   2. **Turn on the key presser** in the app (Android may call it a "restricted setting";
      the window and the app show how to allow it). The window finds the phone on the
      network by itself; if it doesn't, type the address the app shows.
   3. **Pair:** the window and the phone both show a 6-digit code. Tap **Pair** on the
      phone if they match.
   4. **Keyboard:** pick Gboard (or the Gboard build with Rambler). The window checks that
      it's turned on and may use the microphone.
   5. **Try it:** a test dictation, shown in the window.
   6. **Finish:** start at login, and the hotkey.

Prefer a terminal, or have adb? `hearsay setup` does the same steps on the terminal, and
`hearsay setup --adb` installs the app and turns everything on over USB or Wireless
debugging (Developer options on). Setup downloads Google's adb if you don't have it. You
can turn debugging off again afterwards; dictation doesn't use it.

## Use

- **Ctrl+Alt+Space** (Ctrl+Option+Space on a Mac) starts dictation, the same again stops
  it and pastes the text. The tray icon's menu does the same (Start dictation / Stop and
  paste / Cancel), and so does the button on the window's Status page.
- The hotkey works while Hearsay runs in the tray. Closing the window keeps it running;
  **Quit Hearsay** in the tray menu stops it. With start at login on, it starts in the
  tray without the window (`hearsay run`).
- Double-click `hearsay` again (or choose **Open Hearsay** in the tray menu) to open the
  window:
  - **Status:** the paired phone and whether it answers, the hotkey and whether it works,
    and the last error.
  - **Settings:** record a new hotkey, paste or only copy, start at login. Changes apply
    right away.
  - **Troubleshoot:** test the connection, pair again, forget the phone, open the log,
    save a report for a bug report.
- From a terminal: `hearsay toggle`, `hearsay start`, `hearsay stop`, `hearsay cancel`,
  `hearsay pair`, `hearsay pair --forget`.
- Settings are saved in `config.json` in `%AppData%\hearsay` (Windows),
  `~/Library/Application Support/hearsay` (macOS) or `~/.config/hearsay` (Linux).

On Linux with Wayland, apps can't set global hotkeys, so a desktop shortcut runs
`hearsay toggle`. On GNOME, Hearsay adds and changes that shortcut itself; on other
desktops, add one in the keyboard settings (the Settings page shows the command).

On macOS, pasting needs the Accessibility permission for Hearsay (System Settings > Privacy
& Security > Accessibility); macOS asks the first time.

## Troubleshooting

- **"Couldn't find the phone":** is it on the same Wi-Fi, and is the key presser on (the
  Hearsay app shows it)? Then press again.
- **Gboard says "No permission to enable: Voice typing":** allow Gboard the microphone
  (Settings > Apps > Gboard > Permissions). Setup checks this.
- **Something else:** Troubleshoot > **Save report** saves your setup and the end of the
  log to attach to an issue. It never contains what you said.
- **Dictation doesn't start:** run `hearsay diagnose` (needs adb). It saves
  `hearsay-diagnose.txt` with the names and positions of Gboard's keys, which helps if your
  Gboard differs. It contains nothing you typed or said; look it over before sending.
- **Logs:** Troubleshoot > **Open log**. The file is `%LocalAppData%\hearsay\hearsay.log`
  (Windows), `~/Library/Caches/hearsay/hearsay.log` (macOS) or
  `$XDG_RUNTIME_DIR/hearsay/hearsay.log` (Linux). `HEARSAY_DEBUG=1` adds timings.

Please report problems in [Issues](https://github.com/hassanaziz0012/hearsay/issues) with
the report or the log.

## Update

Quit Hearsay (tray menu > Quit Hearsay), unzip the new release over the same folder and
double-click `hearsay` again. When a release updates the phone app too (the release notes
say so), install the new `hearsay.apk` on the phone over the old one: your pairing and
settings stay.

## Uninstall

- Turn off Settings > Start at login, quit Hearsay (tray menu > Quit Hearsay) and delete the
  folder.
- Windows: remove the login entry with
  `reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v Hearsay /f`, then
  delete `%LocalAppData%\hearsay` and `%AppData%\hearsay`.
- Linux: delete `~/.config/autostart/hearsay.desktop`, `~/.config/hearsay` and
  `~/.local/share/hearsay`, and the GNOME shortcut if setup added one.
- macOS: delete `~/Library/LaunchAgents/me.hassandev.hearsay.plist` and
  `~/Library/Application Support/hearsay`.
- On the phone: uninstall the Hearsay app.

## Build

The phone app, then the computer program with the app embedded (for setup's adb path):

    ./gradlew assembleRelease
    cp app/build/outputs/apk/release/app-release.apk cmd/hearsay/apk/hearsay.apk
    go build -o bin/hearsay ./cmd/hearsay

Release signing reads `keystore.properties` (not in git). Release builds can set the
version with `-ldflags "-X main.version=v0.3.0"`.

The window is made with [Wails v3](https://wails.io): a plain HTML, CSS and JS front end in
`cmd/hearsay/frontend`, embedded in the binary, with no JS build step. Go 1.26 or newer.
What each system needs to build:

- **Linux:** cgo (a C compiler) and the GTK 4 and WebKitGTK 6.0 development packages:
  `gtk4 webkitgtk-6.0` (Arch), `libgtk-4-dev libwebkitgtk-6.0-dev` (Debian, Ubuntu
  24.04+), `gtk4-devel webkitgtk6.0-devel` (Fedora). The global hotkey also needs the X11
  headers (`libx11-dev`). Build on Linux; cross-compiling to Linux needs those libraries for
  the target.
- **Windows:** no cgo needed, so it also cross-compiles from Linux or macOS:
  `GOOS=windows go build -o bin/hearsay.exe ./cmd/hearsay`. Running it needs the WebView2
  runtime (see above).
- **macOS:** build on a Mac with the Xcode command line tools (`xcode-select --install`);
  cgo is on by default there. Cross-compiling to macOS from elsewhere isn't supported (a
  `CGO_ENABLED=0 go build -tags purego` build compiles, but has no global hotkey).

`go generate ./cmd/hearsay` redraws the tray icons (needs `rsvg-convert` and ImageMagick).
To work on the window's design, serve `cmd/hearsay/frontend` with any static server and
open it in a browser: it uses made-up data from `mock.js` (`?setup` shows the setup steps).

## License

MIT
