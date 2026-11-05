"""Place each team together using declared start points in a map copy."""
import itertools
import math
import random
import struct
import xml.etree.ElementTree as ET


def _player_table(data, points, players):
    """Locate a complete MapInfo player table and validate its point references.

    Header fields vary across editor versions; player records and the following
    start-point table are stable. Require one unambiguous structural match.
    """
    if len(data) > 65536 or data[:4] != b"IpaM" or len(data) < 8:
        raise ValueError("地图的 MapInfo 格式不受支持")
    version = struct.unpack_from("<I", data, 4)[0]
    if not 0x18 <= version <= 0x27:
        raise ValueError("地图的 MapInfo 版本暂不支持安全组队")
    matches = []
    for offset in range(8, len(data) - 4):
        count = struct.unpack_from("<I", data, offset)[0]
        if not players <= count <= 16:
            continue
        try:
            cursor, slots = offset + 4, {}
            for _ in range(count):
                pid, control, color = struct.unpack_from("<BII", data, cursor)
                cursor += 9
                if pid > 15 or pid in slots or control > 4 or (color > 31 and color != 0xFFFFFFFF):
                    raise ValueError()
                end = data.index(b"\0", cursor)
                if data[cursor:end] not in (b"", b"Terr", b"Terran", b"Prot", b"Protoss", b"Zerg", b"Rand", b"Random"):
                    raise ValueError()
                cursor = end + 1
                _, start, _ = struct.unpack_from("<III", data, cursor)
                if start and start not in points:
                    raise ValueError()
                slots[pid] = (cursor + 4, control)
                cursor += 12
                end = data.index(b"\0", cursor)
                if end - cursor > 256:
                    raise ValueError()
                cursor = end + 1
            count = struct.unpack_from("<I", data, cursor)[0]
            cursor += 4
            if not players <= count <= 16:
                raise ValueError()
            starts = struct.unpack_from("<" + "I" * count, data, cursor)
            used = struct.unpack_from("<I", data, cursor + count * 4)[0]
            if len(set(starts)) != count or any(p not in points for p in starts) or not players <= used <= count:
                raise ValueError()
            if any(pid not in slots or slots[pid][1] not in (1, 2) for pid in range(1, players + 1)):
                raise ValueError()
            matches.append((slots, starts))
        except (ValueError, struct.error):
            continue
    if len(matches) != 1:
        raise ValueError("无法可靠识别地图的玩家与出生点设置")
    return matches[0]


def _groups(points, sizes, rng):
    """Choose compact teams separated from each other; at most 16 map starts."""
    ids = tuple(points)
    distance = {(a, b): sum((x - y) ** 2 for x, y in zip(points[a], points[b])) for a in ids for b in ids}

    def spread(group):
        return max((distance[a, b] for a, b in itertools.combinations(group, 2)), default=0)

    groups = {size: [(group, spread(group)) for group in itertools.combinations(ids, size)] for size in set(sizes)}
    best, chosen, ties = None, None, 0
    for first, first_spread in groups[sizes[0]]:
        occupied = set(first)
        for second, second_spread in groups[sizes[1]]:
            if occupied.intersection(second):
                continue
            gap = min(distance[a, b] for a in first for b in second)
            score = (max(first_spread, second_spread) / max(gap, 1), -gap)
            if best is None or score < best:
                best, chosen, ties = score, (first, second), 1
            elif score == best:
                ties += 1
                if rng.randrange(ties) == 0:
                    chosen = (first, second)
    if chosen is None:
        raise ValueError("地图没有足够的出生点供双方使用")
    # Vary which team member gets each position without separating teammates.
    return tuple(group[r:] + group[:r] for group in chosen for r in [rng.randrange(len(group))])


def arrange_starts(map_info, objects, teams, rng=None):
    """Return patched MapInfo and player positions, leaving input bytes untouched."""
    if not 2 <= len(teams) <= 8 or teams[0] != 1 or set(teams) != {1, 2}:
        raise ValueError("组队需要双方玩家，总人数不能超过 8 人")
    if len(objects) > 16 << 20:
        raise ValueError("地图出生点资料过大")
    root = ET.fromstring(objects)
    if root.tag != "PlacedObjects":
        raise ValueError("地图缺少标准出生点资料")
    points = {}
    for node in root.iter("ObjectPoint"):
        if node.get("Type") != "StartLoc":
            continue
        pid = int(node.attrib["Id"])
        position = tuple(float(v) for v in node.attrib["Position"].split(",")[:2])
        if pid in points or not 0 < pid < 2 ** 32 or len(position) != 2 or any(not math.isfinite(v) or abs(v) > 65536 for v in position):
            raise ValueError("地图出生点资料无效")
        points[pid] = position
    slots, starts = _player_table(map_info, points, len(teams))
    declared = {pid: points[pid] for pid in starts}
    groups = _groups(declared, (teams.count(1), teams.count(2)), rng or random.SystemRandom())
    available = {1: iter(groups[0]), 2: iter(groups[1])}
    patched, positions = bytearray(map_info), []
    for player, team in enumerate(teams, 1):
        point = next(available[team])
        struct.pack_into("<I", patched, slots[player][0], point)
        positions.append({"player": player, "team": team, "point": point, "position": points[point]})
    return bytes(patched), positions
