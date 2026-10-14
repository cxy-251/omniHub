# /// script
# requires-python = ">=3.10"
# dependencies = []
# ///
"""Stream a prepared image directory to CBZ; never remove/replace source assets."""
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time
import zipfile

MAX_FILES = 10000
MAX_ENTRIES = 20000
MAX_IMAGE = 32 << 20
MAX_TOTAL = 2 << 30
IMAGE_EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp", ".gif"}


CANCELLED = False


def cancelled(_signum, _frame):
    global CANCELLED
    CANCELLED = True


def check_cancelled():
    if CANCELLED:
        raise InterruptedError("task cancelled")


def natural_key(value):
    return tuple(int(part) if part.isdigit() else part.lower()
                 for part in re.split(r"([0-9]+)", value)), value


def collect(root):
    """Collect bounded metadata only; source file handles are opened on demand."""
    images, infos, names = [], {}, set()
    visited, name_bytes, scan_bytes, total = 0, 0, 0, 0
    pending = [(".", 0)]
    while pending:
        check_cancelled()
        directory, depth = pending.pop()
        if depth > 32:
            raise ValueError("source directory depth exceeds 32")
        with os.scandir(directory) as entries:
            for entry in entries:
                check_cancelled()
                visited += 1
                if visited > MAX_ENTRIES:
                    raise ValueError("source has too many entries")
                if entry.is_symlink():
                    raise ValueError("source symlinks are not accepted")
                if entry.name.startswith("."):
                    continue
                path = Path(entry.path)
                relative = path.as_posix()
                encoded_length = len(relative.encode("utf-8"))
                scan_bytes += encoded_length
                if encoded_length > 4096 or scan_bytes > 8 << 20:
                    raise ValueError("source path metadata exceeds capacity")
                if not path.resolve().is_relative_to(root):
                    raise ValueError("source escaped its directory")
                if entry.is_dir(follow_symlinks=False):
                    pending.append((relative, depth + 1))
                    continue
                if path.suffix.lower() not in IMAGE_EXTENSIONS and relative not in {"metadata.json", "ComicInfo.xml"}:
                    continue
                info = entry.stat(follow_symlinks=False)
                if not stat.S_ISREG(info.st_mode):
                    raise ValueError("source must be a regular file")
                limit = MAX_IMAGE if path.suffix.lower() in IMAGE_EXTENSIONS else 1 << 20
                if info.st_size > limit:
                    raise ValueError("source entry exceeds size capacity")
                name_bytes += len(relative.encode("utf-8"))
                total += info.st_size
                if len(relative.encode("utf-8")) > 4096 or name_bytes > 4 << 20 or total > MAX_TOTAL:
                    raise ValueError("source exceeds archive capacity")
                if relative.casefold() in names:
                    raise ValueError("case-insensitive archive entry collision")
                names.add(relative.casefold())
                infos[relative] = info
                if path.suffix.lower() in IMAGE_EXTENSIONS:
                    images.append(relative)
                    if len(images) > MAX_FILES:
                        raise ValueError("too many source images")
    cover = "cover.jpg" if "cover.jpg" in infos else "cover.webp" if "cover.webp" in infos else None
    pages = [name for name in images if not ("/" not in name and name.lower().startswith("cover."))]
    if not cover or not pages:
        raise ValueError("prepare root cover.jpg/cover.webp and at least one reading page")
    return sorted(images, key=natural_key), infos


def open_checked(name, expected):
    descriptor = os.open(name, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0))
    source = os.fdopen(descriptor, "rb")
    actual = os.fstat(source.fileno())
    if not stat.S_ISREG(actual.st_mode) or not os.path.samestat(actual, expected) or actual.st_size != expected.st_size or actual.st_mtime_ns != expected.st_mtime_ns:
        source.close()
        raise ValueError("source changed before reading")
    return source


