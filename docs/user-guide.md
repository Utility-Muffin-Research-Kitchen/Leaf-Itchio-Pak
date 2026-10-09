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
another platform folder. Jawaka `0.5.5` or newer is required.

## Browsing

The main list combines the cached catalogue with newly fetched data. The header
shows the active platform, sort, and cache age/date. An existing cache remains
usable if refresh fails.

- Up/Down moves one game.
- Left/Right jumps to the previous/next initial in A-Z or Z-A sort; with other
  sorts it moves by one visible page.
- L1/R1 changes sort.
- L2/R2 changes platform category.
- The footer groups L1/R1 as Sort and L2/R2 as System so both shoulder-button
  actions remain explicit without overflowing the main-list footer.
- Select opens Filter; Select applies staged filter changes.
- Y clears staged search, platform, and sort values inside Filter.
- A opens the selected game.
- B exits from the main list. Menu remains a Leaf control and does not exit.

The supported categories are All, GB, GBC, GBA, NES, Mega Drive, Pico-8, and
PlayStation. Sort choices are Popular, A-Z, Z-A, Newest, Free, Paid, Downloaded,
and Owned. Popular is itch.io's own browse order. Owned requires signing in with
itch.io.

## Detail and gallery

Up/Down scrolls the description, and a scrollbar at its right edge shows when
there is more to read. Left/Right or L1/R1 moves through the cover,
animated GIF, and screenshots. A begins the available download flow. X opens
Manage when the game has installed files. Start opens Settings and B returns to
the main list.

On a paid game, A opens sign-in while you're signed out. Signed in, a paid game
your account doesn't own says **Not owned** under the QR code and has no
download; scan the code to buy it on itch.io. The page follows your account,
so signing in or out from Settings updates it right away.

A game that matches one of your content filters shows a warning in place of its
details. The warning names the categories that matched, as **Content
Moderation** does: **Adult Content**, **Queer Content**, **Heavy Themes** or
**Substance Use**. Press Start to open **Content Moderation** and change them,
then open the game again. B goes back to the list. The filters never delete or
silently remove catalogue entries.

## Download flow

The app distinguishes directly supported ROMs, archives, music, and unknown
formats. Depending on Settings and the upload, it may ask for:

1. a purchase or upload;
2. an archive subset or format;
3. a mounted SD card;
4. a folder inside the canonical system or Music root;
5. final confirmation of every relative output path, with each file under the
   name it's saved as. For an archive, the confirmation lists only the
   folders, because the app names its files as it extracts them.

With **ROM Selection** on **Auto**, the app downloads every supported upload
together when each one is for a different system. A PlayStation game's CUE/BIN
tracks or discs count as one set. When the game offers more than one build for
the same system, such as an update and the original jam release, or offers an
archive, the app lists every upload instead and downloads only the one you
choose.

Signed in, the app can tell Windows, macOS, Linux, Android and browser builds
from the files Leaf can play. It never picks those builds for you. When
you choose the file yourself, they're listed last, behind **Show all files**.

If you later pick another build for the same system that the page still
offers, the app keeps both: the first keeps the title name, such as
`Glory Hunters.gba`, and the next gets its upload name added,
`Glory Hunters (glory_ez4).gba`. An update that replaced the old upload on the
page replaces the old file instead.

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

`.md` is also the extension of Markdown text. A `.md` file, inside an archive or
as its own upload, is installed as a Mega Drive ROM only when it has a Mega
Drive header or is not plain text, so `README.md` and `LICENSE.md` are left out.

PlayStation support files such as BIN are installed with their descriptor but
are not indexed as separate games. From an archive that has a CUE sheet, only
the BIN files the sheet references are installed, so a BIOS image such as
`openbios.bin` shipped next to the game stays out of your PlayStation folder.
Descriptor/playlist names are preserved when renaming could break internal
references.

## Dual-SD destinations

Leaf supplies the ordered source list and canonical system catalogue. The
primary card is source 1 and the optional second card is source 2. A configured
but unmounted card is shown as **Not mounted** and cannot be selected.

**ROM Location** on **Auto** uses the primary canonical system directory.
**ROM Location** on **Ask** lets you select a mounted card and safe subfolder.
The choice can be remembered independently per system. Artwork is always
written to the matching source's canonical `Images/<system>` directory.

**Music Location** on **Auto** uses the primary Music root. **Ask** provides the
same mounted-card/folder picker for music. **Reset Remembered Folders** in
Settings clears remembered destinations without deleting downloads.

## Settings and defaults

The last four settings in this table are under **Content Moderation**.

