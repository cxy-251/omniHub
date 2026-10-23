"""歌词：从 LRCLIB（lrclib.net，免费、不用 key）查，尽量要带时间轴的（LRC），没有就要纯文本，再没有就只记状态。由 general.py 调用。

状态（写进音频标签 LYRICS_STATUS，以后按它筛出要单独处理的歌）：
  synced        带时间轴
  plain         只有纯文本
  instrumental  纯音乐，没有歌词
  none          LRCLIB 上查不到
  error         查询失败（网络、服务器忙），以后可以重查
来源（LYRICS_MATCH）：exact 按艺人、曲名、时长（±2 秒）精确对上；search 是搜索来的（艺人对上、时长差 3 秒内），
可能是别的版本，筛选「歌词待处理」时一起列出来核对。
"""
import json
import os
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor

API = "https://lrclib.net/api/"
_lock = threading.Lock()
_last = [0.0]


def _get(path, params):
    """GET LRCLIB；404 返回 None；服务器忙或网络错误重试，最后仍失败抛出异常。两次请求的开始至少隔 0.3 秒。"""
    handlers = []
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        proxy = os.environ["OMNIHUB_YTDLP_PROXY"]
        handlers.append(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    opener = urllib.request.build_opener(*handlers)
    req = urllib.request.Request(API + path + "?" + urllib.parse.urlencode(params), headers={"User-Agent": "omniHub"})
    err = None
    for attempt in range(5):
        with _lock:
            wait = _last[0] + 0.3 - time.time()
            if wait > 0:
                time.sleep(wait)
            _last[0] = time.time()
        try:
            with opener.open(req, timeout=20) as r:
                return json.load(r)
        except urllib.error.HTTPError as e:
            if e.code == 404:
                return None
            err = e
        except Exception as e:   # 网络错误、服务器忙时返回的非 JSON
            err = e
        time.sleep(2 + attempt * 3)
    raise RuntimeError(str(err))


def _result(d, match):
    if d.get("instrumental"):
        return {"status": "instrumental", "text": "", "match": match}
    if (d.get("syncedLyrics") or "").strip():
        return {"status": "synced", "text": d["syncedLyrics"].strip(), "match": match}
    if (d.get("plainLyrics") or "").strip():
        return {"status": "plain", "text": d["plainLyrics"].strip(), "match": match}
    return None


def _norm(s):
    return "".join(ch for ch in (s or "").casefold() if ch.isalnum())


def _same_artist(a, b):
    a, b = _norm(a), _norm(b)
    return bool(a and b) and (a in b or b in a)


def find(names, duration=None):
    """names 是按可信度排的（艺人, 曲名）候选（如 Last.fm 纠正过的、从标题拆的）。
    先按每个候选精确查（带时长，LRCLIB 只认 ±2 秒内的），再搜索，在结果里挑时长接近、带时间轴的。
    搜索结果必须艺人对得上；知道时长时还要差 3 秒以内（不知道时长就不用搜索结果），免得拿到同名歌、翻唱的歌词。
    返回 {"status", "text", "match"}。"""
    names = [(a, t) for a, t in dict.fromkeys(names) if a and t]
    if not names:
        return {"status": "none", "text": "", "match": ""}
    best = None
    try:
        for artist, track in names:
            params = {"artist_name": artist, "track_name": track}
            if duration:
                params["duration"] = int(round(duration))
            d = _get("get", params)
            r = _result(d, "exact") if d else None
            if r and r["status"] in ("synced", "instrumental"):
                return r
            best = best or r
        for artist, track in names if duration else []:
            for d in _get("search", {"artist_name": artist, "track_name": track}) or []:
                if not d.get("duration") or abs(d["duration"] - duration) > 3 or not _same_artist(d.get("artistName"), artist):
                    continue
                r = _result(d, "search")
                if r and r["status"] == "synced":
                    return r
                best = best or r
    except Exception as e:
        print(f"  LRCLIB 查询失败（{names[0][0]} - {names[0][1]}）：{e}", flush=True)
        return best or {"status": "error", "text": "", "match": ""}
    return best or {"status": "none", "text": "", "match": ""}


def find_many(queries, workers=4):
    """并发查一批：queries 是 [(候选列表, 时长)]。"""
    with ThreadPoolExecutor(max_workers=workers) as pool:
        return list(pool.map(lambda q: find(*q), queries))
