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
| Last upstream review | 2026-10-03, through `v1.1.0-rc3` (`53a8da8`); adopted in stages, one review-record row per merged change |
| Next review | 2026-12-25 |

## Review record

| Review | Accepted | Rejected or deferred | Rationale |
|---|---|---|---|
| Initial fork | All history through `42171a5` | None | Establish the exact `v1.0.19` behavioral baseline before Leaf changes |
| `v1.0.25` toolchain (2026-09-25) | Adapted `32b86c5`: Go 1.27.1 for Leaf's own Makefile, build script, MLP1 Dockerfile, CI, and release lock; `go.mod` raised to `go 1.27.0` | Upstream toolchain images and build matrix | Go 1.22 no longer receives security fixes. Unlike upstream, the `go` line moves too, so Go 1.22 GODEBUG defaults (TLS/x509) do not survive the compiler bump |
| `v1.0.25` identity and server limits (2026-09-25) | Adapted `a4d6b8a` (real User-Agent, `crypto/tls` in place of uTLS, keeping the h2/h1 fallback transport), `00f1022` and `0b717e4` (shared per-host 429 cooldown, every 429 logged, 404/410 past page 1 ends a feed) | Upstream firmware/device reporting and its settings toggle; upstream's 5-minute Retry-After cap, feed-loop 429 retries, and URL-path logging | Requested in issue #4. Leaf sends only product, version and project URL. Retry-After is held between 2 and 60 s, the transport replays itch.io GET/HEAD at most 3 times and never a signed CDN URL (a download resolves a fresh one once instead), a refresh waits at most 2 minutes of cooldown, and logs name only the host because v1 API paths and signed CDN URLs carry secrets |
| `v1.0.25` API v2 backend (2026-09-25) | Adapted `3a6e6ae` (v2 upload listing, unfollowed download redirect, lazily created install session, `size`) and `62479b3` (`game_ids`) | `3a6e6ae`'s UI flows and free-game API listing (later PRs); its extension list; errors carrying server text | Requested in issue #4. **Cutover:** v1 authenticated endpoints are replaced outright with no runtime fallback to them, which would hide v2 errors and double requests; rollback is reinstalling the previous package through Pak Rat. Bundle counts come only from complete scans under the current key, and the resolver validates the location before anything reaches the CDN |
| `v1.0.25` install sessions (2026-09-25) | Adapted the flow half of `3a6e6ae`: one session per install shared by every upload of a listing, kept on the upload (`roms.InstallSession`, as upstream's `roms.DownloadSession`) | Upstream's screen changes; free-game API listing (PR 4c) | `Upload.Install` replaces `DownloadKeyID != ""` as the API test, so free API downloads work. The archive flow's single-ROM ZIP plan keeps the upload's source instead of the inspected CDN URL, which a free web download could not resolve and which expires |
| `v1.0.25` free-game API listing (2026-09-25) | Adapted the free-game half of `3a6e6ae`: with a key, free and name-your-own-price games are listed through the API in one install with no purchase ID | Upstream's fallback on every API error | A rate limit or cancellation no longer falls through to the web flow, the fallback runs at most once, and an API access error is not hidden behind the web flow's generic failure |
| `v1.1.0-rc1` QR sign-in (2026-09-26) | Adapted `559dc7b` (device authorization grant with PKCE), `28ca750` (sign-in screen, settings account row, legacy key removal), `7d25600` (docs) and `8ef782c` (paid games offer sign-in) | Upstream's client ID, firmware/device-info reporting and toggle, first-run account prompt, and 0644 config writes; `6147816` (API update checks) deferred to its own PR | Leaf uses its own OAuth client (`d3db7e0…`) and never a client secret. The key replaces typed API keys, is stored at 0600, redacted as `[TOKEN]`, and sent as the same Bearer credential, so the API v2 stack is unchanged. Polls go through the rate-limit transport, never outlive the code, and a rejected key signs out while network errors do not. Requires itch.io to approve the client for QR login before release |
| `v1.1.0-rc3` metadata and inventory (2026-10-03) | Adapted `7f17dae` and `fea9438` (bounded public game metadata), `6147816` and `225263e` (metadata-only API update checks and legacy baselines) | Full upstream renderer, global connectivity monitor, app updater, and clearing every pending file after one download | Leaf keeps HTML-only fields, its inventory schema and both storage sources. Current prices control download routing; anonymous checks use public pages only. Upload fingerprints flow through direct, multi-file and archive installs, and only the installed version is acknowledged. Account changes discard in-flight scans. |
| QR sign-in verification (2026-10-03) | Corrected instructions for the [official device-login contract](https://itch.io/docs/api/oauth): scan `verification_uri_complete` and match `user_code` on the phone | Full device sign-in, phone approval and live token exchange remain untested | The client approval prerequisite in the 2026-09-26 review is met. Leaf's registered redirect URI is `urn:itchio:poll`; a live `POST /oauth/device` returned HTTP 200 with the required fields, `expires_in=600` and `interval=5`, without logging codes or credentials. The bare `verification_uri` has no code entry, so manual-entry instructions and the unused URL field are removed |

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