| Setting | Default | Choices/behavior |
| --- | --- | --- |
| **ROM Selection** | **Auto** | Automatically choose or ask among supported uploads |
| **ROM Location** | **Auto** | Primary canonical directory or ask for card/folder |
| **Music Download** | **Off** | **Off**, **Auto**, or **Ask** |
| **Music Location** | **Auto** | Primary Music root or ask for card/folder |
| **Rename ROM Files** | **On** | Rename safe standalone ROM files to the itch.io title; Leaf display titles are published independently |
| **Log Level** | **Info** | **Info** or **Debug** |
| **Adult Content** | **Blocked** | **All Category Tags** plus individual tags |
| **Heavy Themes** | **Blocked** | **All Category Tags** plus individual tags |
| **Substance Use** | **Blocked** | **Blocked** or **Allowed** |
| **Queer Content** | **Allowed** | **All Category Tags** plus individual tags |

**Refresh Game List** rebuilds the public catalogue cache. If an itch.io feed
fails, the games of its system stay as they were and the other systems still
update. On its own, the app checks for new games once a day when you open it,
reading only the newest pages of each feed, and rebuilds the whole list once a
week; until then, new games show at the top of their system. **Update
Inventory** checks missing artwork, removed upstream games, and newly offered
uploads without deleting local files.
The app also checks at launch and when you sign in or out, but then skips games
it checked in the last six hours, and it waits while a download runs.
A new version that replaces a file you downloaded, such as a new `.gb` build
for your `.gb` or a new archive for your archive, shows as an update.
When signed in, it also detects files replaced under the same name, including
paid games you own. A changed upload marks a game only when you installed that
upload, so a new Windows or soundtrack build of a game you play as a ROM does
not. Installing the new version clears the mark. Files the new version no
longer includes, such as renamed tracks, stay where they are: the app never
deletes them on its own. A game is marked removed only when its page is gone,
or when, signed in, itch.io no longer lists your file or anything to replace
it. These checks read metadata without starting a download.
When signed out, it can find newly listed filenames on public pages, but cannot
verify changed file contents or hidden paid downloads. Signing in or out does
not mark every file as an update, but an installed upload that was replaced in
the meantime still shows, even before the first check after you download.
A network error leaves its status as it was.
**Clear Image Cache** clears decoded in-memory cover/GIF frames; remote images
are fetched again when needed.

Game details use itch.io's current price and currency when available. A suggested
contribution still allows a free download. Sale prices show the original amount
alongside the current price, a minimum price you may exceed shows as
"$2.00 or more", and a paid game you own shows **Owned**. Once you open a game,
the list shows that current price too. If that metadata cannot load, you still
get the available game-page details.

## Sign in with itch.io

Free browsing and downloads work without an account. Sign in to download paid
games you own. Signed in, free and pay-what-you-want games are also listed
through the itch.io API, which is quicker than the web download page. If the
API fails or lists nothing, the app tries the web download page once, and it
does the same if itch.io lists the game but refuses the download. If itch.io
asks the app to slow down, it stops and asks you to try again later instead.

1. Open **Start > Settings > itch.io Account**, or press **A** on a paid game.
2. Accept the physical-access warning (first time only).
3. Scan the QR code with your phone. The code expires after a few minutes;
   press A for a new one.
4. Check that itch.io shows the same short code as the handheld, then approve
   Leaf. The app loads your owned games and shows your account name. You can
   press B while they load; loading finishes on its own.

If the screen says **Sign-in is unavailable**, itch.io could not start the
sign-in. Free games still download.

itch.io gives the app a key, stored in `config.json`. FAT32 cannot enforce
owner-only permissions against physical access, and the key is not encrypted.
It never appears on screen. **Sign Out** clears it and the owned-game cache but
keeps downloads and inventory. The app cannot revoke the key on itch.io, so
delete it from your itch.io account's API keys if you lose the card. If itch.io
stops accepting the key, the app signs you out and says so on the game list
until you press A.

Earlier releases stored a typed API key. This release removes it, and the list
of games that key owned, on first start and opens Settings so you can sign in.

## Soundtracks

**Music Download** is **Off** by default. Set it to **Auto** or **Ask** to
include common audio files from an upload/archive. Mixed archives may install
both ROM and music content in one transaction summary.

A game's tracks go into one Music folder. When an archive holds tracks with the
same name in different folders, such as `cd1/01 Theme.ogg` and
`cd2/01 Theme.ogg`, every track of those folders keeps its folder as a
subfolder (`cd1/01 Theme.ogg`, `cd1/02 Battle.ogg`, `cd2/01 Theme.ogg`), so
both are installed and Disco Boy plays each disc in order. Tracks whose names
differ only in letter case would be the same file on your SD card, so the later
one gets a number: `Theme.ogg` and `theme (2).ogg`.

Disco Boy is optional. This pak neither installs nor launches it. Open or relaunch
Disco Boy after installing music so its normal scan reads the selected Music
root on either card.

## Manage, rename, and delete

