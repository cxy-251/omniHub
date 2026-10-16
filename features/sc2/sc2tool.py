#!/usr/bin/env python3
"""
omniHub 星际争霸 2 助手（移植自 omni-deck 的 features/sc2/service.py + runner.py）。
由 Go 服务按需以 uv 子进程调用，不常驻。所有位置由环境变量提供：

  SC2PATH            SC2 安装目录（含 Maps/、Mods/、SC2_x64 版本目录）
  SC2_THUMB_CACHE    地图缩略图缓存目录
  SC2MOD_DIR         sc2Mod 项目目录（bake.py，可选；没有则不提供 mod）
  SC2MOD_COMPAT_DATA Proton/Wine 容器（与其他 Wine 游戏共用，存档与快捷键在里面）
  WINE               proton-wine 垫片路径（burnysc2 把它当 wine 调用）

子命令：
  maps    输出地图清单 JSON（去重、缩略图、人数上限、最多电脑数）
  mods    输出可选 mod JSON
  play    开一局（只有你一个真人，其余全是电脑），结束时打印一行 OMNIHUB_SC2_RESULT {json}
"""
from __future__ import annotations

import argparse
import io
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import List, Optional

SC2_DIR = Path(os.environ.get("SC2PATH", ""))
SC2_MAPS_DIR = SC2_DIR / "Maps"
THUMB_CACHE = Path(os.environ.get("SC2_THUMB_CACHE", "/tmp/omnihub-sc2-thumbs"))
SC2MOD_DIR = os.environ.get("SC2MOD_DIR", "")
THUMB_W = THUMB_H = 300

# ---------------------------------------------------------------- 地图


def _read_mpq_file(map_file: Path, inner_name: str) -> Optional[bytes]:
    """从 .SC2Map（MPQ 归档）读出内部文件；读不到返回 None。"""
    import mpyq
    try:
        return mpyq.MPQArchive(str(map_file)).read_file(inner_name)
    except Exception:
        return None


# MapInfo 是无文档的二进制格式。每个「开放槽位/种族不限」玩家留一条
# [槽位号:1][控制类型=Open:01000000][种族=任意:FFFFFFFF] 记录，从 1 起连续编号。
_SLOT_PAT = re.compile(rb".\x01\x00\x00\x00\xff\xff\xff\xff")


def _map_has_force_data(raw: bytes, n: int) -> bool:
    """真正分了队伍（Force）的地图，MapInfo 里会把出生点按两种顺序各排一遍（N 前缀出现两次）。
    没有队伍数据的图，多个电脑会各自一伙互相敌对，所以只能配 1 个电脑。"""
    if n <= 2 or not raw:
        return False
    return raw.count(struct.pack("<I", n)) >= 2


def _map_max_players(map_file: Path):
    raw = _read_mpq_file(map_file, "MapInfo")
    if not raw:
        return None, False
    idxs = {raw[m.start()] for m in _SLOT_PAT.finditer(raw)}
    n = 0
    while (n + 1) in idxs:
        n += 1
    n = min(n, 8) or None
    return n, (_map_has_force_data(raw, n) if n else False)


def _guess_team(stem: str, map_file: Path):
    """(队伍标签, 人数上限, 最多电脑数)。AIE 后缀是 1v1 天梯图；其余按实测人数分档。"""
    if stem.endswith("AIE"):
        return "1v1", 2, 1
    n, has_force = _map_max_players(map_file)
    if n is None or n <= 2:
        return "1v1", n or 2, 1
    max_opp = (n - 1) if has_force else 1
    if n <= 4:
        return "2v2", n, max_opp
    if n <= 6:
        return "3v3", n, max_opp
    return "4v4+", n, max_opp


def _autocrop_black_margin(im):
    """去掉贴着画布边缘、连成一片的黑边（地图内部的暗色地形不受影响）。"""
    import numpy as np
    from scipy import ndimage
    arr = np.asarray(im.convert("L"), dtype=np.uint8)
    h, w = arr.shape
    labels, n = ndimage.label(arr <= 10, structure=np.ones((3, 3)))
    if n == 0:
        return im
    border = set(labels[0, :]) | set(labels[-1, :]) | set(labels[:, 0]) | set(labels[:, -1])
    border.discard(0)
    content = ~np.isin(labels, list(border)) if border else np.ones_like(arr, dtype=bool)
    if content.sum() < 0.15 * h * w:
        return im
    rows, cols = np.where(content.any(axis=1))[0], np.where(content.any(axis=0))[0]
    pad = max(2, round(0.03 * max(h, w)))
    top, left = max(0, int(rows[0]) - pad), max(0, int(cols[0]) - pad)
    bottom, right = min(h - 1, int(rows[-1]) + pad), min(w - 1, int(cols[-1]) + pad)
    return im.crop((left, top, right + 1, bottom + 1))


