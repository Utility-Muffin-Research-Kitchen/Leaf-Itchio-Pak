# Leaf Itch.io Pak — Contributor Reference

This repository is UMRK's Leaf-only fork of `carroarmato0/NextUI-Itchio-Pak`.
The upstream NextUI implementation is a source and behavior reference, not a
runtime or layout compatibility target.

## Current scope

- Target only Miniloong Pocket 1 (`PLATFORM=mlp1`, aarch64 RK3566).
- Package as `Apps/mlp1/Itch-io.pak`; do not add it to Leaf's default apps.
- Publish through Pak Rat only after every verification gate passes.
- Keep the Go catalogue, download, inventory, content-filter, GIF, API-key, and
  music behavior unless the port plan explicitly replaces it.
- Replace SDL renderer ownership with an app-local CGo bridge to Catastrophe's
  retained box-model GUI.
- Support both SD cards through Leaf's runtime environment and explicit
  destination selection.

## Runtime contract

Source `$SDCARD_PATH/.system/leaf/platforms/$PLATFORM/launcher/env.sh` from the
Pak entrypoint when present. Prefer the public variables documented in
`../umrk-workspace/docs/runtime-paths.md`; do not hardcode `/mnt/SDCARD` or
NextUI paths in new code.

Durable app state belongs under `.userdata/mlp1/itchio`; release-managed files
belong under `.system/leaf/platforms/mlp1`. The launcher stack is entered
through `jawakad`, and suspend inhibition must use a generic Jawaka contract.

## Build and test

```sh
./scripts/test.sh
go test -race -tags headless ./...
```

The containerized Linux suite is canonical. The host command is an additional
fast check. Device packaging and staging will move to an MLP1-only lane as the
Leaf port lands.

## Code constraints

- Use `internal/logger` instead of direct production-path prints.
- Keep network tests offline with `httptest` and checked-in fixtures.
- Keep headless business logic independently testable.
- In the Catastrophe bridge, define `CAT_IMPLEMENTATION` and
  `CAT_WIDGETS_IMPLEMENTATION` in exactly one translation unit and include
  `catastrophe_widgets.h` only after `catastrophe.h`.
- Public Catastrophe API names use `cat_`; internal names use `cat__`; constants,
  macros, and enums use `CAT_`.

See `UPSTREAM.md` for provenance and the umbrella implementation plan for the
ordered migration and release gates.
