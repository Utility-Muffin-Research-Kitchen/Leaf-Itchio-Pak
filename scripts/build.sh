#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_DIR"

APP_VERSION=${APP_VERSION:-0.1.0}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || printf 0)}
WORKSPACE_ROOT=${WORKSPACE_ROOT:-$(CDPATH= cd -- "$REPO_DIR/.." && pwd)}
CATASTROPHE_DIR=${CATASTROPHE_DIR:-$WORKSPACE_ROOT/Catastrophe}
MLP1_TOOLCHAIN_IMAGE=${MLP1_TOOLCHAIN_IMAGE:-ghcr.io/utility-muffin-research-kitchen/mlp1-toolchain:local}
GO_IMAGE=${GO_IMAGE:-docker.io/library/golang:1.22.12-bookworm}
MLP1_BUILD_IMAGE=${MLP1_BUILD_IMAGE:-leaf-itchio-pak-mlp1-go1.22.12}

ldflags() {
    printf '%s' "-s -w -buildid= -X main.version=$APP_VERSION -X main.gitCommit=$GIT_COMMIT -X github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/ui.appVersion=$APP_VERSION"
}

build_host() {
    output=$1
    mkdir -p "$(dirname -- "$output")"
    CGO_ENABLED=1 go build \
        -trimpath \
        -buildvcs=false \
        -ldflags "$(ldflags)" \
        -o "$output" \
        ./cmd/itchio-pak
    printf 'Built %s (%s, %s)\n' "$output" "$APP_VERSION" "$GIT_COMMIT"
}

detect_runtime() {
    case ${CONTAINER_RUNTIME:-} in
        docker|podman) printf '%s\n' "$CONTAINER_RUNTIME" ;;
        '')
            if command -v docker >/dev/null 2>&1; then printf 'docker\n'
            elif command -v podman >/dev/null 2>&1; then printf 'podman\n'
            else printf '\n'
            fi
            ;;
        *) echo "unsupported CONTAINER_RUNTIME: $CONTAINER_RUNTIME" >&2; exit 2 ;;
    esac
}

build_mlp1_container() {
    flags_env=/opt/mlp1-toolchain/umrk/mlp1-build-flags.env
    test -f "$flags_env" || { echo "missing MLP1 flag contract: $flags_env" >&2; exit 1; }
    # shellcheck disable=SC1090
    . "$flags_env"

    export CGO_ENABLED=1 GOOS=linux GOARCH=arm64 GOTOOLCHAIN=local
    export CC=${CC:-aarch64-buildroot-linux-gnu-gcc}
    export CXX=${CXX:-aarch64-buildroot-linux-gnu-g++}
    export PKG_CONFIG_SYSROOT_DIR=${PKG_CONFIG_SYSROOT_DIR:-$SYSROOT}
    export PKG_CONFIG_LIBDIR="$SYSROOT/usr/lib/pkgconfig:$SYSROOT/usr/share/pkgconfig"
    export CGO_CFLAGS="$UMRK_MLP1_PROFILE_CFLAGS"
    export CGO_CXXFLAGS="$UMRK_MLP1_PROFILE_CXXFLAGS"
    export CGO_LDFLAGS="$UMRK_MLP1_PROFILE_LDFLAGS"
    export GOCACHE=${GOCACHE:-/go-cache/build}
    export GOMODCACHE=${GOMODCACHE:-/go-cache/mod}

    mkdir -p build/mlp1/bin "$GOCACHE" "$GOMODCACHE"
    go build \
        -a \
        -tags netgo \
        -trimpath \
        -buildvcs=false \
        -ldflags "$(ldflags)" \
        -o build/mlp1/bin/itchio-pak \
        ./cmd/itchio-pak
    "$READELF" -l build/mlp1/bin/itchio-pak | grep -q '/lib/ld-linux-aarch64.so.1'
    printf 'Built build/mlp1/bin/itchio-pak (%s, %s)\n' "$APP_VERSION" "$GIT_COMMIT"
}

case ${1:-} in
    native)
        build_host build/native/bin/itchio-pak
        ;;
    mac)
        test "$(uname -s)" = Darwin || { echo "mac target requires macOS" >&2; exit 1; }
        build_host build/mac/bin/itchio-pak
        ;;
    mlp1)
        runtime=$(detect_runtime)
        test -n "$runtime" || { echo "docker or podman is required for make mlp1" >&2; exit 1; }
        mkdir -p .go_cache/build-cache .go_cache/mod-cache
        "$runtime" build \
            --build-arg "TOOLCHAIN_IMAGE=$MLP1_TOOLCHAIN_IMAGE" \
            --build-arg "GO_IMAGE=$GO_IMAGE" \
            -t "$MLP1_BUILD_IMAGE" \
            -f docker/Dockerfile.mlp1 .
        "$runtime" run --rm \
            -v "$WORKSPACE_ROOT:/workspace" \
            -v "$REPO_DIR/.go_cache:/go-cache" \
            -w /workspace/Leaf-Itchio-Pak \
            -e "APP_VERSION=$APP_VERSION" \
            -e "GIT_COMMIT=$GIT_COMMIT" \
            -e "SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH" \
            -e CATASTROPHE_DIR=/workspace/Catastrophe \
            "$MLP1_BUILD_IMAGE" \
            ./scripts/build.sh mlp1-container
        ;;
    mlp1-container)
        build_mlp1_container
        ;;
    *)
        echo "usage: $0 native|mac|mlp1" >&2
        exit 2
        ;;
esac
