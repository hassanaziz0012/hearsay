#!/usr/bin/env bash
# Packages the working tree for this Linux distro, to install with its package manager.
#
#   scripts/package-linux.sh            build the package into bin/pkg-<distro>/
#   scripts/package-linux.sh --install  and install it (asks for sudo)
#
# Only Arch and its derivatives (pacman) so far: the package is called `hearsay`, puts the
# program in /usr/bin with an app-menu entry, and conflicts with the AUR's hearsay-bin
# (packaging/arch/PKGBUILD, published with scripts/aur.sh). A program under /usr leaves
# updates to the package manager instead of replacing itself (packageManaged in update.go).
#
# The version is "v" + versionName from app/build.gradle.kts, like install-local.sh.
set -euo pipefail
cd "$(dirname "$0")/.."

INSTALL=${1:-}
[ -z "$INSTALL" ] || [ "$INSTALL" = --install ] || { echo "usage: $0 [--install]" >&2; exit 2; }

# ID_LIKE names the distro a derivative is based on (Manjaro, EndeavourOS: "arch").
distro=$(. /etc/os-release && echo "${ID:-} ${ID_LIKE:-}")
case " $distro " in
*" arch "*) ;;
*)
	echo "packaging for $distro isn't supported yet (only Arch and its derivatives)" >&2
	exit 1
	;;
esac

vname=$(sed -n 's/^ *versionName = "\(.*\)"/\1/p' app/build.gradle.kts)
vcode=$(sed -n 's/^ *versionCode = \([0-9]*\)/\1/p' app/build.gradle.kts)
[ -n "$vname" ] && [ -n "$vcode" ] || { echo "no versionName/versionCode in app/build.gradle.kts" >&2; exit 1; }
out=bin/pkg-arch
src=$out/files # not src/: that is makepkg's own

echo "== phone app $vname ($vcode)"
./gradlew -q --offline assembleRelease 2>&1 | grep -v "^w:" || true
apk=app/build/outputs/apk/release/app-release.apk
[ -f "$apk" ] || { echo "no $apk" >&2; exit 1; }
cp "$apk" cmd/hearsay/apk/hearsay.apk # embedded, for `hearsay setup --adb`

echo "== computer program v$vname"
rm -rf "$src"
mkdir -p "$src"
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=v$vname -X main.phoneApp=$vcode" \
	-o "$src/hearsay" ./cmd/hearsay
cp LICENSE README.md cmd/hearsay/icons/app.svg packaging/arch/hearsay.desktop "$src/"

echo "== package"
# The AUR PKGBUILD's package(), but from the files just built instead of a release.
sed -n '/^depends=/,/^options=/p' packaging/arch/PKGBUILD | grep -v -e '^provides=' -e '^conflicts=' >"$src/deps"
cat >"$out/PKGBUILD" <<EOF
pkgname=hearsay
pkgver=$vname
pkgrel=1
pkgdesc="Dictate on the computer with your Android phone's voice typing"
arch=('x86_64')
url="https://github.com/hassanaziz0012/hearsay"
license=('MIT')
$(cat "$src/deps")
provides=('hearsay')
conflicts=('hearsay-bin')

package() {
	cd "$PWD/$src"
	install -Dm755 hearsay "\$pkgdir/usr/bin/hearsay"
	install -Dm644 LICENSE "\$pkgdir/usr/share/licenses/\$pkgname/LICENSE"
	install -Dm644 README.md "\$pkgdir/usr/share/doc/\$pkgname/README.md"
	install -Dm644 app.svg "\$pkgdir/usr/share/icons/hicolor/scalable/apps/hearsay.svg"
	install -Dm644 hearsay.desktop "\$pkgdir/usr/share/applications/hearsay.desktop"
}
EOF
(cd "$out" && rm -f ./*.pkg.tar.* && makepkg -f --noconfirm 2>&1 | grep -E "^(==>|  ->) (ERROR|WARNING)|Finished" || true)
pkg=$(ls "$out"/hearsay-"$vname"-*.pkg.tar.* 2>/dev/null | head -1)
[ -n "$pkg" ] || { echo "makepkg didn't make a package: run it in $out to see why" >&2; exit 1; }
echo "built $pkg"

if [ -n "$INSTALL" ]; then
	sudo pacman -U --noconfirm "$pkg"
else
	echo "install with: sudo pacman -U $pkg"
fi
