#!/bin/sh
set -eu

LAUNCH_SOURCE=${1:-launch.sh}
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/leaf-itchio-launch.XXXXXX")
TMP_ROOT=$(CDPATH= cd -- "$TMP_ROOT" && pwd -P)
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

PAK_DIR="$TMP_ROOT/Itch-io.pak"
CARD_DIR="$TMP_ROOT/card primary"
USERDATA_DIR="$CARD_DIR/.userdata/mlp1"
LOGS_DIR="$USERDATA_DIR/logs"
ENV_FILE="$TMP_ROOT/leaf env.sh"
mkdir -p "$PAK_DIR/bin" "$PAK_DIR/res/certs" "$CARD_DIR"
cp "$LAUNCH_SOURCE" "$PAK_DIR/launch.sh"
: > "$PAK_DIR/res/certs/ca-certificates.crt"

printf '%s\n' \
    "SDCARD_PATH='$CARD_DIR'" \
    "USERDATA_PATH='$USERDATA_DIR'" \
    "LOGS_PATH='$LOGS_DIR'" \
    "PLATFORM=mlp1" \
    > "$ENV_FILE"

printf '%s\n' \
    '#!/bin/sh' \
    'set -eu' \
    'if [ "${1:-}" = "--rotate-log-only" ]; then exit 0; fi' \
    'printf "HOME=%s\n" "$HOME"' \
    'printf "LOGS_PATH=%s\n" "$LOGS_PATH"' \
    'printf "SSL_CERT_FILE=%s\n" "$SSL_CERT_FILE"' \
    'printf "ITCHIO_RES_DIR=%s\n" "$ITCHIO_RES_DIR"' \
    'printf "CAT_THEME_NAME=%s\n" "${CAT_THEME_NAME:-}"' \
    'printf "ARG=%s\n" "${1:-}"' \
    'printf "stderr-marker\n" >&2' \
    > "$PAK_DIR/bin/itchio-pak"
chmod 755 "$PAK_DIR/launch.sh" "$PAK_DIR/bin/itchio-pak"

UMRK_ENV_FILE="$ENV_FILE" CAT_THEME_NAME=smoke-theme "$PAK_DIR/launch.sh" "argument with spaces"

LOG_FILE="$LOGS_DIR/itchio-pak.log"
test -d "$USERDATA_DIR/Itch-io"
test -f "$LOG_FILE"
grep -F "HOME=$USERDATA_DIR/Itch-io" "$LOG_FILE" >/dev/null
grep -F "LOGS_PATH=$LOGS_DIR" "$LOG_FILE" >/dev/null
grep -F "SSL_CERT_FILE=$PAK_DIR/res/certs/ca-certificates.crt" "$LOG_FILE" >/dev/null
grep -F "ITCHIO_RES_DIR=$PAK_DIR/res/fonts" "$LOG_FILE" >/dev/null
grep -F "CAT_THEME_NAME=smoke-theme" "$LOG_FILE" >/dev/null
grep -F "ARG=argument with spaces" "$LOG_FILE" >/dev/null
grep -F "stderr-marker" "$LOG_FILE" >/dev/null

if UMRK_ENV_FILE="$TMP_ROOT/missing-env.sh" "$PAK_DIR/launch.sh" >/dev/null 2>&1; then
    echo "launch-smoke: explicit missing UMRK_ENV_FILE must fail" >&2
    exit 1
fi

echo "launch-smoke: PASS"
