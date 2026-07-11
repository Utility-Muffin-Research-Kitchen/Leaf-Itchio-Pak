#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
WORKSPACE_ROOT="${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}"
CATASTROPHE_DIR="${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}"
BIN="$REPO_DIR/build/mac/bin/itchio-pak"
OUT="$REPO_DIR/build/cat-main-list"

test -x "$BIN" || { echo "missing Mac binary: $BIN" >&2; exit 1; }
mkdir -p "$OUT/logs"

capture() {
    local name="$1" state="$2" width="$3" height="$4" hints="$5" bump="$6" background="$7" text="$8"
    local output="$OUT/$name.png"
    env \
        CAT_ENV=DEV \
        CAT_WINDOW_WIDTH="$width" CAT_WINDOW_HEIGHT="$height" \
        CAT_FONT_PATH="$CATASTROPHE_DIR/res/font.ttf" \
        CAT_STATUS_ASSETS_DIR="$CATASTROPHE_DIR/res/assets" \
        CAT_STATUS_SHOW_WIFI=0 CAT_STATUS_SHOW_BATTERY=0 \
        CAT_STATUS_SHOW_BATTERY_LEVEL=0 CAT_STATUS_SHOW_BLUETOOTH=0 \
        CAT_STATUS_CLOCK=hide CAT_FONT_BUMP="$bump" CAT_SHOW_HINTS="$hints" \
        CAT_COLOR_BACKGROUND="$background" CAT_COLOR_TEXT="$text" \
        CAT_COLOR_HINT='#7f879a' CAT_COLOR_HIGHLIGHT='#ec6b5e' CAT_COLOR_ACCENT='#28304a' \
        ITCHIO_RES_DIR="$REPO_DIR/assets" LOGS_PATH="$OUT/logs" \
        "$BIN" --cat-main-list --cat-main-list-state="$state" \
        --cat-main-list-frames=1 --cat-main-list-screenshot="$output"
}

capture ready-dark-hints-bump0 ready 960 720 1 0 '#141827' '#f7f2e8'
capture loading-dark-hints-bump0 loading 960 720 1 0 '#141827' '#f7f2e8'
capture error-dark-hints-bump0 error 960 720 1 0 '#141827' '#f7f2e8'
capture empty-dark-hints-bump0 empty 960 720 1 0 '#141827' '#f7f2e8'
capture ready-light-nohints-bump5 ready 1280 800 0 5 '#f2eadc' '#202536'

python3 - "$OUT" <<'PY'
import pathlib
import struct
import sys

root = pathlib.Path(sys.argv[1])
expected = {
    "ready-dark-hints-bump0.png": (960, 720),
    "loading-dark-hints-bump0.png": (960, 720),
    "error-dark-hints-bump0.png": (960, 720),
    "empty-dark-hints-bump0.png": (960, 720),
    "ready-light-nohints-bump5.png": (1280, 800),
}
for name, dimensions in expected.items():
    path = root / name
    data = path.read_bytes()
    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit(f"invalid Catastrophe main-list PNG: {path}")
    actual = struct.unpack(">II", data[16:24])
    if actual != dimensions:
        raise SystemExit(f"unexpected fixture size {actual}: {path}")
    if len(data) < 10000:
        raise SystemExit(f"main-list fixture is implausibly small: {path}")
    print(f"cat-main-list: {name} {actual[0]}x{actual[1]} {len(data)} bytes")
PY

echo "cat-main-list-smoke: PASS"
