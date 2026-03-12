#!/usr/bin/env python3

from __future__ import annotations

import subprocess
import sys
import tempfile
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: apply_patchset.py <input.tex> <patchfile>", file=sys.stderr)
        return 1

    input_path = Path(sys.argv[1])
    patch_path = Path(sys.argv[2])
    original = input_path.read_text()
    patch_text = patch_path.read_text() if patch_path.exists() else ""

    if not patch_text.strip():
        sys.stdout.write(original)
        return 0

    with tempfile.TemporaryDirectory() as tmpdir:
        tmp = Path(tmpdir)
        target = tmp / "document.tex"
        target.write_text(original)
        tmp_patch = tmp / "override.patch"
        tmp_patch.write_text(patch_text)
        result = subprocess.run(
            ["patch", "--silent", str(target), str(tmp_patch)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            sys.stderr.write(result.stderr or result.stdout)
            return result.returncode
        sys.stdout.write(target.read_text())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
