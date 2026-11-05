"""Apply explicit alliances to a cached map copy; installed maps stay untouched."""
import ctypes
import ctypes.util
import hashlib
import json
import os
from pathlib import Path
import shutil
import tempfile

import mpyq

from starts import arrange_starts


def alliance_script(script, teams):
    """Set both directions, shared vision/help and allied defeat before AI starts."""
    if not 2 <= len(teams) <= 8 or teams[0] != 1 or any(t not in (1, 2) for t in teams) or 2 not in teams:
        raise ValueError("需要至少一个电脑对手，且总人数不能超过 8 人")
    marker = b"MeleeInitAI();"
    if script.count(marker) != 1 or b'TriggerLibs/NativeLib' not in script:
        raise RuntimeError("这张地图没有标准对战初始化，暂不支持自定义组队")
    lines = ["// omniHub explicit teams"]
    for player, team in enumerate(teams, 1):
        for other, other_team in enumerate(teams, 1):
            if player == other:
                continue
            alliance = "AllyWithSharedVisionAndPushable" if team == other_team else "Enemy"
            lines.append(f"libNtve_gf_SetAlliance({player}, {other}, libNtve_ge_AllianceSetting_{alliance});")
    return script.replace(marker, ("\n    ".join(lines) + "\n    MeleeInitAI();").encode())


def storm_library(mod_dir):
    """Use an installed StormLib, including the existing sc2Mod dependency."""
    candidates = []
    if os.environ.get("SC2_STORM_LIB"):
        candidates.append(os.environ["SC2_STORM_LIB"])
    if mod_dir:
        directory = Path(mod_dir) / "lib"
        for pattern in ("libstorm.so*", "libstorm*.dylib", "StormLib.dll"):
            candidates.extend(str(path) for path in sorted(directory.glob(pattern)))
    found = ctypes.util.find_library("storm")
    if found:
        candidates.append(found)
    for path in candidates:
        try:
            return ctypes.CDLL(path)
        except OSError:
            continue
    raise RuntimeError("自定义组队需要 StormLib；请安装或配置现有 sc2Mod 的 lib 目录")


def reusable_controls(script, teams):
    """End through GameOver with a result dialog, keeping the API engine alive.

    The built-in surrender skips the API result and tears down the simulation.
    A confirmed in-game button ends all AI players first, then the human, so a
    surviving AI ally cannot keep the round running after the human resigns.
    """
    include = b'include "TriggerLibs/NativeLib"'
    marker = b"MeleeInitOptions();"
    if script.count(include) != 1 or script.count(marker) != 1:
        raise RuntimeError("这张地图不支持保留窗口所需的标准对战菜单")
    declarations = """
int ohSC2ExitDialog;
int ohSC2ConfirmDialog;
int ohSC2ExitButton;
int ohSC2ConfirmButton;
int ohSC2CancelButton;
trigger ohSC2ExitTrigger;
bool ohSC2Exit (bool testConds, bool runActions) {
    if (!runActions) { return true; }
    if (EventDialogControl() == ohSC2ExitButton) {
        DialogSetVisible(ohSC2ConfirmDialog, PlayerGroupSingle(1), true);
        return true;
    }
    DialogSetVisible(ohSC2ConfirmDialog, PlayerGroupSingle(1), false);
    if (EventDialogControl() != ohSC2ConfirmButton) { return true; }
    DialogSetVisible(ohSC2ExitDialog, PlayerGroupSingle(1), false);
"""
    for player, team in enumerate(teams[1:], 2):
        outcome = "c_gameOverDefeat" if team == 1 else "c_gameOverVictory"
        declarations += f"    GameOver({player}, {outcome}, false, false);\n"
    declarations += "    GameOver(1, c_gameOverDefeat, true, false);\n    return true;\n}\n"
    init = """
    // A normal defeat dialog leaves the API responsive for the next create_game.
    ohSC2ExitDialog = DialogCreate(240, 54, c_anchorTop, 0, 40, false);
    DialogSetImageVisible(ohSC2ExitDialog, false);
    ohSC2ExitButton = DialogControlCreate(ohSC2ExitDialog, c_triggerControlTypeButton);
    DialogControlSetSize(ohSC2ExitButton, PlayerGroupSingle(1), 240, 54);
    DialogControlSetPosition(ohSC2ExitButton, PlayerGroupSingle(1), c_anchorCenter, 0, 0);
    DialogControlSetPropertyAsText(ohSC2ExitButton, c_triggerControlPropertyText, PlayerGroupSingle(1), StringToText("认输 / 返回大厅"));
    DialogSetVisible(ohSC2ExitDialog, PlayerGroupSingle(1), true);
    ohSC2ConfirmDialog = DialogCreate(400, 180, c_anchorCenter, 0, 0, true);
    DialogSetTitle(ohSC2ConfirmDialog, StringToText("确认认输？"));
    ohSC2ConfirmButton = DialogControlCreate(ohSC2ConfirmDialog, c_triggerControlTypeButton);
    DialogControlSetSize(ohSC2ConfirmButton, PlayerGroupSingle(1), 160, 50);
    DialogControlSetPosition(ohSC2ConfirmButton, PlayerGroupSingle(1), c_anchorBottomLeft, 24, 30);
    DialogControlSetPropertyAsText(ohSC2ConfirmButton, c_triggerControlPropertyText, PlayerGroupSingle(1), StringToText("确认认输"));
    ohSC2CancelButton = DialogControlCreate(ohSC2ConfirmDialog, c_triggerControlTypeButton);
    DialogControlSetSize(ohSC2CancelButton, PlayerGroupSingle(1), 160, 50);
    DialogControlSetPosition(ohSC2CancelButton, PlayerGroupSingle(1), c_anchorBottomRight, 24, 30);
    DialogControlSetPropertyAsText(ohSC2CancelButton, c_triggerControlPropertyText, PlayerGroupSingle(1), StringToText("继续对局"));
    ohSC2ExitTrigger = TriggerCreate("ohSC2Exit");
    TriggerAddEventDialogControl(ohSC2ExitTrigger, 1, ohSC2ExitButton, c_triggerControlEventTypeClick);
    TriggerAddEventDialogControl(ohSC2ExitTrigger, 1, ohSC2ConfirmButton, c_triggerControlEventTypeClick);
    TriggerAddEventDialogControl(ohSC2ExitTrigger, 1, ohSC2CancelButton, c_triggerControlEventTypeClick);
"""
    # Hide native routes that destroy the simulation without an API result.
    for item in ("AbortButton", "EndGameButton", "RestartButton", "LoadButton", "LoadLastButton", "LoadReplayButton", "QuitButton", "ExitButton"):
        init += f"    UISetGameMenuItemVisible(PlayerGroupSingle(1), c_gameMenuDialog{item}, false);\n"
    return script.replace(include, include + declarations.encode()).replace(marker, marker + init.encode())


