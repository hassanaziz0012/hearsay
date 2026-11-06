# Hearsay

Dictate on your computer with **Rambler**, the AI voice typing in Gboard, running on your
Android phone. Press a hotkey on the computer, speak to the phone, press it again, and the
cleaned-up text is pasted wherever you're typing on the computer.

The phone stays on the desk, locked if you like. The Hearsay app on the phone opens a text
field, presses Gboard's mic key and sends the text back to the computer over your Wi-Fi.
The connection is paired and encrypted: the phone only answers computers you've paired it
with.

Rambler is Google's feature; Hearsay isn't affiliated with Google.

**Status: v0.1, early testing.** Windows and Linux builds. macOS isn't built yet.

## What you need

- An Android phone (Android 14 or newer) with Rambler in Gboard: Gboard settings > Voice
  typing. Newer Pixels have it. Any Gboard build that has Rambler works too.
- The phone and the computer on the same Wi-Fi. Guest and hotel networks that keep devices
  apart won't work.
- Windows 10/11 or Linux.

## Install

1. Download the build for your computer from the
   [latest release](https://github.com/hassanaziz0012/hearsay/releases/latest) and unzip it
   somewhere it can stay (e.g. `Documents\Hearsay`). The tray icon is started from there at
   login, so don't run it from Downloads and then delete it.
2. Windows may warn that `hearsay.exe` is from an unknown publisher (it isn't signed).
   Click **More info**, then **Run anyway**.
3. Open a terminal in that folder (on Windows: type `cmd` in Explorer's address bar and
   press Enter) and run:

       hearsay setup

Setup asks how to reach the phone:

- **With adb (easier):** turn on Developer options on the phone (Settings > About phone >
  tap Build number 7 times), then USB debugging or Wireless debugging. Setup downloads
  Google's adb if you don't have it, installs the Hearsay app, turns on its key presser
  and allows what it needs. You can turn debugging off again afterwards; dictation
  doesn't use it.
- **Without adb:** you install the app from the release link (setup shows a QR code) and
  turn things on by hand, as setup explains. Play Protect blocks apps it doesn't know, so
  you turn it off for the install and back on after. Android may also call the key
  presser a "restricted setting"; the app shows how to allow it.

Either way setup then pairs the computer with the phone (both show a 6-digit code; tap
**Pair** in the app if they match), checks the keyboard, does a test dictation and sets up
the hotkey and the tray icon.

## Use

- **Ctrl+Alt+Space** starts dictation, the same again stops it and pastes the text. The
  tray icon's menu does the same (Start dictation / Stop and paste / Cancel).
- From a terminal: `hearsay` (toggle), `hearsay start`, `hearsay stop`, `hearsay cancel`.
- `hearsay pair` pairs again, `hearsay pair --forget` unpairs.
- Settings are in `config.json` in `%AppData%\hearsay` (Windows) or `~/.config/hearsay`
  (Linux): `hotkey` (e.g. `"ctrl+alt+space"`, `"none"`), `paste` (`false` copies to the
  clipboard without pasting).

On Linux with Wayland, global hotkeys go through your desktop's own shortcut settings;
setup adds one for GNOME and prints the command for others (`hearsay toggle`).

## Troubleshooting

- **"Couldn't find the phone":** is it on the same Wi-Fi, and is the key presser on (the
  Hearsay app shows it)? Then press again.
- **Gboard says "No permission to enable: Voice typing":** allow Gboard the microphone
  (Settings > Apps > Gboard > Permissions). Setup checks this.
- **Dictation doesn't start:** run `hearsay diagnose` (needs adb). It saves
  `hearsay-diagnose.txt` with the names and positions of Gboard's keys, which helps if your
  Gboard differs. It contains nothing you typed or said; look it over before sending.
- **Logs:** `%LocalAppData%\hearsay\hearsay.log` (Windows) or
  `$XDG_RUNTIME_DIR/hearsay/hearsay.log` (Linux). `HEARSAY_DEBUG=1` adds timings.

Please report problems in [Issues](https://github.com/hassanaziz0012/hearsay/issues) with
the setup output or the log.

## Uninstall

- Quit the tray icon (menu > Quit Hearsay) and delete the folder.
- Windows: remove the login entry with
  `reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v Hearsay /f`, then
  delete `%LocalAppData%\hearsay` and `%AppData%\hearsay`.
- Linux: delete `~/.config/autostart/hearsay.desktop`, `~/.config/hearsay` and
  `~/.local/share/hearsay`, and the GNOME shortcut if setup added one.
- On the phone: uninstall the Hearsay app.

## Build

    ./gradlew assembleRelease
    cp app/build/outputs/apk/release/app-release.apk cmd/hearsay/apk/hearsay.apk
    go build -o bin/hearsay ./cmd/hearsay

The APK is embedded in the binary for setup's adb path. Release signing reads
`keystore.properties` (not in git).

## License

MIT
