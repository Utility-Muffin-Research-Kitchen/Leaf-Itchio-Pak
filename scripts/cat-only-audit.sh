#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

for removed in internal/renderer internal/theme; do
	if [[ -d "$removed" ]] && find "$removed" -type f -print -quit | grep -q .; then
		echo "cat-only-audit: legacy package still exists: $removed" >&2
		exit 1
	fi
done

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
