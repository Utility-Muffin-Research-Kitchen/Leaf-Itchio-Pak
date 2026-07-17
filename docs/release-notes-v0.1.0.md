# Itch.io for Leaf 0.1.0

This is an unofficial itch.io client for Leaf on the Miniloong Pocket 1. It is
not affiliated with or endorsed by itch.io.

The release can browse itch.io, download compatible ROMs and soundtracks to
either Leaf SD card, publish storefront titles and artwork to the Leaf library,
and manage app-owned content. Animated catalogue artwork and Disco Boy music
integration are retained.

## Requirements and installation

- Leaf on MLP1 with Jawaka 0.5.5 or newer.
- Install through Pak Rat when the production entry is available.
- Manual fallback: verify the checksum, extract the single `Itch-io.pak`
  directory, and copy it to `Apps/mlp1/`.
- This app is optional and is not included in Leaf SD release ZIPs.

## API-key warning

An API key is optional and is only needed for owned paid downloads. When saved,
it is stored in app data on the SD card and is not encrypted. FAT32 cannot
protect it from someone with physical access to the card. Logs redact
registered credentials, account identifiers, and signed download URLs.

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
