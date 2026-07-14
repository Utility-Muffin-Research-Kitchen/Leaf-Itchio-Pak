#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

for removed in \
	internal/renderer internal/theme \
	.claude/skills docs/mockups graphify-out \
	lib/tg5040 lib/tg5050 lib/my355; do
	if [[ -d "$removed" ]] && find "$removed" -type f -print -quit | grep -q .; then
		echo "cat-only-audit: removed operational tree still contains files: $removed" >&2
		exit 1
	fi
done

if find cmd internal -type f -name '*.go' ! -name '*_test.go' -print0 | xargs -0 grep -n -E \
	'NextUITheme|Pico8Core|LastROMDirs|MigratePico8Files|ReadMigrateFormats|tg5040|tg5050|my355|/mnt/SDCARD|Tools/|\.pakz|minuisettings'; then
	echo "cat-only-audit: unsupported platform or migration logic remains in maintained Go source" >&2
	exit 1
fi

if grep -R -n -E --exclude='cat-only-audit.sh' \
	'tg5040|tg5050|my355|/mnt/SDCARD|Tools/|\.pakz|minuisettings' \
	scripts docker .github Makefile launch.sh pak.json 2>/dev/null; then
	echo "cat-only-audit: unsupported operational path remains in build, package, or support tooling" >&2
	exit 1
fi

if find internal/ui -maxdepth 1 -name 'screen_*.go' -print -quit | grep -q .; then
	echo "cat-only-audit: inherited screen implementation remains under internal/ui" >&2
	exit 1
fi

if grep -R -n -E --include='*.go' \
	'veandco/go-sdl2|internal/(renderer|theme)|type Screen interface|HandleEvent\(' cmd internal; then
	echo "cat-only-audit: legacy Go/SDL presentation reference remains" >&2
	exit 1
fi

if go list -deps ./cmd/itchio-pak | grep -E '/internal/(renderer|theme)$|veandco/go-sdl2'; then
	echo "cat-only-audit: production dependency graph still contains the legacy renderer" >&2
	exit 1
fi

echo "cat-only-audit: PASS"
