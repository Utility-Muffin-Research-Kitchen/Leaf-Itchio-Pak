#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}"
CATASTROPHE_DIR="${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}"
BIN="$REPO_DIR/build/mac/bin/itchio-pak"
OUT="$REPO_DIR/build/cat-proof"

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
    CAT_STATUS_CLOCK=24
    ITCHIO_RES_DIR="$REPO_DIR/assets"
    LOGS_PATH="$OUT/logs"
)

env "${common[@]}" \
    CAT_FONT_BUMP=0 CAT_SHOW_HINTS=1 \
    CAT_COLOR_BACKGROUND='#141827' CAT_COLOR_TEXT='#f7f2e8' \
    CAT_COLOR_HINT='#aab2c8' CAT_COLOR_HIGHLIGHT='#ec6b5e' \
    CAT_COLOR_ACCENT='#28304a' \
    "$BIN" --cat-proof --cat-proof-frames=5 \
    --cat-proof-screenshot="$OUT/dark-hints-bump0.png"

env "${common[@]}" \
    CAT_FONT_BUMP=5 CAT_SHOW_HINTS=0 \
    CAT_COLOR_BACKGROUND='#f7f2e8' CAT_COLOR_TEXT='#20253a' \
    CAT_COLOR_HINT='#626a7f' CAT_COLOR_HIGHLIGHT='#65ba67' \
    CAT_COLOR_ACCENT='#d9dfcc' \
    "$BIN" --cat-proof --cat-proof-frames=5 \
    --cat-proof-screenshot="$OUT/light-nohints-bump5.png"

python3 - "$OUT/dark-hints-bump0.png" "$OUT/light-nohints-bump5.png" <<'PY'
import pathlib
import struct
import sys

for name in sys.argv[1:]:
    path = pathlib.Path(name)
    data = path.read_bytes()
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"invalid Catastrophe proof PNG: {path}")
    width, height = struct.unpack(">II", data[16:24])
    if (width, height) != (960, 720):
        raise SystemExit(f"unexpected proof size {width}x{height}: {path}")
    if len(data) < 10000:
        raise SystemExit(f"proof image is implausibly small: {path}")
    print(f"cat-proof: {path.name} {width}x{height} {len(data)} bytes")
PY

echo "cat-proof-smoke: PASS"
