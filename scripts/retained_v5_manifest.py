#!/usr/bin/env python3
"""Record a private retained-build manifest using only the Python standard library."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    artifact = Path(sys.argv[1]).resolve(strict=True)
    revision = sys.argv[2]
    source = artifact / "source"
    if output("git", "-C", str(source), "rev-parse", "HEAD") != revision:
        raise SystemExit("retained checkout is not the pinned commit")
    if output("git", "-C", str(source), "status", "--porcelain", "--untracked-files=all"):
        raise SystemExit("retained checkout is modified")
    artifacts = []
    for name in ("monee-v5", "database-backup-v5", "fixed-schema5-tests"):
        path = artifact / name
        artifacts.append({
            "name": name,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "buildInfo": output("go", "version", "-m", str(path)).replace(str(path), name, 1),
        })
    manifest = {
        "format": 1,
        "revision": revision,
        "tree": output("git", "-C", str(source), "rev-parse", "HEAD^{tree}"),
        "goVersion": output("go", "-C", str(source / "server"), "version"),
        "artifacts": artifacts,
    }
    # Exclusive creation: an existing manifest is never silently replaced.
    with (artifact / "manifest.json").open("x", encoding="utf-8") as handle:
        json.dump(manifest, handle, ensure_ascii=False, indent=2)
        handle.write("\n")


if __name__ == "__main__":
    main()
