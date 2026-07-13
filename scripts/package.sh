#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_DIR"

APP_VERSION=${APP_VERSION:-0.1.0}
MIN_JAWAKA_VERSION=${MIN_JAWAKA_VERSION:-0.5.5}
WORKSPACE_ROOT=${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}
CATASTROPHE_DIR=${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}
PACKAGE_ROOT=build/mlp1/package
PACKAGE_DIR=$PACKAGE_ROOT/Itch-io.pak
ARCHIVE=build/mlp1/Itch-io.mlp1.pak.zip

test -x build/mlp1/bin/itchio-pak || { echo "missing MLP1 binary; run make mlp1" >&2; exit 1; }

python3 - "$APP_VERSION" "$MIN_JAWAKA_VERSION" <<'PY'
import json
import pathlib
import sys

manifest = json.loads(pathlib.Path("pak.json").read_text(encoding="utf-8"))
if manifest.get("pak_version") != sys.argv[1]:
    raise SystemExit(f"pak.json version {manifest.get('pak_version')!r} != build version {sys.argv[1]!r}")
if manifest.get("min_jawaka_version") != sys.argv[2]:
    raise SystemExit("pak.json min_jawaka_version disagrees with the build contract")
PY

rm -rf "$PACKAGE_ROOT" "$ARCHIVE"
mkdir -p "$PACKAGE_DIR/bin" "$PACKAGE_DIR/res/fonts" "$PACKAGE_DIR/res/certs" "$PACKAGE_DIR/licenses"

cp build/mlp1/bin/itchio-pak "$PACKAGE_DIR/bin/itchio-pak"
cp launch.sh "$PACKAGE_DIR/launch.sh"
cp pak.json "$PACKAGE_DIR/pak.json"
cp pak/res/icon.png "$PACKAGE_DIR/res/icon.png"
cp assets/ca-certificates.crt "$PACKAGE_DIR/res/certs/ca-certificates.crt"
cp LICENSE "$PACKAGE_DIR/licenses/LICENSE"
cp "$CATASTROPHE_DIR/LICENSE" "$PACKAGE_DIR/licenses/Catastrophe-LICENSE"
cp THIRD-PARTY-LICENSES.md "$PACKAGE_DIR/licenses/THIRD-PARTY-LICENSES.md"

for font in font.ttf font_fallback_arabic.ttf font_fallback_devanagari.ttf font_fallback_emoji.ttf font_fallback_hebrew.ttf font_fallback_thai.ttf; do
    cp "assets/$font" "$PACKAGE_DIR/res/fonts/$font"
done
for font in font_fallback_arabic font_fallback_devanagari font_fallback_hebrew font_fallback_thai; do
    cp assets/Apache-2.0-NotoSans.txt "$PACKAGE_DIR/res/fonts/$font.LICENSE.txt"
done
cp assets/OFL-1.1-NotoSansJP.txt "$PACKAGE_DIR/res/fonts/font.LICENSE.txt"
cp assets/OFL-1.1-NotoEmoji.txt "$PACKAGE_DIR/res/fonts/font_fallback_emoji.LICENSE.txt"

chmod 755 "$PACKAGE_DIR/launch.sh" "$PACKAGE_DIR/bin/itchio-pak"
find "$PACKAGE_DIR" -type f ! -name launch.sh ! -path '*/bin/itchio-pak' -exec chmod 644 {} +

python3 - "$PACKAGE_DIR" "$ARCHIVE" <<'PY'
import pathlib
import stat
import sys
import zipfile

package = pathlib.Path(sys.argv[1])
archive = pathlib.Path(sys.argv[2])
with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
    for path in sorted(package.rglob("*")):
        if not path.is_file():
            continue
        relative = pathlib.PurePosixPath(package.name) / path.relative_to(package)
        info = zipfile.ZipInfo(str(relative), date_time=(1980, 1, 1, 0, 0, 0))
        mode = 0o755 if path.name == "launch.sh" or relative.as_posix().endswith("/bin/itchio-pak") else 0o644
        info.external_attr = (stat.S_IFREG | mode) << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        info.create_system = 3
        zf.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
PY

printf 'Packaged %s\nArchive  %s\n' "$PACKAGE_DIR" "$ARCHIVE"
