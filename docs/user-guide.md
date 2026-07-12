# Leaf Itch.io Pak user guide

This guide covers Leaf-Itchio-Pak `0.1.x` on the Miniloong Pocket 1. The app is
unofficial and is not affiliated with or endorsed by itch.io.

## Availability and installation

The public distribution route is Pak Rat, but the app is intentionally absent
from the production catalogue until all verification gates pass. It is never a
default Leaf app.

After publication, install **Itch.io** from Pak Rat. If Pak Rat is unavailable,
download the matching `Itch-io.mlp1.pak.zip` GitHub release asset, verify its
published SHA-256, and extract its single `Itch-io.pak` directory to
`Apps/mlp1/` on the Leaf SD card. Do not rename the pak directory or place it in
another platform folder. Jawaka `0.5.4` or newer is required.

## Browsing

The main list combines the cached catalogue with newly fetched data. The header
shows the active platform, sort, and cache age/date. An existing cache remains
usable if refresh fails.

- Up/Down moves one game.
- Left/Right jumps to the previous/next initial in A-Z or Z-A sort; with other
  sorts it moves by one visible page.
- L1/R1 changes sort.
- L2/R2 changes platform category.
- Select opens Filter; Select applies staged filter changes.
- Y clears staged search, platform, and sort values inside Filter.
- A opens the selected game.
- B exits from the main list. Menu remains a Leaf control and does not exit.

The supported categories are All, GB, GBC, GBA, NES, Mega Drive, Pico-8, and
PlayStation. Sort choices are RSS, A-Z, Z-A, Newest, Free, Paid, Downloaded, and
Owned. Owned requires a validated API key.

## Detail and gallery

Up/Down scrolls the description. Left/Right or L1/R1 moves through the cover,
animated GIF, and screenshots. A begins the available download flow. X opens
Manage when the game has installed files. Start opens Settings and B returns to
the main list.

A tag-based content warning must be acknowledged before flagged detail content
is shown. Warning categories can be changed in Settings; they do not delete or
silently remove catalogue entries.

## Download flow

The app distinguishes directly supported ROMs, archives, music, and unknown
formats. Depending on Settings and the upload, it may ask for:

1. a purchase or upload;
2. an archive subset or format;
3. a mounted SD card;
4. a folder inside the canonical system or Music root;
5. final confirmation of every relative output path.

The destination is revalidated immediately before transfer and before later
batch members. If the selected card is removed, the operation fails before
writing outside the sealed plan. Partial transfer files use the target directory
and are removed on failure/cancellation; committed files are recorded in
inventory.

After a ROM transaction commits, the app requests a Jawaka library rescan. The
result screen distinguishes requested, queued, and failed scans. A scan failure
does not remove the installed ROM; use Leaf's Rescan action if needed.

## Supported game formats

| System | Accepted formats |
| --- | --- |
| Game Boy | `.gb` |
| Game Boy Color | `.gbc` |
| Game Boy Advance | `.gba` |
| NES | `.nes` |
| Mega Drive | `.md`, `.gen`, `.smd` |
| Pico-8 | `.p8`, `.p8.png`, multi-file archives |
| PlayStation | `.cbn`, `.chd`, `.cue`/`.bin`, `.img`, `.iso`, `.mdf`, `.pbp`, `.toc`, `.m3u` |
| Archives | `.zip`, `.7z` with inspected supported content |

PlayStation support files such as BIN are installed with their descriptor but
are not indexed as separate games. Descriptor/playlist names are preserved when
renaming could break internal references.

## Dual-SD destinations

Leaf supplies the ordered source list and canonical system catalogue. The
primary card is source 1 and the optional second card is source 2. A configured
but unmounted card is shown as **Not mounted** and cannot be selected.

**ROM Location = auto** uses the primary canonical system directory.
**ROM Location = ask** lets you select a mounted card and safe subfolder. The
choice can be remembered independently per system. Artwork is always written to
the matching source's canonical `Images/<system>` directory.

**Music Location = auto** uses the primary Music root. **ask** provides the same
mounted-card/folder picker for music. Remembered destinations can be cleared
without deleting downloads.

## Settings and defaults

