#!/usr/bin/env bash
# Points the AUR's hearsay-bin at a published GitHub release.
#
#   scripts/aur.sh v0.4.0              update packaging/arch/PKGBUILD and test-build it
#   scripts/aur.sh v0.4.0 --publish    also push it to the AUR
#
# Run it after `scripts/release.sh vX.Y.Z --publish`: the PKGBUILD downloads that release's
# hearsay-linux-amd64.tar.gz. The AUR repo is cloned into bin/aur/hearsay-bin; pushing
# needs the SSH key registered on your AUR account (https://aur.archlinux.org/account).
set -euo pipefail
cd "$(dirname "$0")/.."

V=${1:-}
PUBLISH=${2:-}
[[ $V =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: $0 vX.Y.Z [--publish]" >&2; exit 2; }
[ -z "$PUBLISH" ] || [ "$PUBLISH" = --publish ] || { echo "unknown option $PUBLISH" >&2; exit 2; }
gh release view "$V" >/dev/null 2>&1 || { echo "there's no GitHub release $V: publish it with scripts/release.sh first" >&2; exit 1; }

pb=packaging/arch/PKGBUILD
old=$(sed -n 's/^pkgver=//p' "$pb")
if [ "$old" != "${V#v}" ]; then
	sed -i -e "s/^pkgver=.*/pkgver=${V#v}/" -e "s/^pkgrel=.*/pkgrel=1/" "$pb"
fi

# Build in a copy, so the downloads and makepkg's src/ and pkg/ stay out of the tree.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp "$pb" packaging/arch/hearsay.desktop "$work/"
(
	cd "$work"
	updpkgsums 2>/dev/null
	makepkg -f --noconfirm 2>&1 | grep -E "^(==>|  ->) (ERROR|WARNING)|Finished"
	makepkg --printsrcinfo >.SRCINFO
)
cp "$work/PKGBUILD" "$pb" # with the new checksums
files=$(tar tf "$work"/hearsay-bin-*.pkg.tar.*)
grep -qx usr/bin/hearsay <<<"$files" || { echo "the package has no usr/bin/hearsay" >&2; exit 1; }
echo "packaging/arch/PKGBUILD is at $V and builds"

if [ -z "$PUBLISH" ]; then
	echo "Push it to the AUR with: $0 $V --publish"
	exit 0
fi

aur=bin/aur/hearsay-bin
if [ ! -d "$aur/.git" ]; then
	mkdir -p bin/aur
	git clone -q ssh://aur@aur.archlinux.org/hearsay-bin.git "$aur"
fi
git -C "$aur" pull -q --ff-only 2>/dev/null || true # empty for a package's first push
cp "$work/PKGBUILD" "$work/.SRCINFO" packaging/arch/hearsay.desktop "$aur/"
git -C "$aur" add PKGBUILD .SRCINFO hearsay.desktop
if git -C "$aur" diff --cached --quiet; then
	echo "the AUR already has this"
	exit 0
fi
git -C "$aur" commit -q -m "Update to ${V#v}"
git -C "$aur" push -q origin HEAD:master
echo "pushed hearsay-bin ${V#v} to https://aur.archlinux.org/packages/hearsay-bin"
