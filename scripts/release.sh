#!/usr/bin/env bash
# Builds a release into bin/release-VERSION, and with --publish puts it on GitHub.
#
#   scripts/release.sh v0.4.0              build and package only
#   scripts/release.sh v0.4.0 --publish    also push, tag and create the GitHub release
#
# Steps: signed APK -> check its signing key against the latest release's APK -> embed
# it in cmd/hearsay/apk -> stripped Linux (cgo) and Windows (no cgo) binaries with the
# version (and the APK's versionCode) set -> hearsay-linux-amd64.tar.gz, hearsay-windows-amd64.zip (binary + README +
# LICENSE), hearsay.apk, SHA256SUMS.
#
# Before publishing: commit everything, write bin/release-VERSION/notes.md (the release
# text), and bump versionCode/versionName in app/build.gradle.kts if the phone app changed
# (a higher versionCode is what lets the APK install over the old one).
set -euo pipefail
cd "$(dirname "$0")/.."

V=${1:-}
PUBLISH=${2:-}
[[ $V =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: $0 vX.Y.Z [--publish]" >&2; exit 2; }
[ -z "$PUBLISH" ] || [ "$PUBLISH" = --publish ] || { echo "unknown option $PUBLISH" >&2; exit 2; }
R=bin/release-$V

if [ -n "$PUBLISH" ]; then
	[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "commit your changes first" >&2; exit 1; }
	! git rev-parse -q --verify "refs/tags/$V" >/dev/null || { echo "tag $V exists already" >&2; exit 1; }
	[ -f "$R/notes.md" ] || { echo "write $R/notes.md (the release text) first" >&2; exit 1; }
fi

sdk=$(sed -n 's/^sdk.dir=//p' local.properties 2>/dev/null)
sdk=${sdk:-${ANDROID_HOME:-$HOME/Android/Sdk}}
bt=$(ls -d "$sdk"/build-tools/* | sort -V | tail -1)

echo "== checks"
scripts/check.sh

echo "== phone app"
./gradlew -q --offline assembleRelease 2>&1 | grep -v "^w:" || true
apk=app/build/outputs/apk/release/app-release.apk
[ -f "$apk" ] || { echo "no $apk" >&2; exit 1; }
"$bt/aapt2" dump badging "$apk" | head -1 | grep -o "versionCode='[0-9]*' versionName='[^']*'"
# The computer program knows which phone app it came with, to tell when the phone's is older.
vc=$("$bt/aapt2" dump badging "$apk" | head -1 | sed -n "s/.*versionCode='\([0-9]*\)'.*/\1/p")
[ -n "$vc" ] || { echo "couldn't read the APK's versionCode" >&2; exit 1; }
cert() { "$bt/apksigner" verify --print-certs "$1" | sed -n 's/.*SHA-256 digest: //p' | head -1; }
new=$(cert "$apk")
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if gh release download --pattern hearsay.apk --dir "$tmp" >/dev/null 2>&1; then
	old=$(cert "$tmp/hearsay.apk")
	[ "$new" = "$old" ] || { echo "APK signing key $new differs from the latest release's $old: users couldn't update" >&2; exit 1; }
	echo "signing key matches the latest release"
	if cmp -s "$apk" "$tmp/hearsay.apk"; then echo "(the APK is identical to the latest release's)"; fi
else
	echo "warning: couldn't download the latest release's APK; signing key not compared" >&2
fi
cp "$apk" cmd/hearsay/apk/hearsay.apk

echo "== computer builds"
mkdir -p "$R/hearsay-linux-amd64" "$R/hearsay-windows-amd64"
ld="-s -w -X main.version=$V -X main.phoneApp=$vc"
CGO_ENABLED=1 go build -trimpath -ldflags "$ld" -o "$R/hearsay-linux-amd64/hearsay" ./cmd/hearsay
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ld" -o "$R/hearsay-windows-amd64/hearsay.exe" ./cmd/hearsay
for d in "$R/hearsay-linux-amd64" "$R/hearsay-windows-amd64"; do cp README.md LICENSE "$d/"; done
cp "$apk" "$R/hearsay.apk"

echo "== packages"
(
	cd "$R"
	rm -f hearsay-linux-amd64.tar.gz hearsay-windows-amd64.zip SHA256SUMS
	tar czf hearsay-linux-amd64.tar.gz hearsay-linux-amd64
	# No zip(1) on this machine.
	python3 -I -c '
import zipfile
with zipfile.ZipFile("hearsay-windows-amd64.zip", "w", zipfile.ZIP_DEFLATED) as z:
    for f in ["LICENSE", "README.md", "hearsay.exe"]:
        z.write("hearsay-windows-amd64/" + f)
'
	sha256sum hearsay.apk hearsay-windows-amd64.zip hearsay-linux-amd64.tar.gz >SHA256SUMS
	ls -l hearsay.apk hearsay-windows-amd64.zip hearsay-linux-amd64.tar.gz
)

if [ -z "$PUBLISH" ]; then
	echo "Built in $R. Publish with: $0 $V --publish"
	exit 0
fi

echo "== publish"
git push -q origin HEAD
git tag -a "$V" -m "Hearsay $V"
git push -q origin "$V"
(cd "$R" && gh release create "$V" --title "Hearsay $V" --notes-file notes.md \
	hearsay-windows-amd64.zip hearsay-linux-amd64.tar.gz hearsay.apk SHA256SUMS)
# GitHub's digests must match what was built.
gh release view "$V" --json assets -q '.assets[]|(.digest|sub("sha256:";""))+"  "+.name' | sort >"$tmp/remote"
sort "$R/SHA256SUMS" >"$tmp/local"
grep -v SHA256SUMS "$tmp/remote" | diff - "$tmp/local" && echo "uploaded files match the build"
