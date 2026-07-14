# Icon provenance

The Leaf-Itchio-Pak icon is original UMRK artwork. It combines a generic
gamepad with the two-tone Leaf mark and does not use or reproduce the official
itch.io logo.

The canonical editable source and export tooling live in the private
`Utility-Muffin-Research-Kitchen/umrk-assets` repository:

| Field | Value |
| --- | --- |
| Source revision | `5e8c75ae12ef0bff5846866bed28acc404b5cbdd` |
| Source path | `icons/apps/Leaf-Itchio-Pak/icon.svg` |
| Generator | `icons/apps/Leaf-Itchio-Pak/make-icon.sh` |
| Vendor sync | `icons/apps/Leaf-Itchio-Pak/sync-to-app.sh` |
| SVG SHA-256 | `583a6fb0a5436b6a089a7ea154a0c6ef55a16f8d111d075662a8f77cfee4fe19` |
| PNG SHA-256 | `b5ea2fbf5d7c6fbc77af40f7dc069c4f7a900f0a6aab4f848eed54604aa14a11` |
| Export | 256x256 RGBA PNG through macOS `/usr/bin/sips`, with non-rendering metadata removed |
| License | MIT when distributed with this repository |

The generated PNG is vendored as `pak/res/icon.png`. Public clones, CI, and
offline MLP1 package builds never need access to the private asset repository.
Edit and regenerate the private source first, commit it, then run its vendor
sync and update this pinned revision/hash record in the public repository.
