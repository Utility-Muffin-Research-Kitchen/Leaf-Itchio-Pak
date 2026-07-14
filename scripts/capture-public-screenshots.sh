#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEST="$ROOT/docs/screenshots"

make -C "$ROOT" cat-main-list-snapshots cat-input-snapshots
mkdir -p "$DEST"
rm -f "$DEST"/*.png

install -m 0644 "$ROOT/build/cat-main-list/ready-dark-hints-bump0.png" "$DEST/main-list.png"
install -m 0644 "$ROOT/build/cat-input/filter-960x720-hints1-bump0.png" "$DEST/filter-search.png"
install -m 0644 "$ROOT/build/cat-input/detail-960x720-hints1-bump0.png" "$DEST/detail-gallery.png"
install -m 0644 "$ROOT/build/cat-input/warning-960x720-hints1-bump0.png" "$DEST/content-warning.png"
install -m 0644 "$ROOT/build/cat-input/download-progress-960x720-hints1-bump0.png" "$DEST/download-progress.png"
install -m 0644 "$ROOT/build/cat-input/settings-960x720-hints1-bump0.png" "$DEST/settings.png"
install -m 0644 "$ROOT/build/cat-input/destination-source-960x720-hints1-bump0.png" "$DEST/dual-sd-destination.png"
install -m 0644 "$ROOT/build/cat-input/manage-list-960x720-hints1-bump0.png" "$DEST/downloaded-manage.png"

for screenshot in "$DEST"/*.png; do
    dimensions=$(/usr/bin/sips -g pixelWidth -g pixelHeight "$screenshot" | awk '/pixelWidth/ {w=$2} /pixelHeight/ {h=$2} END {print w "x" h}')
    if [ "$dimensions" != "960x720" ]; then
        echo "unexpected public screenshot dimensions: $screenshot ($dimensions)" >&2
        exit 1
    fi
done

printf 'public screenshots:\n'
shasum -a 256 "$DEST"/*.png
