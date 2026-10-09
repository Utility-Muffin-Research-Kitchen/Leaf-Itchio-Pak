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
| `content-warning.png` | The content warning in front of a game with **Heavy Themes** tags | `968ba609536f4002027f4cfdb5f74f206e0b4b49c9dfb4df4ce0691f440adc37` |
| `download-progress.png` | A download in progress | `e29195572a3dc4e76df66d54282b4fd536498c2dc6430ea6560d036f8ee25574` |
| `settings.png` | Settings, scrolled past the account row | `784ef753b551cb0e456ca4431846e2119a6d7b2517775e588506d2a343b21849` |
| `dual-sd-destination.png` | Choosing the primary or the secondary SD card for a download | `e150c02f77a44043365da893213354a5938667cebf110155f4766ebd831877eb` |
| `downloaded-manage.png` | **Manage** for a downloaded game, with the archive files its ROMs came from | `0c6e488dfaf639a3cac4cecef8310b91e54d95fbe58876ea9ed160e64377e917` |

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