def team_map(source, teams, cache, mod_dir="", *, reusable=False):
    """Write only to cache, publishing a complete MPQ after the script verifies."""
    source, cache = Path(source), Path(cache)
    archive = mpyq.MPQArchive(str(source))
    script = archive.read_file("MapScript.galaxy")
    if not script:
        raise RuntimeError("地图缺少对战脚本，暂不支持自定义组队")
    patched = alliance_script(script, teams)
    if reusable:
        patched = reusable_controls(patched, teams)
    map_info, objects = archive.read_file("MapInfo"), archive.read_file("Objects")
    if not map_info or not objects:
        raise RuntimeError("地图缺少玩家或出生点资料，无法安全安排队友出生")
    map_info, positions = arrange_starts(map_info, objects, teams)
    print("OMNIHUB_SC2_TEAMS " + json.dumps(positions, ensure_ascii=False), flush=True)
    info = source.stat()
    key = hashlib.sha256(str(source.resolve()).encode() + str((info.st_size, info.st_mtime_ns)).encode() + patched + map_info).hexdigest()[:20]
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / f"teams-{key}.SC2Map"
    if target.is_file():
        return target
    storm = storm_library(mod_dir)
    storm.SFileOpenArchive.argtypes = [ctypes.c_char_p, ctypes.c_uint, ctypes.c_uint, ctypes.POINTER(ctypes.c_void_p)]
    storm.SFileOpenArchive.restype = ctypes.c_bool
    storm.SFileAddFileEx.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint, ctypes.c_uint, ctypes.c_uint]
    storm.SFileAddFileEx.restype = ctypes.c_bool
    storm.SFileCloseArchive.argtypes = [ctypes.c_void_p]
    storm.SFileCloseArchive.restype = ctypes.c_bool
    with tempfile.TemporaryDirectory(prefix="teams-", dir=cache) as temporary:
        copy = Path(temporary) / "match.SC2Map"
        payload = Path(temporary) / "MapScript.galaxy"
        shutil.copyfile(source, copy)
        payload.write_bytes(patched)
        handle = ctypes.c_void_p()
        if not storm.SFileOpenArchive(os.fsencode(copy), 0, 0, ctypes.byref(handle)):
            raise RuntimeError("无法打开地图副本，原地图未修改")
        try:
            if not storm.SFileAddFileEx(handle, os.fsencode(payload), b"MapScript.galaxy", 0x80000200, 2, 2):
                raise RuntimeError("无法写入组队脚本，原地图未修改")
            payload.write_bytes(map_info)
            if not storm.SFileAddFileEx(handle, os.fsencode(payload), b"MapInfo", 0x80000200, 2, 2):
                raise RuntimeError("无法写入出生点设置，原地图未修改")
        finally:
            closed = storm.SFileCloseArchive(handle)
        checked = mpyq.MPQArchive(str(copy))
        if not closed or checked.read_file("MapScript.galaxy") != patched or checked.read_file("MapInfo") != map_info:
            raise RuntimeError("组队地图副本校验失败，原地图未修改")
        os.replace(copy, target)
    return target
