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
import asyncio
import io
import json
import os
import re
import shutil
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


def _map_max_players(map_file: Path):
    raw = _read_mpq_file(map_file, "MapInfo")
    if not raw:
        return None
    idxs = {raw[m.start()] for m in _SLOT_PAT.finditer(raw)}
    n = 0
    while (n + 1) in idxs:
        n += 1
    n = min(n, 8) or None
    return n


def _guess_team(stem: str, map_file: Path):
    """(队伍标签, 人数上限, 最多电脑数)。AIE 后缀是 1v1 天梯图；其余按实测人数分档。"""
    if stem.endswith("AIE"):
        return "1v1", 2, 1
    n = _map_max_players(map_file)
    if n is None or n <= 2:
        return "1v1", n or 2, 1
    max_opp = n - 1
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


def _sc2_pids(port: int) -> List[str]:
    """仅查本局 API 端口对应的游戏进程，不匹配 Proton 包装器或其他 SC2。"""
    found = []
    for proc in Path("/proc").iterdir():
        if not proc.name.isdigit():
            continue
        try:
            args = proc.joinpath("cmdline").read_bytes().split(b"\0")
            exe = args[0].replace(b"\\", b"/").rsplit(b"/", 1)[-1].lower()
            if exe != b"sc2_x64.exe":
                continue
            if any(a == b"-port" and b == str(port).encode() for a, b in zip(args, args[1:])):
                found.append(proc.name)
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            continue
    return found


def _force_kill_sc2(port: int) -> None:
    """burnysc2 只结束 Proton 垫片；同时结束本局实际游戏，避免留下黑窗口。"""
    try:
        for pid in _sc2_pids(port):
            subprocess.run(["kill", "-9", pid], timeout=3)
    except Exception:
        pass


WINDOW_LOST_GRACE = 15


def _sc2_window_states(port: int):
    """SC2 名下所有 X 窗口的状态（Normal/Iconic/Withdrawn）；查不到返回 None。"""
    try:
        pids = _sc2_pids(port)
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


def _watch_window(stop: threading.Event, lost: dict, port: int) -> None:
    """窗口出现过之后连续 15 秒既不可见也没最小化，视为闪退并结束对局
    （burnysc2 只看 websocket，进程还在发数据时会一直等下去）。"""
    seen, missing_since = False, None
    while not stop.wait(3):
        states = _sc2_window_states(port)
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
            _force_kill_sc2(port)
            return


def _replay_files(directory: Path):
    """记录回放文件身份，只用本次开局后生成或替换的文件判断结束。"""
    files = {}
    for path in directory.rglob("*.SC2Replay"):
        try:
            info = path.stat()
            files[path] = (info.st_mtime_ns, info.st_size)
        except FileNotFoundError:
            continue
    return files


def _has_finished_replay(directory: Path, before: dict, map_path: Path) -> bool:
    """API 卡住时，以本局完整回放为退出凭据；不读取或改写已有回放。"""
    import mpyq
    marker = str(map_path).replace("\\", "/").casefold()
    for path, identity in _replay_files(directory).items():
        if before.get(path) == identity:
            continue
        try:
            details = mpyq.MPQArchive(str(path)).read_file("replay.details")
            if details and marker in details.decode("utf-8", errors="ignore").replace("\\", "/").casefold():
                return True
        except Exception:
            # 写入未完成的 MPQ 不能作为结束凭据，下次再读。
            continue
    return False


async def _run_match(game_map, players, stop: threading.Event, lost: dict) -> str:
    """运行一局真人对战；认输退出及无响应时也保证关闭本局的 Proton 游戏。"""
    from loguru import logger
    from sc2.client import Client
    from sc2.sc2process import SC2Process

    replays = _pfx_docs() / "Replays"
    before = _replay_files(replays)
    process = SC2Process(fullscreen=players[0].fullscreen)
    port = process._port
    threading.Thread(target=_watch_window, args=(stop, lost, port), daemon=True).start()
    pending = None
    try:
        async with process as server:
            try:
                await server.ping()
                created = await server.create_game(game_map, players, realtime=True)
                if created.create_game.HasField("error"):
                    raise RuntimeError(f"无法创建对局：{created.create_game}")
                client = Client(server._ws)
                player = await client.join_game(race=players[0].race)
                while True:
                    pending = asyncio.create_task(client.observation())
                    started = time.monotonic()
                    while not (await asyncio.wait({pending}, timeout=2))[0]:
                        # 暂停/最小化仍正常回复 observation。真人从游戏菜单认输时，
                        # SC2 却会保存回放并停止回复，而不发送 player_result。
                        if _has_finished_replay(replays, before, game_map.relative_path):
                            logger.info("游戏界面已退出，本局回放已保存")
                            return "Quit"
                        if time.monotonic() - started >= 30:
                            raise RuntimeError("游戏状态接口超过 30 秒未响应，已关闭本局 SC2")
                    pending.result()
                    if client._game_result:
                        return client._game_result[player].name
                    await asyncio.sleep(0.2)
            finally:
                # 必须先杀实际游戏再让 burnysc2 清理包装器。不要直接取消 observation：
                # 它的取消处理还会等一次响应，在认输后同样会无限等待。
                stop.set()
                _force_kill_sc2(port)
                if pending is not None:
                    if not pending.done():
                        await server._ws.close()
                    await asyncio.gather(pending, return_exceptions=True)
    finally:
        stop.set()
        _force_kill_sc2(port)