def extract_thumb(map_file: Path) -> Optional[Path]:
    """Minimap.tga → 去黑边 → 裁切填满 300×300 → PNG 缓存（地图未变时直接复用）。"""
    from PIL import Image
    out = THUMB_CACHE / f"{map_file.stem}.png"
    if out.exists() and out.stat().st_mtime >= map_file.stat().st_mtime:
        return out
    raw = _read_mpq_file(map_file, "Minimap.tga")
    if not raw:
        return None
    try:
        im = _autocrop_black_margin(Image.open(io.BytesIO(raw)).convert("RGB"))
        scale = max(THUMB_W / im.width, THUMB_H / im.height)
        im = im.resize((round(im.width * scale), round(im.height * scale)), Image.LANCZOS)
        left, top = (im.width - THUMB_W) // 2, (im.height - THUMB_H) // 2
        im = im.crop((left, top, left + THUMB_W, top + THUMB_H))
        THUMB_CACHE.mkdir(parents=True, exist_ok=True)
        tmp = out.with_suffix(".tmp.png")
        im.save(tmp)
        os.replace(tmp, out)
        return out
    except Exception:
        return None


def _fingerprint(map_file: Path):
    from PIL import Image
    import numpy as np
    raw = _read_mpq_file(map_file, "Minimap.tga")
    if not raw:
        return None
    try:
        return np.asarray(Image.open(io.BytesIO(raw)).convert("L").resize((32, 16)), dtype=float)
    except Exception:
        return None


def dedupe_maps(map_files: List[Path]) -> List[Path]:
    """同一地形只留一张（32×16 灰度平均差 < 1.0 视为同图，实测同图不同赛季 ≤0.2、不同图 ≥2.8）。
    烘焙产物（名字含 '__'）不列出。"""
    import numpy as np
    candidates = sorted((p for p in map_files if "__" not in p.stem), key=lambda p: (len(p.stem), p.stem))
    groups = []
    for mf in candidates:
        fp = _fingerprint(mf)
        if fp is not None and any(g is not None and np.abs(g - fp).mean() < 1.0 for g, _ in groups):
            continue
        groups.append((fp, mf))
    return sorted((mf for _, mf in groups), key=lambda p: p.stem.lower())


def cmd_maps(_args) -> None:
    from PIL import Image
    import numpy as np
    out = []
    for mf in dedupe_maps(sorted(SC2_MAPS_DIR.glob("*.SC2Map"))):
        thumb = extract_thumb(mf)
        if thumb is None:
            continue
        try:
            arr = np.asarray(Image.open(thumb).convert("RGB")).reshape(-1, 3)
        except Exception:
            continue
        if arr.std(axis=0).mean() < 2.0:   # 纯色缩略图没有可辨认信息，跳过
            continue
        team, max_players, max_opponents = _guess_team(mf.stem, mf)
        out.append({"stem": mf.stem, "team": team, "max_players": max_players, "max_opponents": max_opponents})
    print(json.dumps(out, ensure_ascii=False))

# ---------------------------------------------------------------- mod


def _import_bake():
    if not SC2MOD_DIR or not os.path.isdir(SC2MOD_DIR):
        raise RuntimeError("未配置 sc2Mod 项目目录")
    if SC2MOD_DIR not in sys.path:
        sys.path.insert(0, SC2MOD_DIR)
    import bake  # noqa
    return bake


def cmd_mods(_args) -> None:
    try:
        bake = _import_bake()
        print(json.dumps([{"key": k, **v} for k, v in bake.MOD_INFO.items()], ensure_ascii=False))
    except Exception:
        print("[]")


def _pfx_docs() -> Path:
    return Path(os.environ["SC2MOD_COMPAT_DATA"]) / "pfx/drive_c/users/steamuser/Documents/StarCraft II"


HOTKEY_PROFILE = "sc2Mod"
HOTKEYS_INI = "[Settings]\n[Hotkeys]\nArmySelect=Grave\nTownCamera=Space\nAlertRecall=Backspace\nPTT=\n"


