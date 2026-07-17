#!/usr/bin/env python3
"""Validate app-owned Pak Rat and reproducible-release metadata."""

from __future__ import annotations

import json
import pathlib
import re


ROOT = pathlib.Path(__file__).resolve().parent.parent
SHA_IMAGE = re.compile(r"^[a-z0-9./_-]+(?:\.[a-z0-9./_-]+)*@sha256:[a-f0-9]{64}$")


def load(name: str) -> dict:
    path = ROOT / name
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SystemExit(f"{name}: {exc}") from exc
    if not isinstance(value, dict):
        raise SystemExit(f"{name}: root must be an object")
    return value


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(f"pakrat-metadata-check: {message}")


metadata = load("pakrat.json")
runtime = load("pak.json")
release_lock = load("release-lock.json")

require(metadata.get("schema") == 1, "pakrat.json schema must be 1")
require(metadata.get("id") == "org.umrk.itchio", "unexpected app id")
require(metadata.get("name") == "Itch.io", "unexpected app name")
require(metadata.get("repo_url") == "https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak", "unexpected repo URL")
require("compat" not in metadata, "NextUI compatibility metadata is not allowed")

packages = metadata.get("leaf", {}).get("packages", [])
require(isinstance(packages, list) and len(packages) == 1, "exactly one Leaf package is required")
package = packages[0]
require(package.get("platform") == "mlp1", "package platform must be mlp1")
require(package.get("version") == runtime.get("pak_version") == "0.1.0", "Pak Rat and runtime versions must agree")
require(package.get("artifact_name") == "Itch-io.mlp1.pak.zip", "unexpected artifact name")
require(package.get("install_name") == "Itch-io.pak", "unexpected install name")
require(package.get("runtime_manifest_path") == "pak.json", "unexpected runtime manifest path")
require(package.get("package_dir") == "build/mlp1/package/Itch-io.pak", "unexpected package directory")
require(
    package.get("build_command") == ["make", "package-platform", "PLATFORM=mlp1"],
    "unexpected package build command",
)

require(release_lock.get("schema") == 1, "release-lock.json schema must be 1")
catastrophe_commit = release_lock.get("catastrophe_commit", "")
require(
    isinstance(catastrophe_commit, str) and re.fullmatch(r"[a-f0-9]{40}", catastrophe_commit) is not None,
    "Catastrophe commit must be a full Git SHA",
)
for key in ("mlp1_toolchain_image", "go_image"):
    image = release_lock.get(key, "")
    require(isinstance(image, str) and SHA_IMAGE.fullmatch(image) is not None, f"{key} must be digest-pinned")

print("pakrat-metadata-check: PASS")