def _prepare_match(args):
    os.environ.setdefault("SC2PF", "WineLinux")
    os.environ.setdefault("SC2_TIMEOUT", "180")
    from sc2 import maps
    from sc2.data import AIBuild, Difficulty, Race
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
    teams = [1] + [o.get("team") or 2 for o in opponents]
    mods = [m for m in args.mods.split(",") if m]
    if not re.fullmatch(r"[^/\\\x00-\x1f]{1,128}", args.map) or ".." in args.map:
        raise ValueError("无效地图名")
    if any(not re.fullmatch(r"[A-Za-z0-9_]{1,64}", m) for m in mods):
        raise ValueError("无效 mod")
    if not opponents or len(teams) > 8 or any(type(t) is not int or t not in (1, 2) for t in teams) or 2 not in teams:
        raise RuntimeError("至少要有一个电脑对手")
    mf = SC2_MAPS_DIR / f"{args.map}.SC2Map"
    if mf.exists():
        _, _, max_opp = _guess_team(args.map, mf)
        if len(opponents) > max_opp:
            raise RuntimeError(f"这张图最多只能配 {max_opp} 个电脑")
    map_name = _import_bake().bake(args.map, mods) if mods else args.map
    game_map = maps.get(map_name)
    reusable = getattr(args, "reusable", False)
    if len(teams) > 2 or reusable:
        from teams import team_map
        cache = Path(os.environ.get("SC2_MATCH_CACHE", str(THUMB_CACHE.parent / "matches")))
        prepared = team_map(game_map.path, teams, cache, SC2MOD_DIR, reusable=reusable)
        game_map = maps.Map(prepared)
        if os.environ.get("SC2PF") == "WineLinux":
            game_map.relative_path = Path("Z:" + str(prepared.resolve()).replace("/", "\\"))
    install_hotkeys()
    sync_mods_to_pfx()
    players = [Human(race[args.race.upper()], fullscreen=True)]
    for o in opponents:
        players.append(Computer(race[o["race"].upper()], diff[o["difficulty"]],
                                build.get(o.get("ai_build", "random"), AIBuild.RandomBuild)))
    return game_map, players


def cmd_play(args) -> None:
    result = {"map": args.map, "race": args.race, "opponents": json.loads(args.opponents),
              "mods": [m for m in args.mods.split(",") if m]}
    stop, lost = threading.Event(), {}
    try:
        if os.environ.get("SC2_HOST_SOCKET"):
            from resident import forward_match
            result = asyncio.run(forward_match(os.environ["SC2_HOST_SOCKET"], result))
            print("OMNIHUB_SC2_RESULT " + json.dumps(result, ensure_ascii=False), flush=True)
            return
        game_map, players = _prepare_match(args)
        result["result"] = asyncio.run(_run_match(game_map, players, stop, lost))
        if lost:
            raise RuntimeError(lost["reason"])
    except Exception as e:
        result["result"] = "Error"
        result["error"] = lost.get("reason") or str(e)
    finally:
        stop.set()
    print("OMNIHUB_SC2_RESULT " + json.dumps(result, ensure_ascii=False), flush=True)


def main() -> None:
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("maps")
    sub.add_parser("mods")
    play = sub.add_parser("play")
    play.add_argument("--map", required=True)
    play.add_argument("--race", required=True, choices=["T", "P", "Z", "R", "t", "p", "z", "r"])
    play.add_argument("--opponents", required=True, help='JSON：[{"race":"T","difficulty":"hard","ai_build":"random","team":2}]；team=1 为队友，2 为对手')
    play.add_argument("--mods", default="")
    args = p.parse_args()
    {"maps": cmd_maps, "mods": cmd_mods, "play": cmd_play}[args.cmd](args)


if __name__ == "__main__":
    main()
