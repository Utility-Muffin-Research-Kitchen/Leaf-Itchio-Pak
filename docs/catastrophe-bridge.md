# Catastrophe bridge contract

Phase 5 introduces the application-owned CGo bridge in `internal/catui`. It is
the rendering foundation for the Leaf screen migration; it is not a general Go
binding published by Catastrophe.

## Ownership

- `cat_bridge.c` is the only translation unit in this repository that defines
  `CAT_IMPLEMENTATION`.
- The bridge does not include `catastrophe_widgets.h`, so there is no
  `CAT_WIDGETS_IMPLEMENTATION` definition.
- Catastrophe owns SDL initialization, the window, renderer, fonts, input pump,
  presentation, and shutdown on the migrated proof path.
- Go receives final-pixel boxes, colors, input values, and opaque generation-
  checked texture IDs. It never receives an `SDL_Window`, `SDL_Renderer`,
  `SDL_Texture`, `TTF_Font`, or `SDL_Surface` pointer.
- The owning goroutine locks its OS thread before initialization. Every bridge
  operation except `Wake` rejects calls from another OS thread.

The existing screens still use the inherited renderer until their vertical
slices move in later phases. `--cat-proof` is the first complete Catastrophe-
owned route in the real executable; it establishes the boundary without
claiming that the old screens have already been ported.

## Main-loop and worker wake contract

The proof loop drains the bridge input queue, applies state changes, draws only
when input, a worker wake, or the GIF animation deadline requires it, and calls
`cat_present` once per rendered frame. Animation uses `cat_request_frame_in`
rather than a permanent 60 fps loop.

Workers call `Context.Wake`. The bridge records a wake event atomically and
calls Catastrophe's generic `cat_wake`. Desktop builds use an SDL user event;
device builds additionally write a nonblocking pipe included in
`cat_present`'s evdev poll set. This wakes an otherwise idle MLP1 renderer
without unsafe cross-thread drawing or a polling loop. The proof deliberately
tests this with no scheduled redraw and fails when the wake takes over 750 ms.

## Texture lifetime

Textures are stored in a fixed bridge registry. A public ID contains a slot
index and generation; a destroyed or reused slot cannot be addressed through a
stale ID. Upload, draw, size, and destroy calls validate the ID and the owner
thread. `Context.Close` destroys every live texture before `cat_quit` and marks
all Go handles closed.

RGBA uploads provide the common path for decoded images, GIF frames, and QR
codes. File loading remains available for local static assets. Clip operations
are bridge calls and never expose the renderer.

## Text and layout

Primary chrome uses Catastrophe's live font tiers and theme roles. Remote text
uses the app-local Noto set under `ITCHIO_RES_DIR`. The bridge decodes UTF-8,
selects the first font providing each codepoint, groups matching codepoints
into bounded runs, and uses the same run logic for measurement and drawing.
Unsupported codepoints are omitted rather than rendered as tofu.

Box operations are direct calls to `cat_box_content`, both carve functions,
`cat_box_split_cols`, and `cat_box_fit_rows`. They use final pixels. The proof
carves title and optional footer bands, applies internal padding, then splits
the content 58/42 with a padding-backed gutter.

## Visual proof

Run interactively:

```sh
make run-cat-proof
```

Generate the deterministic Mac verification pair:

```sh
make cat-proof-snapshots
```

The generated, ignored files are:

- `build/cat-proof/dark-hints-bump0.png`;
- `build/cat-proof/light-nohints-bump5.png`.

Together they cover inherited title/status chrome, hint-visible and hidden
layout, minimum and maximum font bump, theme-role color changes, fitted rows,
multilingual fallback runs, a decoded animated GIF, an uploaded QR texture,
progress and triangle primitives, clipping, and worker wake delivery.

For a finite device proof, stage explicitly through Leaf and run:

```sh
make -C ../Leaf stage-app APP=Leaf-Itchio-Pak DEVICE=mlp1
adb shell 'UMRK_ENV_FILE=/mnt/sdcard/.system/leaf/platforms/mlp1/launcher/env.sh \
  /mnt/sdcard/Apps/mlp1/Itch-io.pak/launch.sh \
  --cat-proof --cat-proof-frames=10 \
  --cat-proof-screenshot=/mnt/sdcard/.userdata/mlp1/Itch-io/cat-proof.png'
```

The proof route is temporary. Delete it after shared primitives and all screens
exercise the same bridge directly.
