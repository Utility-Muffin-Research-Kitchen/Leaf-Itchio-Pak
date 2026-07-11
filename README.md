# Leaf Itch.io Pak

Leaf-Itchio-Pak is UMRK's Leaf-only hard fork of
[carroarmato0/NextUI-Itchio-Pak](https://github.com/carroarmato0/NextUI-Itchio-Pak).
It browses itch.io, downloads compatible games and soundtracks, and integrates
the results into the Leaf library. This project is unofficial and is not
affiliated with or endorsed by itch.io.

The fork retains the upstream Go catalogue, network, archive, inventory,
settings, animated-GIF, paid-download, and soundtrack behavior while adapting
storage and runtime integration for Leaf. The active target is the Miniloong
Pocket 1 (`mlp1`) only. NextUI and the former TrimUI/Miyoo package lanes are not
supported.

The UI is in active migration to Catastrophe's box model. Until that bridge is
complete, the inherited renderer remains a temporary implementation detail;
the shipping design has Catastrophe as the sole GUI owner.

## Provenance and licensing

This repository preserves upstream history through release `v1.0.19`, commit
`42171a5a764ff341d581b6e3ec6cd02adb936eb7`. See [UPSTREAM.md](UPSTREAM.md) for
the audit ledger, [LICENSE](LICENSE) for the original MIT license, and
[THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) for dependency and bundled
asset notices.

The original project notes that it was developed with AI assistance. UMRK
retains that disclosure and records its own agent-assisted changes through Git
history and review.

## Build and test

Requirements for native macOS work:

- Go 1.22 or newer;
- `pkg-config`, SDL2, SDL2_image, and SDL2_ttf;
- a sibling `../Catastrophe` checkout.

The MLP1 lane requires Docker or Podman and the UMRK MLP1 toolchain image. It
layers the pinned `golang:1.22.12-bookworm` toolchain over that image and
cross-compiles with CGo for Linux arm64.

```sh
make test
make test-race
make native
make mac
make run-mac
make mlp1
make package-platform PLATFORM=mlp1
make package-mlp1
make package-smoke
```

`CATASTROPHE_DIR`, `MLP1_TOOLCHAIN_IMAGE`, `GO_IMAGE`, and
`CONTAINER_RUNTIME` can be overridden. `MIN_JAWAKA_VERSION` defaults to
`0.5.4`, the first planned launcher release containing the runtime, dual-Music,
and suspend-inhibitor contracts required by this app. Unknown package platforms
fail closed.

## Leaf package contract

`make package-mlp1` produces:

```text
build/mlp1/package/Itch-io.pak/
  bin/itchio-pak
  launch.sh
  pak.json
  res/icon.png
  res/fonts/...
  res/certs/ca-certificates.crt
  licenses/...
build/mlp1/Itch-io.mlp1.pak.zip
```

The archive contains one unambiguous `Itch-io.pak/` root. Package assembly is
reproducible, uses only regular FAT32-safe files, and does not include source,
Go caches, user state, debug profiles, or foreign-platform libraries.

The Pak consumes Leaf's launcher environment and writes durable app state to
`$USERDATA_PATH/Itch-io` and logs to `$LOGS_PATH/itchio-pak.log`. It preserves
the inherited `CAT_*` appearance snapshot and uses the packaged CA bundle.

## Installation policy

This app is optional. It is not part of Leaf's default staged apps, release
ZIPs, managed-app list, or bootstrap set. A manually cloned checkout can be
staged for development with:

```sh
make -C ../Leaf stage-app APP=Leaf-Itchio-Pak DEVICE=mlp1
```

The promoted distribution route will be Pak Rat. Production catalog metadata
must not be added until every verification phase in the cross-repository plan
has passed. The immutable `.pak.zip` is retained as a recovery/development
artifact.

## Development boundaries

- Product code, build logic, and packaging stay in this repository.
- Leaf may explicitly dispatch package/stage targets but does not rebuild the
  app or include it in default payloads.
- Catastrophe remains a generic C UI/runtime toolkit with no app-specific Go
  binding.
- Runtime paths come from Leaf's environment contract; package code does not
  choose a card by scanning arbitrary mounts.
- Pak Rat production metadata belongs to the final gated publication phase.
