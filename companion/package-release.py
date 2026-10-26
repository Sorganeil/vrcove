#!/usr/bin/env python3
"""Package only the desktop app binaries, user documentation and license; no local state."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import sys
import tarfile
import time
import zipfile


def package(tag: str) -> None:
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?", tag):
        raise ValueError("invalid release tag")
    base = Path(__file__).resolve().parent
    epoch = int(os.environ["SOURCE_DATE_EPOCH"])
    out = base / "dist" / "releases" / tag
    out.mkdir(parents=True, exist_ok=True)
    archives = []
    targets = [
        ("vrcove-desktop", "linux", "vrcove-desktop"),
        ("vrcove-desktop", "windows", "vrcove-desktop.exe"),
    ]
    for app_name, platform, binary in targets:
        root = f"{app_name}-{tag}-{platform}-amd64"
        # Read all inputs before opening output: missing builds fail cleanly.
        entries = {
            binary: (base / "dist" / binary).read_bytes(),
            "README.md": (base / "README.md").read_bytes(),
            "LICENSE": (base.parent / "LICENSE").read_bytes(),
            "BUILD-INFO.txt": (
                f"Version: {tag}\nCommit: {os.environ['COMMIT']}\n"
                f"Built: {os.environ['BUILD_DATE']}\nPlatform: {platform}/amd64\n"
            ).encode(),
        }
        if platform == "linux":
            dest = out / f"{root}.tar.gz"
            with dest.open("wb") as raw:
                with gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=epoch, compresslevel=9) as gz:
                    with tarfile.open(fileobj=gz, mode="w") as archive:
                        for name, data in sorted(entries.items()):
                            info = tarfile.TarInfo(f"{root}/{name}")
                            info.size, info.mtime = len(data), epoch
                            info.mode = 0o755 if name == binary else 0o644
                            archive.addfile(info, io.BytesIO(data))
        else:
            dest = out / f"{root}.zip"
            with zipfile.ZipFile(dest, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
                # ZIP's timestamp format starts in 1980 and uses whole seconds.
                stamp = time.gmtime(max(epoch, 315532800))[:6]
                for name, data in sorted(entries.items()):
                    info = zipfile.ZipInfo(f"{root}/{name}", date_time=stamp)
                    info.compress_type = zipfile.ZIP_DEFLATED
                    info.external_attr = 0o644 << 16
                    archive.writestr(info, data, compresslevel=9)
        archives.append(dest)
    (out / "SHA256SUMS").write_text("".join(
        f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"
        for path in archives
    ))
    for path in archives:
        print(f"{path.name}: {path.stat().st_size / 1048576:.2f} MiB compressed")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: package-release.py vMAJOR.MINOR.PATCH[-prerelease]")
    package(sys.argv[1])
