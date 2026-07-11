#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}"
CATASTROPHE_DIR="${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}"
BIN="$REPO_DIR/build/mac/bin/itchio-pak"
OUT="$REPO_DIR/build/cat-input"

test -x "$BIN" || { echo "missing Mac binary: $BIN" >&2; exit 1; }
mkdir -p "$OUT/logs"
rm -f "$OUT"/*.png

capture() {
    local screen="$1" width="$2" height="$3" hints="$4" bump="$5"
    local output="$OUT/${screen}-${width}x${height}-hints${hints}-bump${bump}.png"
    env \
        CAT_ENV=DEV CAT_WINDOW_WIDTH="$width" CAT_WINDOW_HEIGHT="$height" \
        CAT_FONT_PATH="$CATASTROPHE_DIR/res/font.ttf" \
        CAT_STATUS_ASSETS_DIR="$CATASTROPHE_DIR/res/assets" \
        CAT_STATUS_SHOW_WIFI=0 CAT_STATUS_SHOW_BATTERY=0 CAT_STATUS_CLOCK=hide \
        CAT_FONT_BUMP="$bump" CAT_SHOW_HINTS="$hints" \
        CAT_COLOR_BACKGROUND='#141827' CAT_COLOR_TEXT='#f7f2e8' \
        CAT_COLOR_HINT='#7f879a' CAT_COLOR_HIGHLIGHT='#ec6b5e' CAT_COLOR_ACCENT='#28304a' \
        ITCHIO_RES_DIR="$REPO_DIR/assets" LOGS_PATH="$OUT/logs" \
        "$BIN" --cat-input="$screen" --cat-input-frames=1 --cat-input-screenshot="$output"
}

capture filter 960 720 1 0
capture filter 1280 800 0 5
capture detail 960 720 1 0
capture detail 1280 800 0 5
capture warning 960 720 1 0
capture download-select 960 720 1 0
capture download-select 1280 800 0 5
capture download-progress 960 720 1 0
capture download-progress 1280 800 0 5
capture download-done 960 720 1 0
capture download-error 960 720 1 0
capture download-inhibit 960 720 1 0
capture download-cancelled 960 720 1 0
capture download-handoff 960 720 1 0
capture destination-source 960 720 1 0
capture destination-source 1280 800 0 5
capture destination-folder 960 720 1 0
capture destination-folder 1280 800 0 5
capture destination-music 960 720 1 0
capture manage-list 960 720 1 0
capture manage-list 1280 800 0 5
capture manage-confirm 960 720 1 0
capture rename-saves 960 720 1 0
capture rename-states 1280 800 0 5
capture settings 960 720 1 0
capture settings 1280 800 0 5
capture settings-confirm 960 720 1 0
capture moderation 960 720 1 0
capture moderation-tags 1280 800 0 5
capture about 960 720 1 0
capture refresh 960 720 1 0
capture refresh-done 1280 800 0 5

python3 - "$OUT" <<'PY'
import pathlib
import struct
import sys

root = pathlib.Path(sys.argv[1])
for path in sorted(root.glob("*.png")):
    data = path.read_bytes()
    expected = (1280, 800) if "1280x800" in path.name else (960, 720)
    if len(data) < 10000 or data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"invalid Catastrophe input PNG: {path}")
    actual = struct.unpack(">II", data[16:24])
    if actual != expected:
        raise SystemExit(f"unexpected fixture size {actual}: {path}")
    print(f"cat-input: {path.name} {actual[0]}x{actual[1]} {len(data)} bytes")
PY

echo "cat-input-smoke: PASS"
