"""Ten complete, app-owned replay bundles; oldest bundles use the OS trash."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time
import uuid

ID = re.compile(r"[0-9a-f]{32}")

def replay_root():
    return Path(os.environ["SC2_REPLAY_DIR"])

def records(root):
    rows = []
    if not root.exists():
        return rows
    for p in root.iterdir():
        if not ID.fullmatch(p.name) or p.is_symlink() or not p.is_dir():
            continue
        try:
            row = json.loads((p / "record.json").read_text())
            if row.get("id") == p.name and all((p / f).is_file() and not (p / f).is_symlink() for f in ("record.json", "game.SC2Replay", "map.SC2Map")):
                rows.append(row)
        except (OSError, ValueError):
            continue
    return sorted(rows, key=lambda r: (r["created_at"], r["id"]))

def bundle(replay_id):
    root = replay_root()
    if not isinstance(replay_id, str) or not ID.fullmatch(replay_id) or not any(r["id"] == replay_id for r in records(root)):
        raise ValueError("回放不存在或已被队列淘汰")
    return root / replay_id

def save(data, game_map, result):
    if not data:
        raise ValueError("游戏未返回回放数据")
    root = replay_root()
    root.mkdir(parents=True, exist_ok=True)
    replay_id = uuid.uuid4().hex
    pending = root / (".pending-" + replay_id)
    pending.mkdir()
    try:
        (pending / "game.SC2Replay").write_bytes(data)
        shutil.copyfile(game_map, pending / "map.SC2Map")
        row = {**result, "id": replay_id, "created_at": time.time_ns()}
        (pending / "record.json").write_text(json.dumps(row, ensure_ascii=False))
        old = records(root)
        for entry in old[:max(0, len(old) - 9)]:
            subprocess.run(["gio", "trash", "--", str(root / entry["id"])], check=True, capture_output=True, timeout=15)
        pending.rename(root / replay_id)
        return replay_id
    finally:
        if pending.exists():
            shutil.rmtree(pending)  # Only this call's unpublished temporary bundle.
