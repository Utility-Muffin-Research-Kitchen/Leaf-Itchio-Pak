#!/usr/bin/env python3
"""Validate the exact Leaf MLP1 package and deterministic release archive."""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import re
import stat
import struct
import subprocess
import sys
import zipfile


FONTS = (
    "font.ttf",
    "font_fallback_arabic.ttf",
    "font_fallback_devanagari.ttf",
    "font_fallback_emoji.ttf",
    "font_fallback_hebrew.ttf",
    "font_fallback_thai.ttf",
)
STATIC_FILES = {
    "bin/itchio-pak",
    "launch.sh",
    "pak.json",
    "res/icon.png",
    "res/certs/ca-certificates.crt",
    "licenses/LICENSE",
    "licenses/Catastrophe-LICENSE",
    "licenses/THIRD-PARTY-LICENSES.md",
}
EXPECTED_FILES = STATIC_FILES | {f"res/fonts/{font}" for font in FONTS} | {
    f"res/fonts/{font.removesuffix('.ttf')}.LICENSE.txt" for font in FONTS
}
FAT_INVALID = re.compile(r'[<>:"/\\|?*\x00-\x1f]')
FAT_RESERVED = {"CON", "PRN", "AUX", "NUL"} | {
    f"{prefix}{number}" for prefix in ("COM", "LPT") for number in range(1, 10)
}


def fail(message: str) -> None:
    raise SystemExit(f"package-smoke: {message}")


def check_fat_name(path: pathlib.PurePath) -> None:
    for part in path.parts:
        stem = part.split(".", 1)[0].upper()
        if FAT_INVALID.search(part) or part.endswith((" ", ".")) or stem in FAT_RESERVED:
            fail(f"not FAT32-safe: {path}")


def elf_machine(path: pathlib.Path) -> tuple[int, int, int]:
    data = path.read_bytes()[:64]
    if len(data) < 20 or data[:4] != b"\x7fELF":
        fail("bin/itchio-pak is not ELF")
    byte_order = "<" if data[5] == 1 else ">"
    return data[4], data[5], struct.unpack_from(byte_order + "H", data, 18)[0]


