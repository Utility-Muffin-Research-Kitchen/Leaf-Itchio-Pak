#!/bin/sh
set -eu
PAK_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

PLATFORM="${PLATFORM:-mlp1}"
if [ -n "${UMRK_ENV_FILE:-}" ] && [ -f "$UMRK_ENV_FILE" ]; then
    . "$UMRK_ENV_FILE"
elif [ -n "${SDCARD_PATH:-}" ] &&
     [ -f "$SDCARD_PATH/.system/leaf/platforms/$PLATFORM/launcher/env.sh" ]; then
    . "$SDCARD_PATH/.system/leaf/platforms/$PLATFORM/launcher/env.sh"
else
    for _root in /mnt/sdcard /media/sdcard1; do
        _env="$_root/.system/leaf/platforms/$PLATFORM/launcher/env.sh"
        if [ -f "$_env" ]; then . "$_env"; break; fi
    done
    unset _root _env
fi

USERDATA_PATH="${USERDATA_PATH:-${SDCARD_PATH:-/mnt/sdcard}/.userdata/$PLATFORM}"
LOGS_PATH="${LOGS_PATH:-$USERDATA_PATH/logs}"
export PLATFORM USERDATA_PATH LOGS_PATH
export HOME="$USERDATA_PATH/Itch-io"
# Select bundled SDL2 libs for this device family.
# cpuinfo hwserial contains TG5050 on the Smart Pro S; all other TrimUI devices
# fall through to tg5040.  Miyoo devices expose /usr/miyoo.
if [ -d /usr/miyoo ]; then
    PLATFORM_LIB="$PAK_DIR/lib/my355"
elif grep -q "TG5050" /proc/cpuinfo 2>/dev/null; then
    PLATFORM_LIB="$PAK_DIR/lib/tg5050"
else
    PLATFORM_LIB="$PAK_DIR/lib/tg5040"
fi

# Remove stale versioned SDL2 files left by previous pak versions.  Only the
# SONAME files (libSDL2-2.0.so.0, libSDL2_ttf-2.0.so.0) are needed at runtime;
# the versioned siblings are never referenced directly by the dynamic linker.
rm -f "$PLATFORM_LIB"/libSDL2-2.0.so.0.* \
      "$PLATFORM_LIB"/libSDL2_ttf-2.0.so.0.* 2>/dev/null || true

# Prefer the device-native SDL2 when available (it is tuned for the device's
# display and audio backends).  The bundled LoveRetro SDL2 in PLATFORM_LIB
# serves as a working fallback on devices where the native path is absent.
NATIVE_SDL_LIB=""
for _d in /usr/trimui/lib /usr/miyoo/lib /usr/lib /usr/local/lib; do
    if [ -f "$_d/libSDL2-2.0.so.0" ]; then
        NATIVE_SDL_LIB="$_d"
        break
    fi
done
unset _d
export LD_LIBRARY_PATH="${NATIVE_SDL_LIB:+$NATIVE_SDL_LIB:}$PLATFORM_LIB:${LD_LIBRARY_PATH:-}"
export PATH="$PAK_DIR:$PATH"
# The device has no system CA certificate store; point Go's TLS stack at the
# bundle we ship so HTTPS requests to itch.io can be verified correctly.
export SSL_CERT_FILE="$PAK_DIR/assets/ca-certificates.crt"
mkdir -p "$HOME"
cd "$PAK_DIR"
# Optional profiling flags written by ./scripts/debug.sh profile commands.
# Absent in normal operation; present only during a profiling session.
# Word-splitting is intentional — the file contains space-separated flags.
PROFILE_FLAGS=""
if [ -f "$PAK_DIR/.profile-flags" ]; then
    PROFILE_FLAGS="$(cat "$PAK_DIR/.profile-flags")"
fi
# shellcheck disable=SC2086
exec "$PAK_DIR/itchio-pak" $PROFILE_FLAGS "$@"
