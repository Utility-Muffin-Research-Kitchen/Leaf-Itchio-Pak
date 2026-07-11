#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}"
CATASTROPHE_DIR="${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}"
BIN="$REPO_DIR/build/mac/bin/itchio-pak"
OUT="$REPO_DIR/build/cat-fixtures"

test -x "$BIN" || { echo "missing Mac binary: $BIN" >&2; exit 1; }
mkdir -p "$OUT/logs"

common=(
    CAT_ENV=DEV
    CAT_WINDOW_WIDTH=960
    CAT_WINDOW_HEIGHT=720
    CAT_FONT_PATH="$CATASTROPHE_DIR/res/font.ttf"
    CAT_STATUS_ASSETS_DIR="$CATASTROPHE_DIR/res/assets"
    CAT_STATUS_SHOW_WIFI=0
    CAT_STATUS_SHOW_BATTERY=0
    CAT_STATUS_SHOW_BATTERY_LEVEL=0
    CAT_STATUS_SHOW_BLUETOOTH=0
    CAT_STATUS_CLOCK=hide
    CAT_FONT_BUMP=2
    CAT_SHOW_HINTS=1
    CAT_COLOR_BACKGROUND='#141827'
    CAT_COLOR_TEXT='#f7f2e8'
    CAT_COLOR_HINT='#aab2c8'
    CAT_COLOR_HIGHLIGHT='#ec6b5e'
    CAT_COLOR_ACCENT='#28304a'
    ITCHIO_RES_DIR="$REPO_DIR/assets"
    LOGS_PATH="$OUT/logs"
)

names=(list overlays input gallery states)
for page in 0 1 2 3 4; do
    output="$OUT/${page}-${names[$page]}.png"
    env "${common[@]}" "$BIN" --cat-fixtures \
        --cat-fixture-page="$page" --cat-fixture-frames=1 \
        --cat-fixture-screenshot="$output"
done

python3 - "$OUT" <<'PY'
import pathlib
import struct
import sys

root = pathlib.Path(sys.argv[1])
files = sorted(root.glob("[0-4]-*.png"))
if len(files) != 5:
    raise SystemExit(f"expected five fixture PNGs, found {len(files)}")
for path in files:
    data = path.read_bytes()
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"invalid Catastrophe fixture PNG: {path}")
    width, height = struct.unpack(">II", data[16:24])
    if (width, height) != (960, 720):
        raise SystemExit(f"unexpected fixture size {width}x{height}: {path}")
    if len(data) < 10000:
        raise SystemExit(f"fixture image is implausibly small: {path}")
    print(f"cat-fixture: {path.name} {width}x{height} {len(data)} bytes")
PY

echo "cat-fixtures-smoke: PASS"