def install_hotkeys() -> None:
    """` = 全选部队，空格 = 回基地，Backspace = 跳到上个警报。
    只补上缺的这几项：玩家在游戏里自己改过的键原样保留（以前每次开局整份重写，改过的键会被冲掉）。"""
    docs = _pfx_docs()
    (docs / "Hotkeys").mkdir(parents=True, exist_ok=True)
    path = docs / "Hotkeys" / f"{HOTKEY_PROFILE}.SC2Hotkeys"
    if path.exists():
        lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
        have = {ln.split("=", 1)[0].strip() for ln in lines if "=" in ln}
        defaults = [ln for ln in HOTKEYS_INI.splitlines() if "=" in ln and ln.split("=", 1)[0] not in have]
        if defaults:
            if "[Hotkeys]" not in lines:
                lines.append("[Hotkeys]")
            at = lines.index("[Hotkeys]") + 1
            lines[at:at] = defaults
            path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    else:
        path.write_text(HOTKEYS_INI, encoding="utf-8")
    restore_cancel(path)
    var = docs / "Variables.txt"
    lines = var.read_text(encoding="utf-8", errors="replace").splitlines() if var.exists() else []
    lines = [ln for ln in lines if not ln.lower().startswith("hotkeyprofile=")] + [f"hotkeyProfile={HOTKEY_PROFILE}"]
    var.write_text("\n".join(lines) + "\n", encoding="utf-8")


def restore_cancel(path: Path) -> None:
    """「取消」命令被清空（Cancel=）时恢复成 Esc。
    在游戏的快捷键设置里给它重新绑键时，按 Esc 会被当成「清除」，结果 Cancel 变成空，游戏里 Esc 就失灵了。"""
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    section, changed = "", False
    for i, ln in enumerate(lines):
        if ln.startswith("[") and ln.endswith("]"):
            section = ln
        elif section == "[Commands]" and ln.strip() == "Cancel=":
            lines[i], changed = "Cancel=Escape", True
    if changed:
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def sync_mods_to_pfx() -> None:
    """游戏按 Wine 容器里的 Documents/StarCraft II/Mods 解析 mod 依赖，每次开局前同步新版。"""
    dst_dir = _pfx_docs() / "Mods"
    dst_dir.mkdir(parents=True, exist_ok=True)
    for f in (SC2_DIR / "Mods").glob("*.SC2Mod"):
        dst = dst_dir / f.name
        if not dst.exists() or dst.stat().st_mtime < f.stat().st_mtime:
            shutil.copy2(f, dst)

# ---------------------------------------------------------------- 对局


def _sc2_pids() -> List[str]:
    out = subprocess.run(["pgrep", "-f", "SC2_x64.exe"], capture_output=True, text=True, timeout=5).stdout
    return [p for p in out.split() if p.isdigit()]


def _force_kill_sc2() -> None:
    """burnysc2 只结束它直接启动的垫片，真正的 SC2_x64.exe 在 Proton 下好几层，兜底按名字 SIGKILL。"""
    try:
        for pid in _sc2_pids():
            subprocess.run(["kill", "-9", pid], timeout=3)
    except Exception:
        pass


WINDOW_LOST_GRACE = 15


def _sc2_window_states():
    """SC2 名下所有 X 窗口的状态（Normal/Iconic/Withdrawn）；查不到返回 None。"""
    try:
        pids = _sc2_pids()
        if not pids:
            return None
        states = []
        for pid in pids:
            wins = subprocess.run(["xdotool", "search", "--pid", pid], capture_output=True, text=True, timeout=5).stdout.split()
            for w in wins:
                out = subprocess.run(["xprop", "-id", w, "WM_STATE"], capture_output=True, text=True, timeout=5).stdout
                states.append("Normal" if "Normal" in out else "Iconic" if "Iconic" in out else "Withdrawn")
        return states
    except Exception:
        return None


def _watch_window(stop: threading.Event, lost: dict) -> None:
    """窗口出现过之后连续 15 秒既不可见也没最小化，视为闪退并结束对局
    （burnysc2 只看 websocket，进程还在发数据时会一直等下去）。"""
    seen, missing_since = False, None
    while not stop.wait(3):
        states = _sc2_window_states()
        if states is None:
            missing_since = None
            continue
        if any(s in ("Normal", "Iconic") for s in states):
            seen, missing_since = True, None
            continue
        if not seen:
            continue
        missing_since = missing_since or time.monotonic()
        if time.monotonic() - missing_since >= WINDOW_LOST_GRACE:
            lost["reason"] = "SC2 窗口消失（疑似闪退），已结束对局"
            _force_kill_sc2()
            return