X on a downloaded game's detail screen opens Manage. The screen distinguishes
ROM and music files and can remove one content group or all app-managed files.
A confirmation that lists more files than fit, such as **Delete all downloads**
for a game with a soundtrack, scrolls with Up/Down and pages with Left/Right.
A file from an archive that was saved under another name, such as the game's
title, shows which archive file it came from, for example
**From Glory Hunters 1.3 EZ IV Patched.gba** under `Glory Hunters.gba`.
When you download an archive or file again, files of its earlier version that
the new install no longer uses stay on your card. For example, an older version
of this app put every soundtrack track in one folder, and the new install keeps
same-named tracks in `cd1/` and `cd2/` subfolders. Manage marks the old copies
**OLD** and offers **Delete left-over files**. Nothing is deleted until you
choose it. If you download the same version again into another folder or onto
the other card, Manage marks the first copy **OLD** too and calls it an earlier
copy of files you installed again, so you can keep whichever copy you want.
A file is only offered as left over while the newer copy that replaced it is
still on your card. If you delete that copy, the older one becomes an ordinary
file again, so **Delete left-over files** can never remove the last copy.

Only artwork recorded as created by this app and no longer referenced by another
managed file is removed. User artwork is retained. Inventory repair drops app
ownership when the recorded hash no longer matches.

Each cart of a multi-file Pico-8 game gets its own launcher art, saved as
`Images/PICO8/<cart name>.png` because the launcher looks art up by the cart's
file name, not its folder. The game's cover is downloaded once for all of them,
and a cart that is itself a `.p8.png` image is its own art. Two carts with the
same name in different folders share one image, which is removed with the
last of them.

Where safe, **Rename ROM Files** can rename a ROM plus selected saves/states.
New downloads also publish the itch.io title to Leaf as display metadata, even
when physical renaming is disabled or unsafe. Manual Leaf display-name edits
take precedence. Existing downloads are not backfilled automatically.
PlayStation descriptors, playlists, and companion files keep their original
names when a rename could break references. The files of a multi-file Pico-8
game keep their names too, carts and `.lua` files alike, because its carts and
code find each other by name. When two files from one download
or archive would get the same title name, they keep their original names so
neither replaces the other. FAT32 ignores letter case, so two names that
differ only in case count as the same file: a download stops before writing
anything, and an archive skips the later file and lists it on the
**Download complete** screen. Every committed ROM/artwork change requests one
Jawaka rescan.

For each ROM it can safely rename, Manage offers one rename row.
**Use title for leafbound_v2.gb** renames the file after the game's title.
**Use original name for Glory Hunters.gb** renames it back: a file you
downloaded on its own gets its upload's name again, and a ROM from an archive
gets the name of its file in the archive, such as `Glory Hunters 2.0.1.gb`.
Manage offers a rename only when it changes the name, and it asks before
renaming the ROM's saves and save states with it. After you go back to an
original name, downloading the game again keeps original names, even with
**Rename ROM Files** on. A ROM that an older version of this app extracted
from an archive has no record of its file in the archive, so it gets no
**Use original name** row until you download the game again.

A download never replaces a file that another game installed, or a file the
app did not install. It saves its own copy as `<Title> - <file name>` instead,
and downloading the same game again later updates that copy. If two games
already share a file from an earlier version, deleting one of them in Manage
keeps the file for the other.

## Data and logs

The Settings screen displays the resolved App Data directory. Under the normal
Leaf contract it is `$USERDATA_PATH/Itch-io` and contains:

- `config.json`: settings and the itch.io sign-in key, when signed in;
- `games_cache.json`: timestamped public catalogue;
- `owned_cache.json`: owned-game URL cache;
- `inventory.json`: installed-file and artwork ownership records.

The log is `$LOGS_PATH/itchio-pak.log`. On the stock primary card these normally
appear under `.userdata/mlp1/Itch-io` and `.userdata/mlp1/logs`. No state is
stored inside `.system/leaf` or the replaceable pak directory.

Logs remain local. itch.io keys, authorization/cookie values, signed URLs, account
names, and known absolute runtime roots are redacted whether **Log Level** is
**Info** or **Debug**. There is no telemetry and no UMRK network service.

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

### A README shows up as a Mega Drive game

Earlier versions installed some `README.md` files from archives as Mega Drive
ROMs, named after the game, such as `Roms/GENESIS/<Title>.md`. Open the game's
Manage screen and delete that file.

### A paid/owned game is unavailable

Select **itch.io Account** in Settings to check your sign-in again. A
successful check reports the owned-game count. Signing out, or signing in to a
different account, clears the old owned cache by design.

### A network or download request fails

Keep the existing cached catalogue, set **Log Level** to **Debug**, reproduce
once, then inspect `$LOGS_PATH/itchio-pak.log`. Set it back to **Info**
afterward. Do not post the raw configuration file; although logs redact
registered secrets, config contains the saved key.
