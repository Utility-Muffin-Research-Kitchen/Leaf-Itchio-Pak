# Shared Catastrophe screen primitives

Phase 6 defines the application-level composition vocabulary in
`internal/catui/primitives.go`. Individual screen ports consume these helpers;
they do not recreate title, footer, list, modal, gallery, or state geometry.

## Spacing and screen frame

`BasePad`, `ArtPad`, and `ModalPad` are the only shared logical padding values.
`NewComposer` scales them once through Catastrophe. `ComputeScreenLayout` and
`BeginScreen` perform the canonical sequence:

1. carve the inherited title/status band;
2. reserve the footer only when hints are enabled and the screen has actions;
3. apply base content padding;
4. optionally carve a sub-header and its half-pad gap;
5. expose the remaining final-pixel content box.

Catastrophe measures and clips the title against the live status bar, so screen
titles cannot occupy status space. `ScreenFrame.Finish` draws the resolved
footer after content.

## Primitive catalog

| Need | Shared entry point |
| --- | --- |
| Root title/content/footer and sub-header | `BeginScreen`, `ComputeScreenLayout`, `DrawSubHeader` |
| List/detail and full-width body | `ListDetailSplit`, `FullWidthBody`, `DrawScrollingBody` |
| Fitted scrolling list | `FitScrollingList`, `ListGeometry.Row` |
| List selection and settings values | `DrawListRow`, `DrawValueRow` |
| Modal and warning overlays | `CenteredModalRect`, `DrawModal`, `DrawWarningCover` |
| Transfer state | `DrawProgressView` |
| Text entry | `DrawTextField`, `LayoutKeyboard`, `DrawKeyboard` |
| Remote tags | `DrawTagPills` |
| Artwork and screenshots | `FitImage`, `DrawImageFit`, `LayoutGallery`, `DrawGallery` |
| Empty/loading/offline/error | `DrawState` |
| Footer grouping and short labels | `ResolveFooterGroups` |

Column percentages are calculated in Go and converted to a final pixel width
before `cat_box_split_cols`. Every scrolling list obtains its row pitch through
`cat_box_fit_rows`. Components use Catastrophe font tiers and theme roles; they
do not carry screen-height font formulas or literal UI colors.

Remote strings and images are clipped to their content rectangles. Text uses
the bridge's primary/fallback font runs, while textures remain opaque,
generation-checked handles owned by the main render thread.

## Footer policy

Footer hints retain action items in Catastrophe's left group and confirm items
in its right group. `ResolveFooterGroups` estimates the two groups using live
small-tier text measurements. If the long labels would overlap, all available
narrow labels are selected before the footer reaches Catastrophe's own overflow
handling.

## Offline visual route

Run all primitives interactively without network access:

```sh
make run-cat-fixtures
```

Left/up and right/down move between the five pages; A advances and B exits.
Generate the deterministic verification set with:

```sh
make cat-fixture-snapshots
```

The pages cover list/detail composition, body and overlays, text input,
gallery/progress, and the four standard states. The route also rejects an
off-owner-thread draw and proves that a worker wakes an otherwise idle renderer.
It is a development/acceptance route and performs no Leaf runtime or network
initialization.
