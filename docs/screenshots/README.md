# Public screenshots

These screenshots are captured on a Miniloong Pocket 1 running the app, so
they show the launcher's theme and font as you see them on the device. Each
is a 960x720 PNG of the display's scanout, taken with `kmsgrab` over ADB and
turned upright.

They show public itch.io games only. No account name, sign-in code, host path
or private file appears in them: Settings is captured with its account row
scrolled off the screen, and the content warning is a **Heavy Themes** one.

| Screenshot | What it shows | SHA-256 |
| --- | --- | --- |
| `main-list.png` | The game list for Game Boy, sorted by **Popular** | `da660df96cc167c1db095505d7589a1b6b852b18f938147f87182ea097a8aa55` |
| `filter-search.png` | **Filter & Search** with a platform and a sort staged | `26f2a435ca89a851593020373684f2f808fb996fd02a4a3079732dbfcf0a6a7a` |
| `detail-gallery.png` | A game's page with its gallery, tags and description | `8d2e3f75bbfca518030e242f9058b9e2a182151aee789579d469b2540c41df20` |
| `content-warning.png` | The content warning in front of a game with **Heavy Themes** tags | `3f4703aea31140691627bf960730cebbbfa447457f27a471461b2dc93a80c485` |
| `download-progress.png` | A download in progress | `e29195572a3dc4e76df66d54282b4fd536498c2dc6430ea6560d036f8ee25574` |
| `settings.png` | Settings, scrolled past the account row | `0aa98a540de93c6cc5eb3013c22b43c7d6e751f5f01725143320b7dd6f8005de` |
| `dual-sd-destination.png` | Choosing the primary or the secondary SD card for a download | `d8a17b298f34a27f549b046bd65fd3ce7fbe7211d90b443b55d2d1f1a28a4014` |
| `downloaded-manage.png` | **Manage** for a downloaded game, with the archive files its ROMs came from | `034ae1553620e3ca81d7ef0150d4f3bea80269c3c7530186faaf64aff7042082` |

`scripts/public-assets-check.py` checks each file's size and hash and that it
carries no text or EXIF metadata. `make test` runs it.

## Retaking them

1. Install the build you want to show on an MLP1 and start the app.
2. Bring the screen to the state in the table. Wait until images and text
   have finished loading.
3. Capture the screen. In the umbrella workspace the `mlp1-screenshot` skill
   does this (`adb exec-out kmsgrab`, then `fb_to_png.py` to turn the
   portrait scanout upright). Capture twice and keep a shot only when both
   are identical, so no frame is caught mid-draw.
4. Replace the file, then update its hash here and in
   `scripts/public-assets-check.py`.

The fixture snapshots (`make cat-main-list-snapshots`,
`make cat-input-snapshots`) remain the automated visual checks; they are not
used for these screenshots.
