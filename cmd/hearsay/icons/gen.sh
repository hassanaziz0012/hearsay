#!/bin/sh
# Renders the tray icons (run by `go generate`; needs rsvg-convert and ImageMagick).
# app.png: the window and app icon. <name>.png: white, for Linux panels; <name>-template.png: black, a macOS template
# image; <name>.ico: grey, for the Windows tray. listening.*: the mic in red.
set -e
cd "$(dirname "$0")"
render() { # svg colour out size
    sed "s/COLOR/$2/g" "$1.svg" | rsvg-convert -w "$4" -h "$4" -o "$3"
}
for n in mic busy error; do
    render $n '#ffffff' $n.png 64
    render $n '#000000' $n-template.png 44
    for s in 16 24 32 48; do render $n '#7f7f7f' /tmp/hearsay-$n-$s.png $s; done
    magick /tmp/hearsay-$n-16.png /tmp/hearsay-$n-24.png /tmp/hearsay-$n-32.png /tmp/hearsay-$n-48.png $n.ico
done
rsvg-convert -w 256 -h 256 -o app.png app.svg
render mic '#e01b24' listening.png 64
for s in 16 24 32 48; do render mic '#e01b24' /tmp/hearsay-l-$s.png $s; done
magick /tmp/hearsay-l-16.png /tmp/hearsay-l-24.png /tmp/hearsay-l-32.png /tmp/hearsay-l-48.png listening.ico
rm -f /tmp/hearsay-*.png
