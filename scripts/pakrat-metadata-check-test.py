#!/usr/bin/env python3
"""Fixture runs for pakrat-metadata-check.py.

Copies the checked-in metadata into a temporary root, runs the check against
it unchanged (must pass), then with a mismatched author, a build script whose
default version differs, and no release notes for the version (each must
fail).
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
VERSION = json.loads((ROOT / "pak.json").read_text(encoding="utf-8"))["pak_version"]
FILES = (
    "pakrat.json",
    "pak.json",
    "release-lock.json",
    "Makefile",
    "scripts/build.sh",
    "scripts/package.sh",
    f"docs/release-notes-v{VERSION}.md",
)


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
        (root / file).parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / file, root / file)
    return root


def expect_failure(root: pathlib.Path, case: str, reason: str) -> None:
    result = run_check(root)
    output = f"{result.stdout}{result.stderr}"
    if result.returncode == 0:
        fail(f"{case} fixture passed; expected a failure")
    if reason not in output:
        fail(f"{case} failed for another reason: {output.strip()}")


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
    expect_failure(mismatched, "mismatched author", "author")

    stale = fixture(tmp_path, "stale-build-script")
    script = stale / "scripts" / "package.sh"
    script.write_text(
        script.read_text(encoding="utf-8").replace(
            f"APP_VERSION=${{APP_VERSION:-{VERSION}}}", "APP_VERSION=${APP_VERSION:-0.0.1}"
        ),
        encoding="utf-8",
    )
    expect_failure(stale, "stale build script", "scripts/package.sh default APP_VERSION")

    unnoted = fixture(tmp_path, "missing-release-notes")
    (unnoted / "docs" / f"release-notes-v{VERSION}.md").unlink()
    expect_failure(unnoted, "missing release notes", f"release-notes-v{VERSION}.md")

print("pakrat-metadata-check-test: PASS")
