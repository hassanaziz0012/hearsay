#!/usr/bin/env bash
# Puts the working tree on this machine and the phone: what `release.sh` builds, without
# publishing anything.
#
#   scripts/install-local.sh          phone app (over wireless adb) and computer program
#   scripts/install-local.sh pc       computer program only
#   scripts/install-local.sh phone    phone app only
#
# The version is "v" + versionName from app/build.gradle.kts. The computer program goes to
# bin/hearsay (what the desktop shortcut and autostart run) and the tray is restarted.
set -euo pipefail
cd "$(dirname "$0")/.."

what=${1:-all}
case $what in all | pc | phone) ;; *) echo "usage: $0 [pc|phone]" >&2; exit 2 ;; esac

vname=$(sed -n 's/^ *versionName = "\(.*\)"/\1/p' app/build.gradle.kts)
vcode=$(sed -n 's/^ *versionCode = \([0-9]*\)/\1/p' app/build.gradle.kts)
[ -n "$vname" ] && [ -n "$vcode" ] || { echo "no versionName/versionCode in app/build.gradle.kts" >&2; exit 1; }
apk=app/build/outputs/apk/release/app-release.apk

echo "== phone app $vname ($vcode)"
./gradlew -q --offline assembleRelease 2>&1 | grep -v "^w:" || true
[ -f "$apk" ] || { echo "no $apk" >&2; exit 1; }
cp "$apk" cmd/hearsay/apk/hearsay.apk # what `hearsay setup --adb` installs

if [ "$what" != pc ]; then
	# Wireless debugging: listing the mDNS services is what makes adb connect.
	for _ in 1 2 3 4 5; do
		[ "$(adb devices | grep -c 'device$')" -gt 0 ] && break
		adb mdns services >/dev/null
		sleep 2
	done
	adb devices | grep -q 'device$' || { echo "no phone over adb: turn on wireless debugging" >&2; exit 1; }
	adb install -r "$apk" | tail -1
	adb shell dumpsys package me.hassandev.hearsay | grep -m1 -o "versionName=.*"
	if adb shell settings get secure enabled_accessibility_services | grep -q me.hassandev.hearsay/.KeyPressService; then
		echo "key presser still on"
	else
		echo "warning: the key presser is off on the phone: turn it on in Accessibility settings" >&2
	fi
fi

if [ "$what" != phone ]; then
	echo "== computer program v$vname"
	CGO_ENABLED=1 go build -trimpath -ldflags "-X main.version=v$vname -X main.phoneApp=$vcode" -o bin/hearsay ./cmd/hearsay
	# pgrep -f would also match this script's own command line; match the program exactly.
	for pid in $(pgrep -x hearsay); do
		if tr '\0' ' ' </proc/"$pid"/cmdline | grep -q "bin/hearsay run"; then kill "$pid"; fi
	done
	sleep 1
	setsid bin/hearsay run >/dev/null 2>&1 </dev/null &
	sleep 2
	pgrep -x hearsay >/dev/null && echo "tray restarted" || { echo "the tray didn't start: see $XDG_RUNTIME_DIR/hearsay/hearsay.log" >&2; exit 1; }
fi
