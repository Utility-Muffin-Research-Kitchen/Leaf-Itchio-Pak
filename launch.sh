#!/bin/sh
set -eu

PAK_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PLATFORM=${PLATFORM:-mlp1}

if [ -n "${UMRK_ENV_FILE:-}" ]; then
    if [ ! -f "$UMRK_ENV_FILE" ]; then
        echo "Leaf runtime environment not found: $UMRK_ENV_FILE" >&2
        exit 1
    fi
    # shellcheck disable=SC1090
    . "$UMRK_ENV_FILE"
else
    _sdcard=${SDCARD_PATH:-/mnt/sdcard}
    _env="$_sdcard/.system/leaf/platforms/$PLATFORM/launcher/env.sh"
    if [ -f "$_env" ]; then
        # shellcheck disable=SC1090
        . "$_env"
    fi
    unset _sdcard _env
fi

SDCARD_PATH=${SDCARD_PATH:-/mnt/sdcard}
USERDATA_PATH=${USERDATA_PATH:-$SDCARD_PATH/.userdata/$PLATFORM}
LOGS_PATH=${LOGS_PATH:-$USERDATA_PATH/logs}
HOME="$USERDATA_PATH/Itch-io"
SSL_CERT_FILE="$PAK_DIR/res/certs/ca-certificates.crt"
ITCHIO_RES_DIR="$PAK_DIR/res/fonts"
LOG_FILE="$LOGS_PATH/itchio-pak.log"

export PLATFORM SDCARD_PATH USERDATA_PATH LOGS_PATH HOME SSL_CERT_FILE ITCHIO_RES_DIR
mkdir -p "$HOME" "$LOGS_PATH"

# The application owns rotation so shell redirection cannot split a run across
# the pre- and post-rotation files.
"$PAK_DIR/bin/itchio-pak" --rotate-log-only
export ITCHIO_LOG_PREPARED=1

cd "$PAK_DIR"

# Normal launcher starts use the in-progress Catastrophe screen graph so an
# explicitly staged development pak exercises the port under test. Arguments
# are left untouched for fixture/smoke routes. Set ITCHIO_UI_MODE=legacy as a
# temporary escape hatch until the legacy renderer is deleted after cutover.
if [ "$#" -eq 0 ] && [ "${ITCHIO_UI_MODE:-cat}" = "cat" ]; then
    set -- --cat-live-list
fi
exec "$PAK_DIR/bin/itchio-pak" "$@" >>"$LOG_FILE" 2>&1
