# Upstream provenance

Leaf-Itchio-Pak is a hard fork of
[carroarmato0/NextUI-Itchio-Pak](https://github.com/carroarmato0/NextUI-Itchio-Pak).

| Field | Value |
|---|---|
| Upstream release | `v1.0.19` |
| Fork-point commit | `42171a5a764ff341d581b6e3ec6cd02adb936eb7` |
| Local fork-point tag | `upstream-v1.0.19` |
| Fork established | 2026-07-09 |
| Local repository | `Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak` |
| Last upstream review | 2026-07-09 |
| Next review | Before `v0.1.0`, or 2026-10-09 if earlier |

## Review record

| Review | Accepted | Rejected or deferred | Rationale |
|---|---|---|---|
| Initial fork | All history through `42171a5` | None | Establish the exact `v1.0.19` behavioral baseline before Leaf changes |
| `v1.0.25` inventory and naming (2026-09-25) | Adapted `79539ff` (superseded upload is an update), `dc94f66` (ask between alternative builds), `4915dde` and `c346eb0` (no rename inside one operation replaces another of its files) | Upstream's screen files and palette/tooling changes bundled with `c346eb0` | Each defect reproduced first with a failing test on Leaf `main`. Leaf's versions: PlayStation CUE/BIN and multi-disc sets stay one batch; names are reserved per operation and compared as FAT32 does; files whose unified names meet keep their original names, planned up front for ZIP and 7z after magic-byte classification; case-only duplicates fail the download before writing, or are skipped in an archive, including both Pico-8 paths |

Future reviews append a row here. Do not rewrite old decisions.

## Locally replaced subsystems

The Leaf port intentionally replaces the upstream renderer, input mapping,
power integration, runtime paths, system catalogue, inventory schema, device
build matrix, packaging, staging, and release distribution. Catalogue fetching,
downloads, content filtering, GIF behavior, API-key support, music extraction,
and screen-state behavior remain candidates for narrow upstream fixes after
review.

The `upstream` Git remote must continue to point to the source repository.
Upstream changes are reviewed and cherry-picked manually; this fork does not
promise merge compatibility because its device, packaging, filesystem, input,
power, and renderer contracts deliberately diverge for Leaf and Catastrophe.

When importing an upstream fix:

1. fetch `upstream` and identify the exact source commit;
2. review it against the Leaf runtime and dual-SD contracts;
3. cherry-pick or reimplement the narrow change;
4. record the source commit in the local commit message;
5. run the headless suite and all affected integration/device gates.

The original copyright and MIT license remain in `LICENSE`. UMRK changes are
distributed under the same license unless a file states otherwise.