def runtime() -> str:
    configured = os.environ.get("CONTAINER_RUNTIME", "")
    if configured and configured not in {"docker", "podman"}:
        fail(f"unsupported CONTAINER_RUNTIME: {configured}")
    choices = [configured] if configured else ["docker", "podman"]
    for candidate in choices:
        if candidate and subprocess.run(
            ["sh", "-c", f"command -v {candidate}"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ).returncode == 0:
            return candidate
    fail("docker or podman is required to inspect MLP1 dynamic dependencies")


def readelf(package: pathlib.Path, image: str) -> str:
    repo = pathlib.Path(__file__).resolve().parent.parent
    workspace = repo.parent
    binary = pathlib.PurePosixPath("/workspace/Leaf-Itchio-Pak") / package.relative_to(repo) / "bin/itchio-pak"
    command = [
        runtime(), "run", "--rm", "-v", f"{workspace}:/workspace", image,
        "sh", "-c", f'"$READELF" -l -d "{binary}"',
    ]
    result = subprocess.run(command, text=True, capture_output=True)
    if result.returncode:
        fail(f"readelf failed:\n{result.stderr.strip()}")
    return result.stdout


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--package", type=pathlib.Path, required=True)
    parser.add_argument("--archive", type=pathlib.Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--min-jawaka-version", required=True)
    parser.add_argument("--toolchain-image", required=True)
    args = parser.parse_args()

    package = args.package.resolve()
    archive = args.archive.resolve()
    if package.name != "Itch-io.pak" or not package.is_dir():
        fail(f"missing exact package root: {package}")

    for path in package.rglob("*"):
        if path.is_symlink():
            fail(f"symlink forbidden: {path.relative_to(package)}")
        if not (path.is_dir() or path.is_file()):
            fail(f"non-regular package entry: {path.relative_to(package)}")
        check_fat_name(path.relative_to(package))

    actual_files = {path.relative_to(package).as_posix() for path in package.rglob("*") if path.is_file()}
    missing = sorted(EXPECTED_FILES - actual_files)
    extra = sorted(actual_files - EXPECTED_FILES)
    if missing or extra:
        fail(f"layout mismatch; missing={missing}, extra={extra}")

    binary = package / "bin/itchio-pak"
    launcher = package / "launch.sh"
    if not os.access(binary, os.X_OK) or not os.access(launcher, os.X_OK):
        fail("binary and launch.sh must be executable")

    manifest = json.loads((package / "pak.json").read_text(encoding="utf-8"))
    expected_manifest = {
        "name": "Itch.io",
        "icon": "res/icon.png",
        "platform": "mlp1",
        "pak_version": args.version,
        "min_jawaka_version": args.min_jawaka_version,
        "author": "Carroarmato0 & Utility Muffin Research Kitchen",
        "repo_url": "https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak",
    }
    for key, expected in expected_manifest.items():
        if manifest.get(key) != expected:
            fail(f"manifest {key}={manifest.get(key)!r}; expected {expected!r}")
    if "platforms" in manifest or not manifest.get("description"):
        fail("manifest must have a description and no NextUI platforms array")

    for font in FONTS:
        notice = package / "res/fonts" / f"{font.removesuffix('.ttf')}.LICENSE.txt"
        if not notice.stat().st_size:
            fail(f"empty font license: {notice.name}")
    certs = (package / "res/certs/ca-certificates.crt").read_bytes()
    if certs.count(b"-----BEGIN CERTIFICATE-----") < 100:
        fail("CA certificate bundle is missing or implausibly small")
    icon = (package / "res/icon.png").read_bytes()
    if len(icon) < 24 or icon[:8] != b"\x89PNG\r\n\x1a\n":
        fail("res/icon.png is not a PNG")
    width, height = struct.unpack(">II", icon[16:24])
    if (width, height) != (256, 256):
        fail(f"res/icon.png must be 256x256, got {width}x{height}")

    launcher_text = launcher.read_text(encoding="utf-8")
    for prefix in ("/Users/", "/Volumes/", "/home/"):
        if prefix in launcher_text:
            fail(f"host path leaked into launch.sh: {prefix}")
    binary_strings = subprocess.run(["strings", str(binary)], text=True, capture_output=True, check=True).stdout
    repo_text = str(pathlib.Path(__file__).resolve().parent.parent)
    if repo_text in binary_strings or "/Volumes/Storage/UMRK" in binary_strings:
        fail("absolute checkout path leaked into binary")

    elf_class, elf_data, machine = elf_machine(binary)
    if (elf_class, elf_data, machine) != (2, 1, 183):
        fail(f"wrong ELF target: class={elf_class}, data={elf_data}, machine={machine}")
    elf_report = readelf(package, args.toolchain_image)
    if "/lib/ld-linux-aarch64.so.1" not in elf_report:
        fail("wrong or missing MLP1 ELF interpreter")
    if "RPATH" in elf_report or "RUNPATH" in elf_report:
        fail("target binary must not carry RPATH/RUNPATH")
    needed = sorted(set(re.findall(r"Shared library: \[(.+?)\]", elf_report)))
    if not needed or not any(name.startswith("libSDL2-") for name in needed):
        fail(f"SDL2 dynamic dependency missing: {needed}")

    installed_size = sum((package / name).stat().st_size for name in actual_files)
    if installed_size > 64 * 1024 * 1024:
        fail(f"installed size exceeds 64 MiB: {installed_size}")

    if not archive.is_file():
        fail(f"missing archive: {archive}")
    with zipfile.ZipFile(archive) as zf:
        members = zf.infolist()
        roots = {pathlib.PurePosixPath(member.filename).parts[0] for member in members}
        if roots != {"Itch-io.pak"}:
            fail(f"archive roots are ambiguous: {sorted(roots)}")
        archive_files = set()
        for member in members:
            path = pathlib.PurePosixPath(member.filename)
            check_fat_name(path)
            mode = member.external_attr >> 16
            if stat.S_ISLNK(mode):
                fail(f"archive symlink forbidden: {member.filename}")
            if not member.is_dir():
                archive_files.add(path.relative_to("Itch-io.pak").as_posix())
        if archive_files != actual_files:
            fail("archive contents disagree with package directory")

    print(f"package-smoke: PASS ({installed_size / (1024 * 1024):.2f} MiB installed)")
    print(f"package-smoke: dynamic dependencies: {', '.join(needed)}")


if __name__ == "__main__":
    main()