def cmd_play(args) -> None:
    os.environ.setdefault("SC2PF", "WineLinux")
    os.environ.setdefault("SC2_TIMEOUT", "180")
    from sc2 import maps
    from sc2.data import AIBuild, Difficulty, Race
    from sc2.main import run_game
    from sc2.player import Computer, Human

    race = {"T": Race.Terran, "P": Race.Protoss, "Z": Race.Zerg, "R": Race.Random}
    diff = {
        "veryeasy": Difficulty.VeryEasy, "easy": Difficulty.Easy, "medium": Difficulty.Medium,
        "mediumhard": Difficulty.MediumHard, "hard": Difficulty.Hard, "harder": Difficulty.Harder,
        "veryhard": Difficulty.VeryHard, "cheatvision": Difficulty.CheatVision,
        "cheatmoney": Difficulty.CheatMoney, "cheatinsane": Difficulty.CheatInsane,
    }
    build = {"random": AIBuild.RandomBuild, "rush": AIBuild.Rush, "timing": AIBuild.Timing,
             "power": AIBuild.Power, "macro": AIBuild.Macro, "air": AIBuild.Air}

    opponents = json.loads(args.opponents)
    mods = [m for m in args.mods.split(",") if m]
    result = {"map": args.map, "race": args.race, "opponents": opponents, "mods": mods}
    stop, lost = threading.Event(), {}
    try:
        if not opponents:
            raise RuntimeError("至少要有一个电脑对手")
        mf = SC2_MAPS_DIR / f"{args.map}.SC2Map"
        if mf.exists():
            _, _, max_opp = _guess_team(args.map, mf)
            if len(opponents) > max_opp:
                raise RuntimeError(f"这张图最多只能配 {max_opp} 个电脑")
        install_hotkeys()
        sync_mods_to_pfx()
        map_name = _import_bake().bake(args.map, mods) if mods else args.map
        players = [Human(race[args.race.upper()], fullscreen=True)]
        for o in opponents:
            players.append(Computer(race[o["race"].upper()], diff[o["difficulty"]],
                                    build.get(o.get("ai_build", "random"), AIBuild.RandomBuild)))
        threading.Thread(target=_watch_window, args=(stop, lost), daemon=True).start()
        # 对局一结束（burnysc2 记下 Result）就先报告结果并结束 SC2：burnysc2 自己在 Proton 下清理进程
        # 要等一分多钟，界面会一直显示「对局进行中」。
        from loguru import logger

        def early_result(message):
            text = message.record["message"]
            if text.startswith("Result for player 1") and "early" not in result:
                outcome = text.rsplit(":", 1)[-1].strip()
                result["early"] = outcome
                print("OMNIHUB_SC2_RESULT " + json.dumps({**result, "result": outcome}, ensure_ascii=False), flush=True)
                threading.Thread(target=_force_kill_sc2, daemon=True).start()

        logger.add(early_result, level="INFO")
        res = run_game(maps.get(map_name), players, realtime=True)
        if lost:
            raise RuntimeError(lost["reason"])
        if isinstance(res, list):
            res = res[0] if res else None
        result["result"] = getattr(res, "name", str(res)) if res is not None else "Unknown"
    except Exception as e:
        if "early" in result:
            # 已经拿到结果后我们主动结束了 SC2，burnysc2 随后报连接断开，不算出错。
            result["result"] = result["early"]
        else:
            result["result"] = "Error"
            result["error"] = lost.get("reason") or str(e)
    finally:
        stop.set()
        _force_kill_sc2()
    result.pop("early", None)
    print("OMNIHUB_SC2_RESULT " + json.dumps(result, ensure_ascii=False), flush=True)


def main() -> None:
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("maps")
    sub.add_parser("mods")
    play = sub.add_parser("play")
    play.add_argument("--map", required=True)
    play.add_argument("--race", required=True, choices=["T", "P", "Z", "R", "t", "p", "z", "r"])
    play.add_argument("--opponents", required=True, help='JSON：[{"race":"T","difficulty":"hard","ai_build":"random"}]')
    play.add_argument("--mods", default="")
    args = p.parse_args()
    {"maps": cmd_maps, "mods": cmd_mods, "play": cmd_play}[args.cmd](args)


if __name__ == "__main__":
    main()
