#!/usr/bin/env python3
"""Fixture runs for pakrat-metadata-check.py.

Copies the checked-in metadata into a temporary root, runs the check against
it unchanged (must pass), then with a mismatched author (must fail).
"""

from __future__ import annotations

import json
import pathlib
import shutil
import subprocess
import sys
import tempfile


ROOT = pathlib.Path(__file__).resolve().parent.parent
CHECK = ROOT / "scripts" / "pakrat-metadata-check.py"
FILES = ("pakrat.json", "pak.json", "release-lock.json")


def fail(message: str) -> None:
    raise SystemExit(f"pakrat-metadata-check-test: {message}")


def run_check(root: pathlib.Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(CHECK), "--root", str(root)],
        capture_output=True,
        text=True,
    )


def fixture(parent: pathlib.Path, name: str) -> pathlib.Path:
    root = parent / name
    root.mkdir()
    for file in FILES:
        shutil.copyfile(ROOT / file, root / file)
    return root


with tempfile.TemporaryDirectory(prefix="pakrat-metadata-check-") as tmp:
    tmp_path = pathlib.Path(tmp)

    matching = fixture(tmp_path, "matching")
    result = run_check(matching)
    if result.returncode != 0:
        fail(f"unchanged fixture must pass, got: {result.stdout}{result.stderr}".strip())

    mismatched = fixture(tmp_path, "mismatched-author")
    metadata_path = mismatched / "pakrat.json"
    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    metadata["author"] = "Carroarmato0 & Someone Else"
    metadata_path.write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    result = run_check(mismatched)
    output = f"{result.stdout}{result.stderr}"
    if result.returncode == 0:
        fail("mismatched author fixture passed; expected a failure")
    if "author" not in output:
        fail(f"mismatched author failed for another reason: {output.strip()}")

print("pakrat-metadata-check-test: PASS")
