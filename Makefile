SHELL := /bin/bash

APP_VERSION ?= 0.1.0
MIN_JAWAKA_VERSION ?= 0.5.5
GIT_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || printf 0)
WORKSPACE_ROOT ?= $(abspath ..)
CATASTROPHE_DIR ?= $(WORKSPACE_ROOT)/Catastrophe
MLP1_TOOLCHAIN_IMAGE ?= ghcr.io/utility-muffin-research-kitchen/mlp1-toolchain:local
GO_IMAGE ?= docker.io/library/golang:1.22.12-bookworm
MLP1_BUILD_IMAGE ?= leaf-itchio-pak-mlp1-go1.22.12

export APP_VERSION MIN_JAWAKA_VERSION GIT_COMMIT SOURCE_DATE_EPOCH
export WORKSPACE_ROOT CATASTROPHE_DIR MLP1_TOOLCHAIN_IMAGE GO_IMAGE MLP1_BUILD_IMAGE

.DEFAULT_GOAL := native
.PHONY: test test-race cat-only-audit public-assets-check pakrat-metadata-check native mac run-mac run-cat-fixtures cat-fixture-snapshots cat-main-list-snapshots cat-input-snapshots public-screenshots mlp1 package-platform package-mlp1 package-smoke clean check-catastrophe check-sdl

test: cat-only-audit public-assets-check pakrat-metadata-check
	go test -count=1 -tags headless ./...

test-race: cat-only-audit public-assets-check pakrat-metadata-check
	go test -count=1 -race -tags headless ./...

cat-only-audit:
	./scripts/cat-only-audit.sh

public-assets-check:
	./scripts/public-assets-check.py

pakrat-metadata-check:
	./scripts/pakrat-metadata-check.py

check-catastrophe:
	@test -f "$(CATASTROPHE_DIR)/include/catastrophe.h" || { \
		echo "Catastrophe not found at $(CATASTROPHE_DIR) (set CATASTROPHE_DIR to its checkout)." >&2; \
		exit 1; \
	}

check-sdl:
	@pkg-config --exists sdl2 SDL2_image SDL2_ttf 2>/dev/null || { \
		echo "SDL2 dependencies not found. On macOS: brew install pkg-config sdl2 sdl2_image sdl2_ttf" >&2; \
		exit 1; \
	}

native: check-catastrophe check-sdl cat-only-audit
	./scripts/build.sh native

mac: check-catastrophe check-sdl cat-only-audit
	@case "$$(uname -s)" in Darwin) ;; *) echo "make mac requires macOS" >&2; exit 1 ;; esac
	./scripts/build.sh mac

run-mac: mac
	@mkdir -p "$(CURDIR)/build/mac/sdcard"
	ITCHIO_RES_DIR="$(CURDIR)/assets" \
		PLATFORM=mlp1 \
		SDCARD_PATH="$(CURDIR)/build/mac/sdcard" \
		SDCARD_PATHS="$(CURDIR)/build/mac/sdcard" \
		UMRK_PLATFORM_PATH="$(WORKSPACE_ROOT)/miniloong-launcher-switcher/device/mlp1" \
		USERDATA_PATH="$(CURDIR)/build/mac/userdata" \
		LOGS_PATH="$(CURDIR)/build/mac/logs" \
		"$(CURDIR)/build/mac/bin/itchio-pak"

run-cat-fixtures: mac
	CAT_ENV=DEV \
		CAT_WINDOW_WIDTH=960 CAT_WINDOW_HEIGHT=720 \
		CAT_FONT_PATH="$(CATASTROPHE_DIR)/res/font.ttf" \
		CAT_STATUS_ASSETS_DIR="$(CATASTROPHE_DIR)/res/assets" \
		ITCHIO_RES_DIR="$(CURDIR)/assets" \
		LOGS_PATH="$(CURDIR)/build/cat-fixtures/logs" \
		"$(CURDIR)/build/mac/bin/itchio-pak" --cat-fixtures

cat-fixture-snapshots: mac
	./scripts/cat-fixtures-smoke.sh

cat-main-list-snapshots: mac
	./scripts/cat-main-list-smoke.sh

cat-input-snapshots: mac
	./scripts/cat-input-smoke.sh

public-screenshots:
	./scripts/capture-public-screenshots.sh

mlp1: check-catastrophe cat-only-audit
	./scripts/build.sh mlp1

package-platform:
	@test -n "$(PLATFORM)" || { echo "usage: make package-platform PLATFORM=mlp1" >&2; exit 1; }
	@case "$(PLATFORM)" in \
		mlp1) $(MAKE) package-mlp1 ;; \
		*) echo "unsupported Leaf-Itchio-Pak platform: $(PLATFORM)" >&2; exit 1 ;; \
	esac

package-mlp1: mlp1
	./scripts/package.sh

package-smoke: package-mlp1
	./scripts/package-smoke.py \
		--package build/mlp1/package/Itch-io.pak \
		--archive build/mlp1/Itch-io.mlp1.pak.zip \
		--version "$(APP_VERSION)" \
		--min-jawaka-version "$(MIN_JAWAKA_VERSION)" \
		--toolchain-image "$(MLP1_TOOLCHAIN_IMAGE)"
	./scripts/launch-smoke.sh build/mlp1/package/Itch-io.pak/launch.sh

clean:
	rm -rf build
