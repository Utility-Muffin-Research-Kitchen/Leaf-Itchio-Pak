#!/usr/bin/env python3
"""Verify pinned public icon/screenshot bytes without image-library dependencies."""

from __future__ import annotations

import hashlib
import pathlib
import struct


ROOT = pathlib.Path(__file__).resolve().parent.parent
EXPECTED = {
    "pak/res/icon.png": (256, 256, "b5ea2fbf5d7c6fbc77af40f7dc069c4f7a900f0a6aab4f848eed54604aa14a11"),
    "docs/screenshots/main-list.png": (960, 720, "22003df4922758f628bc5365a58af8a08a0aa708ad241c72f29c059465127791"),
    "docs/screenshots/filter-search.png": (960, 720, "09828b490d6c9709ac0768b8883648f96efb966724dea215f7081376e4aab829"),
    "docs/screenshots/detail-gallery.png": (960, 720, "40a88a823995a2e37ffe66a99bfd5516fdcf1cb17a4ada4068556e51dc5b69aa"),
    "docs/screenshots/content-warning.png": (960, 720, "b96e7549aa831035a821ad792843292643a38c5acc96dad2d4d02cd44e11b36a"),
    "docs/screenshots/download-progress.png": (960, 720, "eb569f943a779738861d005f453d094240aa2421c240f8af31203eb3401fd44b"),
    "docs/screenshots/settings.png": (960, 720, "8fc03428fbe828ebbf4d94e1ec2fd317ac3e9f1865c995403fe44a9dd01cc68f"),
    "docs/screenshots/dual-sd-destination.png": (960, 720, "350be9e0d313281b15b46cf2f0328616cefb9c74fb6dcaa2d20ed313bc16d87d"),
    "docs/screenshots/downloaded-manage.png": (960, 720, "16c882fdf8be2c9b909c1e99d9ab8ec00e61b9a6eebd515ef1c41b59e2ffc898"),
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
    if depth != 8 or color_type != 6:
        fail(f"artifact must be 8-bit RGBA, got depth={depth} color={color_type}")
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