def image_header(name, block):
    extension = Path(name).suffix.lower()
    valid = (block.startswith(b"\xff\xd8\xff") if extension in {".jpg", ".jpeg"} else
             block.startswith(b"\x89PNG\r\n\x1a\n") if extension == ".png" else
             block[:6] in {b"GIF87a", b"GIF89a"} if extension == ".gif" else
             block.startswith(b"RIFF") and block[8:12] == b"WEBP" if extension == ".webp" else False)
    if not valid:
        raise ValueError("image header does not match its filename")


def copy_entry(archive, name, expected):
    with open_checked(name, expected) as source, archive.open(name, "w", force_zip64=True) as target:
        copied = 0
        while block := source.read(64 << 10):
            check_cancelled()
            if copied == 0 and Path(name).suffix.lower() in IMAGE_EXTENSIONS:
                image_header(name, block)
            copied += len(block)
            if copied > expected.st_size:
                raise ValueError("source grew while packing")
            target.write(block)
        actual = os.fstat(source.fileno())
        if (copied == 0 and Path(name).suffix.lower() in IMAGE_EXTENSIONS) or copied != expected.st_size or actual.st_mtime_ns != expected.st_mtime_ns or actual.st_size != expected.st_size:
            raise ValueError("source changed while packing")


def pack(output):
    if len(sys.argv) != 2 or Path(output).name != output or len(output.encode("utf-8")) > 240 or output.startswith(".") or any(ch in output for ch in "/\\\0\r\n") or not output.lower().endswith(".cbz"):
        raise ValueError("output must be a local .cbz filename")
    if os.path.lexists(output):
        raise FileExistsError("output already exists; no file will be replaced")
    root = Path.cwd().resolve()
    images, infos = collect(root)
    cover = "cover.jpg" if "cover.jpg" in infos else "cover.webp"
    with open_checked(cover, infos[cover]) as source:
        image_header(cover, source.read(12))
    print(f"INFO packing {len(images)} images; originals remain untouched", flush=True)
    if "metadata.json" in infos:
        with open_checked("metadata.json", infos["metadata.json"]) as source:
            metadata = source.read((1 << 20) + 1)
        if len(metadata) > 1 << 20 or not isinstance(json.loads(metadata), dict):
            raise ValueError("invalid metadata.json")
    else:
        metadata = json.dumps({"title": root.name}, ensure_ascii=False).encode("utf-8")
    publisher = os.environ.get("OMNIHUB_TASK_PUBLISHER")
    if not publisher:
        raise ValueError("run this task through omniHub")
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(prefix=".omnihub-pack-", suffix=".tmp", dir=".", delete=False) as target:
            temporary = Path(target.name).name
            check_cancelled()
            with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_STORED, allowZip64=True) as archive:
                archive.writestr("metadata.json", metadata)
                if "ComicInfo.xml" in infos:
                    copy_entry(archive, "ComicInfo.xml", infos["ComicInfo.xml"])
                last = time.monotonic()
                for index, name in enumerate(images, 1):
                    copy_entry(archive, name, infos[name])
                    if time.monotonic() - last >= 1:
                        print(f"INFO packed {index}/{len(images)} images", flush=True)
                        last = time.monotonic()
            target.flush()
            os.fsync(target.fileno())
        check_cancelled()
        subprocess.run([publisher, "--publish-cbz", temporary, output], check=True)
        temporary = None
        print(f"INFO published {output}; originals preserved", flush=True)
    finally:
        # Only this invocation's generated temporary is removed, never images
        # or any existing archive. A forced kill may leave a hidden temp file.
        if temporary and os.path.lexists(temporary):
            os.unlink(temporary)


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, cancelled)
    signal.signal(signal.SIGINT, cancelled)
    try:
        if len(sys.argv) != 2:
            raise ValueError("one output filename is required")
        pack(sys.argv[1])
    except (OSError, ValueError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        print(f"ERROR {error}", file=sys.stderr, flush=True)
        sys.exit(1)
