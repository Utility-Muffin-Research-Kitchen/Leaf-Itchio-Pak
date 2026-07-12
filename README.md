# Leaf Itch.io Pak

Leaf-Itchio-Pak is UMRK's Leaf-only hard fork of
[carroarmato0/NextUI-Itchio-Pak](https://github.com/carroarmato0/NextUI-Itchio-Pak).
It browses itch.io, downloads compatible games and soundtracks, and integrates
them with the Leaf library. It targets only the Miniloong Pocket 1 (`mlp1`).

This project is unofficial and is not affiliated with or endorsed by itch.io.
It is an optional app: it is not included in Leaf's default package, staged-app
set, release archive, or bootstrap flow.

## Features

- Browse, search, filter, and sort itch.io console-homebrew feeds.
- Download anonymous free/pay-what-you-want uploads or owned paid uploads with
  an optional itch.io API key.
- Support GB, GBC, GBA, NES, Mega Drive, Pico-8, and PlayStation (`PSX`).
- Inspect ZIP and 7z archives before extraction, including mixed ROM/music
  archives and multi-file Pico-8 games.
- Install PlayStation CHD, PBP, CUE/BIN, ISO, IMG, MDF, TOC, CBN, and M3U sets.
- Choose either mounted SD card and an optional folder inside the canonical
  Leaf ROM or Music root.
- Generate source-local Jawaka artwork under
  `Images/<system>/<ROM stem>.png`, then request a library rescan.
- Preserve animated GIFs in the catalogue/detail UI while using a bounded PNG
  frame for launcher art.
- Download soundtracks for Disco Boy without requiring or installing Disco Boy.
- Manage installed files, app-owned artwork, unified names, saves, and states.
- Keep a usable timestamped catalogue cache when itch.io is unavailable.
- Use Catastrophe's box model and Leaf appearance/input/runtime contracts as the
  only production GUI path.

See the [user guide](docs/user-guide.md) for controls, settings, storage,
security, and troubleshooting.

## Installation

Pak Rat is the primary distribution route for a public release. The app must
not be added to Pak Rat until the repository plan's complete non-store and
local-store verification gates pass. Until that final publication phase, it
will not appear in the production Pak Rat catalogue.

Once published:

1. Open Pak Rat in Leaf.
2. Find **Itch.io** and install it.
3. Return to Leaf's Apps list and launch **Itch.io**.

The manual fallback is the immutable `Itch-io.mlp1.pak.zip` attached to the
[matching GitHub release](https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/releases).
Verify the published SHA-256, extract the archive, and copy its single
`Itch-io.pak` directory to `Apps/mlp1/` on the Leaf SD card:

```sh
unzip Itch-io.mlp1.pak.zip
rm -rf "$LEAF_SD/Apps/mlp1/Itch-io.pak"
cp -R Itch-io.pak "$LEAF_SD/Apps/mlp1/"
```

`$LEAF_SD` above means the mounted root of the Leaf card, not a device-side
hardcoded path. App updates do not remove downloads or durable settings because
those live outside the pak directory. Restart Leaf's launcher or reboot after a
manual install so Jawaka rescans Apps. The package requires Jawaka `0.5.4` or
newer and does not support other device/platform folders.

For developer-only ADB staging from the umbrella workspace:

```sh
make -C ../Leaf stage-app APP=Leaf-Itchio-Pak DEVICE=mlp1
```

This explicit target does not add the app to Leaf's default payload.

## Essential controls

| Control | Main behavior |
| --- | --- |
| D-pad Up/Down | Move selection or scroll text |
| D-pad Left/Right | Page or alphabetical jump; change the selected value where applicable |
| A | Open, select, toggle, or confirm |
| B | Back/cancel; exit only from the main list |
| Start | Open Settings from the main list or a game detail screen |
| Select | Open Filter on the main list; apply changes inside Filter |
| L1/R1 | Previous/next sort on the main list; page long lists or change gallery image on other screens |
| L2/R2 | Previous/next platform category on the main list |
| X | Manage an installed game from its detail screen; dismiss an update notice on the main list |
| Y | Clear staged search/filter values on the Filter screen |
| Menu | Reserved for Leaf; it does not exit or navigate the app |

Footer hints show the active subset. The standard Catastrophe keyboard is used
for search and API-key entry. Short/long power-button behavior remains under
the app's Jawaka-protected Leaf power flow while a transfer is active.

## Storage and dual SD cards

The pak consumes Leaf's sourced runtime environment. It never discovers cards
by scanning arbitrary mount points or writes into the release-managed
`.system/leaf` tree.

With **ROM Location = auto**, downloads use the primary source and canonical
system directory. With **ROM Location = ask**, the picker lists both configured
sources but disables a card that is not actually mounted. A chosen subfolder is
sandboxed below that source's canonical system root and may be remembered per
system. ROMs and their launcher artwork always remain on the same source:

```text
Roms/<system>/<optional folders>/<game files>
Images/<system>/<ROM stem>.png
```

The same source-aware rule applies to both `/mnt/sdcard` and
`/media/sdcard1` on the stock MLP1 layout, but code and package scripts use
`SDCARD_PATHS`, `ROMS_PATHS`, `IMAGES_PATHS`, and the other Leaf variables
instead of relying on those example mounts.

After a committed ROM download, rename, deletion, or artwork repair, the app
asks Jawaka for a non-destructive library rescan. It never opens or edits
Jawaka's `library.db` itself.

## Soundtracks and Disco Boy

**Music Download** defaults to `off` and can be changed to:

- `auto`: include supported music and write to the primary Music root;
- `ask`: choose which music entries to install;
- `off`: ignore soundtrack entries.

When music is enabled, **Music Location** can use the primary root automatically
or ask for a mounted card and folder. Disco Boy is optional and is not installed
or launched by this pak. Open or relaunch Disco Boy after a download so its
normal library scan sees the new files.

## API key and physical security

Anonymous browsing and free downloads do not require a key. A key enables the
Owned filter and authenticated downloads for games already present in the
account's itch.io library.

Enter or replace it in **Start > Settings > API Key**. Before the first save,
the app warns that the key is stored in App Data on the SD card. POSIX storage
can request owner-only mode, but FAT32 cannot protect the file from someone
with physical access to the card. The key is not encrypted at rest.

After saving, Settings shows only a short suffix. Editing always starts blank,
so the persisted key is never prefilled; newly typed characters remain visible
because fully masked controller entry was rejected during device testing. Keys,
authorization values, cookies, signed download URLs, account names, and known
runtime roots are redacted from local logs. Removing or replacing a key clears
the old owned-game authentication cache without deleting installed content.

## Content-warning defaults

Warnings do not remove files or silently hide matching games. They place a
confirmation screen in front of flagged detail content. The defaults are:

| Category | Default |
| --- | --- |
| Adult/suggestive content | On |
| Heavy themes | On |
| Substance use | On |
| Queer/LGBTQ+ themes | Off |

Each category and its individual tags can be changed under
**Settings > Content Moderation**. These tag-based advisories are best effort;
itch.io authors control their own tags and descriptions.

## App data, logs, and privacy

Durable state is stored under `$USERDATA_PATH/Itch-io`:

| File | Purpose |
| --- | --- |
| `config.json` | Settings and optional API key |
| `games_cache.json` | Timestamped public catalogue cache |
| `owned_cache.json` | URLs found through successful key validation |
| `inventory.json` | App-managed downloads, source identities, and artwork ownership |

Logs are local at `$LOGS_PATH/itchio-pak.log` and rotate before startup. The
stock Leaf defaults resolve these to `.userdata/mlp1/Itch-io` and
`.userdata/mlp1/logs/itchio-pak.log` at the primary SD root. Settings displays
the resolved App Data path. No telemetry or UMRK network endpoint exists.

Info logging is the default. Debug logging is intended for a short reproduction
of network/download failures; the same secret and private-path redaction applies
at both levels.

## Build and test

Native macOS work requires Go 1.22 or newer, `pkg-config`, SDL2, SDL2_image,
SDL2_ttf, and a sibling `../Catastrophe` checkout. The MLP1 lane requires Docker
or Podman plus the UMRK MLP1 toolchain image.

```sh
make test
make test-race
go test -count=1 -race ./...
make native
make mac
make run-mac
make cat-fixture-snapshots
make cat-main-list-snapshots
make cat-input-snapshots
make package-smoke
```

`make test` and `make test-race` include the durable Cat-only/remnant audit.
`CATASTROPHE_DIR`, `MLP1_TOOLCHAIN_IMAGE`, `GO_IMAGE`, and
`CONTAINER_RUNTIME` may be overridden. Unknown package platforms fail closed.

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

The archive contains one `Itch-io.pak/` root and only regular FAT32-safe files.
It excludes source, build caches, user state, debug profiles, and unsupported
platform libraries.

## Architecture and provenance

The Catastrophe CGo bridge is the sole owner of SDL initialization, rendering,
input, presentation, and shutdown. Retained Go catalogue/download workers are
headless backends and cannot be selected as an alternate UI. See the
[bridge contract](docs/catastrophe-bridge.md) and
[screen primitives](docs/catastrophe-primitives.md).

This repository preserves upstream history through release `v1.0.19`, commit
`42171a5a764ff341d581b6e3ec6cd02adb936eb7`. See
[UPSTREAM.md](UPSTREAM.md) for the audit ledger, [LICENSE](LICENSE) for the MIT
license, and [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md) for bundled
dependency/asset notices. Upstream changes are reviewed and selectively
cherry-picked or reimplemented; merge compatibility is not promised because
the runtime, filesystem, renderer, input, power, and release contracts differ.

The original project disclosed AI assistance. UMRK retains that disclosure and
records its own agent-assisted changes through Git history and review.

## Repository boundaries

- Product code, build logic, and packaging stay in this repository.
- Leaf may dispatch explicit build/stage targets but does not include this app
  in its default payload.
- Catastrophe remains a generic C UI/runtime toolkit with no app-specific Go
  binding or workaround.
- Pak Rat production metadata belongs only to the final gated publication
  phase.