| Setting | Default | Choices/behavior |
| --- | --- | --- |
| ROM Selection | `auto` | Automatically choose or ask among supported uploads |
| ROM Location | `auto` | Primary canonical directory or ask for card/folder |
| Music Download | `off` | `off`, `auto`, or `ask` |
| Music Location | `auto` | Primary Music root or ask for card/folder |
| Use game title | On | Use the safe itch.io title where renaming is supported |
| Log Level | Info | Info or Debug |
| Adult warnings | On | Master switch plus individual tags |
| Heavy-theme warnings | On | Master switch plus individual tags |
| Substance-use warnings | On | Master switch plus individual tags |
| Queer/LGBTQ+ warnings | Off | Master switch plus individual tags |

**Refresh Game List** rebuilds the public catalogue cache without replacing a
working cache with partial results. **Update Inventory** checks missing artwork,
removed upstream games, and newly offered uploads without deleting local files.
**Clear Image Cache** clears decoded in-memory cover/GIF frames; remote images
are fetched again when needed.

## API key

Free browsing/downloads work without a key. Add a key to authenticate owned paid
games:

1. Open Start > Settings > API Key.
2. Accept the physical-access warning.
3. Enter the complete key with the Catastrophe keyboard and confirm.
4. Wait for validation and the owned-game count.

The key is stored in `config.json`. FAT32 cannot enforce owner-only permissions
against physical access, and the key is not encrypted. After saving, Settings
shows only its suffix. Editing starts from a blank field and never prefills the
saved key. Newly typed characters are visible. Removing/replacing the key clears
credential-derived cache state but preserves downloads and inventory.

## Soundtracks

Music support is disabled by default. Enable `auto` or `ask` to include common
audio files from an upload/archive. Mixed archives may install both ROM and music
content in one transaction summary.

Disco Boy is optional. This pak neither installs nor launches it. Open or relaunch
Disco Boy after installing music so its normal scan reads the selected Music
root on either card.

## Manage, rename, and delete

X on a downloaded game's detail screen opens Manage. The screen distinguishes
ROM and music files and can remove one content group or all app-managed files.
Only artwork recorded as created by this app and no longer referenced by another
managed file is removed. User artwork is retained. Inventory repair drops app
ownership when the recorded hash no longer matches.

Where safe, **Use game title** can rename a ROM plus selected saves/states.
PlayStation descriptors, playlists, and companion files keep their original
names when a rename could break references. Every committed ROM/artwork change
requests one Jawaka rescan.

## Data and logs

The Settings screen displays the resolved App Data directory. Under the normal
Leaf contract it is `$USERDATA_PATH/Itch-io` and contains:

- `config.json`: settings and optional API key;
- `games_cache.json`: timestamped public catalogue;
- `owned_cache.json`: owned-game URL cache;
- `inventory.json`: installed-file and artwork ownership records.

The log is `$LOGS_PATH/itchio-pak.log`. On the stock primary card these normally
appear under `.userdata/mlp1/Itch-io` and `.userdata/mlp1/logs`. No state is
stored inside `.system/leaf` or the replaceable pak directory.

Logs remain local. API keys, authorization/cookie values, signed URLs, account
names, and known absolute runtime roots are redacted at Info and Debug levels.
There is no telemetry and no UMRK network service.

## Troubleshooting

### A downloaded game is missing from Jawaka

Wait for the result screen's library-rescan status. If the request failed, use
Leaf's Rescan action. Confirm that the selected card remains mounted and that
the game format is launchable rather than a support file such as `.bin`.

### A game has no launcher art

Current downloads write art to the selected source's canonical
`Images/<system>/<ROM stem>.png` and rescan automatically. **Update Inventory**
repairs missing app art. It also migrates the pre-release `.media` placement only
when the recorded app-owned hash still matches, so modified/user artwork is not
moved or overwritten.

### The second SD card cannot be selected

The picker requires a real mounted filesystem, not merely the stock empty mount
directory. Reinsert/mount the card and reopen the picker. The app intentionally
does not guess between arbitrary mounts.

### A paid/owned game is unavailable

Validate the stored API key again from Settings. A successful validation reports
the owned-game count. Replacing/removing a key invalidates the old owned cache by
design.

### A network or download request fails

Keep the existing cached catalogue, set **Log Level = Debug**, reproduce once,
then inspect `$LOGS_PATH/itchio-pak.log`. Return to Info afterward. Do not post
the raw configuration file; although logs redact registered secrets, config
contains the saved key.
