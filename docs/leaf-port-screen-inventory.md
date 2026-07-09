# Leaf port screen and interaction inventory

This document freezes the public UI surface at the `v1.0.19` fork point before
the SDL renderer is replaced by the Catastrophe box-model bridge. It describes
behavior to preserve, not a promise of NextUI layout compatibility.

## Shared screen contract

Every production screen implements `ui.Screen`:

- `Draw(*renderer.Renderer)` draws the current state;
- `HandleEvent(sdl.Event) Screen` processes one event and returns self, a new
  screen, the previous screen, or `nil` to exit;
- `NeedsRedraw()` reports active progress or animation;
- `HasPendingAnimation()` reports a delayed animation that still needs a timed
  wake-up.

Screens performing destructive or network work may also implement
`BusyChecker.IsBusy()`. The main loop blocks sleep, shutdown, and input while a
busy screen is completing a transaction.

Catastrophe migration must preserve these state and transition semantics while
replacing SDL events with semantic Leaf actions and replacing direct renderer
calls with retained box trees.

## Public constructors and transitions

| Constructor | Responsibility | Primary transitions/actions |
|---|---|---|
| `NewListScreen` | Catalogue, paging, sort/filter/search, badges | Detail, filter, settings, refresh, exit |
| `NewDetailScreen` | Metadata, artwork/GIF, screenshots, warnings, QR | Fetch uploads, manage files, rename migration, settings, back |
| `NewFilterScreen` | Platform/sort/search overlay | Keyboard, apply, clear, cancel |
| `NewKeyboardScreen` | Controller text entry | Confirm callback or cancel callback, then previous |
| `NewSettingsScreen` | Key, naming, music, filters, cache, theme, about | Key test, keyboard, moderation, Pico-8 migration, refresh, about |
| `NewContentModerationScreen` | Content-filter categories | Adult, queer, or heavy-theme tag screen; back |
| `NewAdultContentFilterScreen` | Adult-content tag policy | Toggle tags, skip group, back |
| `NewQueerContentFilterScreen` | Queer-content tag policy | Toggle tags, skip group, back |
| `NewHeavyThemesFilterScreen` | Heavy-theme/substance tag policy | Toggle tags, skip group, back |
| `NewAboutScreen` | Version and attribution | Back |
| `NewKeyTestScreen` | Validate API key and load owned games | Success/failure result, back |
| `NewFetchUploadsScreen` | Resolve free/owned upload choices | Purchase picker, ROM picker, format picker, ZIP inspect, direct or multi-download |
| `NewPurchasePickerScreen` | Select an owned purchase/bundle | ROM/format/location picker or download |
| `NewROMPickerScreen` | Select a known ROM upload | ZIP inspect, location picker, direct download |
| `NewFormatPickerScreen` | Classify an unknown upload | Auto-detect, ZIP inspect, location picker, direct download |
| `NewAutoDetectScreen` | Inspect an upload asynchronously | ZIP inspect, format picker, direct download, cancel/back |
| `NewZIPInspectScreen` | Inspect ZIP/7z contents and choose a plan | ZIP contents, music location, ZIP download, direct download |
| `NewZIPContentsScreen` | Select archive entries | Music location or ZIP download |
| `NewLocationPickerScreen` | Select ROM destination | Enter/up/confirm/cancel, then download |
| `NewMusicLocationPickerScreen` | Select music destination | Enter/up/confirm/cancel, then ZIP download |
| `NewDownloadScreen` | Stream one upload transaction | Progress/result, then back |
| `NewMultiROMDownloadScreen` | Download multiple selected uploads | Progress/result, then back |
| `NewZIPDownloadScreen` | Download/extract a selected archive plan | Progress/result, then back |
| `NewManageDownloadsScreen` | Inspect/delete installed files and naming mode | Delete confirmation, rename migration, back |
| `NewMigrateFlowScreen` | Guided ROM/art/save/state rename transaction | Collision/overwrite decisions, completion, back |
| `NewPico8CoreMigrateScreen` | Move Pico-8 content between core roots | Confirm/cancel/progress/result |
| `NewCacheRefreshScreen` | Cancel-safe catalogue rebuild | Progress, cancel, result/back |
| `NewDevStartScreen` | Native-development screen selector | Selected screen/list/settings; development only |

## Control vocabulary

The current UI uses D-pad navigation plus `A`, `B`, `X`, `Y`, `L1`, `R1`,
`SELECT`, `START`, and the power key. Labels are not stable across the port:
Catastrophe must bind semantic actions first, then render Leaf-appropriate
footer hints. Required action families are:

- move, page, jump, scroll, and change selection;
- accept/open/toggle and back/cancel/exit;
- apply, clear/delete, edit, and settings;
- previous/next screenshot, filter, sort, and source;
- sleep and shutdown, with busy-state inhibition.

## Worker and wake-up paths

The SDL baseline wakes the render loop with `sdl.UserEvent`. The following work
must become typed bridge events rather than calling Catastrophe from workers:

- catalogue page fetch and full-cache replacement;
- owned-games refresh and inventory update service;
- cover-art/GIF decode and texture upload readiness;
- game-detail and screenshot loading;
- upload lookup, archive inspection, extraction, and downloads;
- API-key validation, Pico-8 migration, and cache refresh;
- power manager wake notifications.

Only the main UI thread may mutate or render the retained Catastrophe tree.
Workers publish immutable payloads to a bounded event queue; stale payloads are
discarded using a screen/request generation id.

## Renderer surface to replace

The screen layer currently depends on immediate-mode calls for headers,
footers, text measurement/wrapping/truncation, rectangles, clip regions,
badges, pills, triangles, tag pills, QR textures, cover/screenshot textures,
and animated scrolling text. The Catastrophe bridge needs equivalent app-local
widgets or models for each capability. GIF animation stays a first-class image
model and must not be flattened to a static-only feature.

## Baseline evidence

At the fork point, the canonical containerized Linux race suite and the native
macOS headless race suite both pass all 11 packages. Existing tests already
characterize feed/cache cancellation, owned and free URL resolution, content
filters, GIF compositing/animation, filename collision guards, ROM/save/state
migration, archive inspection, inventory badges, settings persistence, and
secret redaction.

Still required before Phase 1 closes:

- focused feed-code/system classification tests;
- nested archive, 7z, multi-ROM, and soundtrack transaction fixtures;
- signed-URL redaction tests and log scans;
- explicit cancellation/no-partial-write tests for every cache/download path;
- native baseline memory/latency measurements and animated-GIF observation.
