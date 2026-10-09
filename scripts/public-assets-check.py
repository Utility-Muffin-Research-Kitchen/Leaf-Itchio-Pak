#!/usr/bin/env python3
"""Verify pinned public icon/screenshot bytes without image-library dependencies."""

from __future__ import annotations

import hashlib
import pathlib
import struct


ROOT = pathlib.Path(__file__).resolve().parent.parent
EXPECTED = {
    "pak/res/icon.png": (256, 256, "b5ea2fbf5d7c6fbc77af40f7dc069c4f7a900f0a6aab4f848eed54604aa14a11"),
    "docs/screenshots/main-list.png": (960, 720, "da660df96cc167c1db095505d7589a1b6b852b18f938147f87182ea097a8aa55"),
    "docs/screenshots/filter-search.png": (960, 720, "26f2a435ca89a851593020373684f2f808fb996fd02a4a3079732dbfcf0a6a7a"),
    "docs/screenshots/detail-gallery.png": (960, 720, "8d2e3f75bbfca518030e242f9058b9e2a182151aee789579d469b2540c41df20"),
    "docs/screenshots/content-warning.png": (960, 720, "968ba609536f4002027f4cfdb5f74f206e0b4b49c9dfb4df4ce0691f440adc37"),
    "docs/screenshots/download-progress.png": (960, 720, "e29195572a3dc4e76df66d54282b4fd536498c2dc6430ea6560d036f8ee25574"),
    "docs/screenshots/settings.png": (960, 720, "784ef753b551cb0e456ca4431846e2119a6d7b2517775e588506d2a343b21849"),
    "docs/screenshots/dual-sd-destination.png": (960, 720, "e150c02f77a44043365da893213354a5938667cebf110155f4766ebd831877eb"),
    "docs/screenshots/downloaded-manage.png": (960, 720, "0c6e488dfaf639a3cac4cecef8310b91e54d95fbe58876ea9ed160e64377e917"),
}
FORBIDDEN_METADATA = {b"tEXt", b"zTXt", b"iTXt", b"eXIf"}


def fail(message: str) -> None:
    raise SystemExit(f"public-assets-check: {message}")


def png_info(data: bytes) -> tuple[int, int, set[bytes]]:
    if len(data) < 33 or data[:8] != b"\x89PNG\r\n\x1a\n":
        fail("artifact is not a PNG")
    length = struct.unpack(">I", data[8:12])[0]
    if data[12:16] != b"IHDR" or length != 13:
        fail("artifact has no canonical PNG header")
    width, height, depth, color_type = struct.unpack(">IIBB", data[16:26])
    # 2 is RGB (screenshots captured on the device), 6 is RGBA (the icon).
    if depth != 8 or color_type not in (2, 6):
        fail(f"artifact must be 8-bit RGB or RGBA, got depth={depth} color={color_type}")
    chunks: set[bytes] = set()
    offset = 8
    while offset + 12 <= len(data):
        chunk_length = struct.unpack(">I", data[offset : offset + 4])[0]
        chunk_type = data[offset + 4 : offset + 8]
        chunks.add(chunk_type)
        offset += 12 + chunk_length
    if offset != len(data) or b"IEND" not in chunks:
        fail("artifact has malformed PNG chunks")
    return width, height, chunks


def main() -> None:
    for relative, (expected_width, expected_height, expected_hash) in EXPECTED.items():
        path = ROOT / relative
        if not path.is_file():
            fail(f"missing {relative}")
        data = path.read_bytes()
        width, height, chunks = png_info(data)
        if (width, height) != (expected_width, expected_height):
            fail(f"{relative} is {width}x{height}, expected {expected_width}x{expected_height}")
        actual_hash = hashlib.sha256(data).hexdigest()
        if actual_hash != expected_hash:
            fail(f"{relative} hash {actual_hash} does not match the public manifest")
        forbidden = chunks & FORBIDDEN_METADATA
        if forbidden:
            names = ", ".join(sorted(chunk.decode("ascii") for chunk in forbidden))
            fail(f"{relative} contains public-unsafe metadata chunks: {names}")
    print(f"public-assets-check: PASS ({len(EXPECTED)} pinned PNGs)")


if __name__ == "__main__":
    main()
