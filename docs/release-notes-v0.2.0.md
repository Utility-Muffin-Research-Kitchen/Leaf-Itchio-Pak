# Itch.io for Leaf 0.2.0

This is an unofficial itch.io client for Leaf on the Miniloong Pocket 1. It is
not affiliated with or endorsed by itch.io.

This release replaces the typed API key with sign-in by QR code, moves owned
downloads to itch.io's current API, makes the game list smooth to move through
even with the full catalog, and fixes many download, archive and screen
details.

## Upgrading from 0.1.0

0.1.0 stored a typed itch.io API key. On first start, 0.2.0 removes that key
and the list of games it owned, and opens Settings so you can sign in again.
Your downloads and inventory stay as they are.

## What's new

### Sign in with a QR code

- Open **Start > Settings > itch.io Account**, or press **A** on a paid game,
  scan the code with your phone, and approve Leaf on itch.io. You no longer
  type a key.
- **Sign Out** clears the key and the list of games you own, and keeps your
  downloads.
- Signed in, free and pay-what-you-want games download through the itch.io
  API, with the web download page as a fallback.

### Downloads and archives

- Owned games download through itch.io's current API.
- When itch.io asks the app to slow down, it stops and asks you to try again
  later. A stalled download stops instead of hanging.
- ZIP and 7z selections leave out Markdown files, soundtrack tracks with the
  same name are all kept, and a replaced upload no longer removes the wrong
  file.
- Manage shows which archive a file came from and can restore an archive
  ROM's original name.
- Pico-8 sets keep their names, and each file of a multi-file set keeps its own
  record.
- A game's page checks for newer uploads without downloading them, and Manage
  still works when the page fails to load.
- A file for a system the app can't install, such as Nintendo DS, says so
  instead of asking you to choose a format.

### Game list

- Holding the d-pad moves through the list smoothly and at a steady pace, even
  with all 11,000+ games showing, and a press shows on screen right away.
- Cover art loads once you stop on a game instead of for every game you pass.
- Long titles end in "..." before the price.
- The list checks the newest pages of each feed for new games once a day and
  rebuilds fully once a week. If one feed fails, the other systems still
  update.

### Screens

- Settings, Content Moderation and long text show a scrollbar when there is
  more to see.
- Prompts list files two lines each, and **Left** and **Right** page through
  them.
- Errors read as plain sentences.
- The content warning names the categories that matched.
- About shows the installed Leaf version, and the Leaf library rescan result
  shows as soon as it arrives.

## Requirements and installation

- Leaf on MLP1 with Jawaka 0.5.5 or newer.
- Install or update through Pak Rat: press **Menu**, open **Actions > Pak
  Rat**, and choose **Itch.io**.
- Manual fallback: verify the checksum, extract the single `Itch-io.pak`
  directory, and copy it to `Apps/mlp1/`.
- This app is optional and is not included in Leaf SD release ZIPs.

## Sign-in key and privacy

When you sign in, itch.io gives the app a key. It is stored in app data on the
SD card and is not encrypted. FAT32 cannot protect it from someone with
physical access to the card. The key never appears on screen, and logs redact
credentials, account identifiers, and signed download URLs. The app cannot
revoke the key on itch.io, so delete it from your itch.io account's API keys if
you lose the card.

## Content-warning defaults

- Adult/suggestive content: on
- Heavy themes: on
- Substance use: on
- Queer/LGBTQ+ themes: off

Warnings are tag-based confirmations. They do not delete or silently hide
games.

## Provenance

UMRK preserves the upstream history, attribution, and MIT license from
Carroarmato0's NextUI-Itchio-Pak. The Leaf port uses an original UMRK icon and
Catastrophe UI, and supports only the Leaf MLP1 runtime.
