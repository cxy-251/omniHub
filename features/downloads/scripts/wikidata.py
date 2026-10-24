"""按 Wikidata 判断艺人是不是女团（纯规则，不用 AI）。由 general.py 调用。

每个名字：搜索 Wikidata 条目（取简介像音乐人的第一个）→ 看结构化字段：
  P31「是什么」含 女团 Q641066 → 女团；含 男团 Q216337、人 Q5 → 不是
  否则看成员 P527 的性别 P21：全是女性 → 女团；有男性 → 不是
  再不行看简介里有没有 girl group / boy band
  还查不出：名字以已确认的女团开头（WJSN CHOCOME → WJSN）算女团的小分队，其余「不确定」
结果缓存在脚本目录旁边的 artist-kinds.json，同一个名字只查一次。请求之间隔 1 秒（Wikidata 要求克制）。
"""
import json
import os
import time
import urllib.parse
import urllib.request

API = "https://www.wikidata.org/w/api.php"
UA = {"User-Agent": "omniHub/0.1 (https://github.com/cxy251/omniHub; personal media library)"}
GIRL_GROUP, BOY_BAND, HUMAN, FEMALE, MALE = "Q641066", "Q216337", "Q5", "Q6581072", "Q6581097"
MUSIC_WORDS = ("group", "band", "singer", "rapper", "duo", "idol", "musician", "unit", "trio")
CACHE = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "artist-kinds.json")
_last = [0.0]


def _get(params):
    wait = _last[0] + 1.0 - time.time()
    if wait > 0:
        time.sleep(wait)
    _last[0] = time.time()
    req = urllib.request.Request(API + "?" + urllib.parse.urlencode({**params, "format": "json"}), headers=UA)
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                return json.load(r)
        except Exception:
            if attempt == 2:
                raise
            time.sleep(3 + attempt * 5)


def _ids(claims, prop):
    return [c["mainsnak"].get("datavalue", {}).get("value", {}).get("id") for c in claims.get(prop, [])]


def _entities(ids):
    out = {}
    for i in range(0, len(ids), 50):   # 一次最多 50 个
        out.update(_get({"action": "wbgetentities", "ids": "|".join(ids[i:i + 50]), "props": "claims"}).get("entities", {}))
    return out


def classify(names, progress=lambda done, total, text: None, log=print):
    """names → {名字: "girl" | "other" | "unknown"}。"""
    try:
        with open(CACHE, encoding="utf-8") as f:
            cache = json.load(f)
    except (OSError, ValueError):
        cache = {}
    todo = [n for n in dict.fromkeys(names) if n and n not in cache]
    found = {}   # 名字 → (条目 ID, 简介)
    for k, name in enumerate(todo):
        progress(k, len(todo), f"查 Wikidata：{name} 是不是女团（{k + 1}/{len(todo)}）")
        try:
            hits = _get({"action": "wbsearchentities", "search": name, "language": "en", "type": "item", "limit": 7}).get("search", [])
        except Exception as e:
            log(f"   Wikidata 查询失败（{name}）：{e}")
            continue
        hit = next((h for h in hits if any(w in (h.get("description") or "").lower() for w in MUSIC_WORDS)), None)
        found[name] = (hit["id"], (hit.get("description") or "").lower()) if hit else (None, "")
    ents = _entities([q for q, _ in found.values() if q]) if found else {}
    members = {}
    for name, (qid, _) in found.items():
        claims = ents.get(qid, {}).get("claims", {}) if qid else {}
        if not ({GIRL_GROUP, BOY_BAND, HUMAN} & set(_ids(claims, "P31"))):
            members[name] = _ids(claims, "P527")[:8]
    member_ents = _entities(sorted({m for ms in members.values() for m in ms if m})) if members else {}
    for name, (qid, desc) in found.items():
        kinds = set(_ids(ents.get(qid, {}).get("claims", {}), "P31")) if qid else set()
        sexes = {(_ids(member_ents.get(m, {}).get("claims", {}), "P21") or [None])[0] for m in members.get(name, [])} - {None}
        if GIRL_GROUP in kinds or (sexes == {FEMALE}) or "girl group" in desc:
            cache[name] = "girl"
        elif kinds & {BOY_BAND, HUMAN} or MALE in sexes or "boy band" in desc or "boy group" in desc or "singer" in desc or "rapper" in desc:
            cache[name] = "other"
        else:
            cache[name] = "unknown"
    try:
        with open(CACHE + ".tmp", "w", encoding="utf-8") as f:
            json.dump(cache, f, ensure_ascii=False, indent=0)
        os.replace(CACHE + ".tmp", CACHE)
    except OSError as e:
        log(f"   艺人类别缓存写不了：{e}")
    result = {n: cache.get(n, "unknown") for n in names if n}
    girls = [n for n, k in result.items() if k == "girl"]
    for n, k in result.items():   # 小分队：名字以确认的女团开头（WJSN CHOCOME、WJSN THE BLACK）
        if k == "unknown" and any(n.casefold().startswith(g.casefold() + " ") for g in girls):
            result[n] = "girl"
    return result
