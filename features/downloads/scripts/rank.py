"""按质量挑选：读一次列表页 → （音乐）并发查 Last.fm 重播率 → 排名 → 每个链接取前 N 个。由 general.py 调用。
不逐个打开视频页（每个要几秒，几百首要十几分钟）。
音乐里的完整专辑（YouTube Music 专辑）整张下载、按原曲序，不排名；只有单曲（歌单、频道里的）才看重播率。

分数：
  replay  重播率 = Last.fm 总播放数 ÷ 听众数：听过的人平均听了几遍，反映「耐不耐听」，不受推广影响（只用于音乐，需要 API key）
  views   播放量（列表页自带）
没有分数的条目（数据缺失、Last.fm 查不到）不参与排名，日志里列出来。
"""
import json
import os
import re
import threading
import time
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor

from yt_dlp import YoutubeDL


class Quiet:
    def debug(self, msg):
        pass

    def info(self, msg):
        pass

    def warning(self, msg):
        pass

    def error(self, msg):
        print("  出错：" + str(msg)[:200], flush=True)


def ydl_opts(cookies, extra):
    opts = {"quiet": True, "no_warnings": True, "logger": Quiet(), "skip_download": True, "ignoreerrors": True}
    if cookies:
        opts["cookiefile"] = cookies
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        opts["proxy"] = os.environ["OMNIHUB_YTDLP_PROXY"]
    opts.update(extra)
    return opts


def is_album(url, info):
    """完整专辑：YouTube Music 的专辑列表（ID 以 OLAK5uy_ 开头）或专辑页（/browse/MPREb_）。"""
    pid = str(info.get("id") or "")
    return pid.startswith("OLAK5uy_") or "list=OLAK5uy_" in url or "/browse/MPREb_" in url


def extract_list(url, cookies, pool):
    """提取列表里的单个视频链接（只读列表页）。返回 (列表名, [{id, url, title}], 是不是完整专辑)。
    专辑不截断（要整张）。"""
    if "list=OLAK5uy_" in url or "/browse/MPREb_" in url:
        pool = 0
    with YoutubeDL(ydl_opts(cookies, {"extract_flat": "in_playlist", "playlistend": pool or None})) as ydl:
        info = ydl.extract_info(url, download=False) or {}
    if not info:
        raise RuntimeError("读不出列表（网站拒绝或需要登录：到「下载 → 账号与 Cookie」读取该平台的 Cookie）")
    if info.get("_type") not in ("playlist", "multi_video"):   # 单个视频：原链接留着（Shorts 要靠它认出来）
        return info.get("title") or "", [{"id": str(info.get("id") or ""), "url": info.get("webpage_url") or url, "title": info.get("title") or "",
                                          "uploader": info.get("channel") or info.get("uploader") or "", "views": info.get("view_count"),
                                          "source": url}], False
    out = []
    for e in info.get("entries") or []:
        if not e:
            continue
        u = e.get("url") or e.get("webpage_url") or ""
        if u and not u.startswith("http") and e.get("ie_key") == "Youtube":
            u = "https://www.youtube.com/watch?v=" + u
        if u:
            out.append({"id": str(e.get("id") or ""), "url": u, "title": e.get("title") or "",
                        "uploader": e.get("channel") or e.get("uploader") or "", "views": e.get("view_count")})
    if any(not e["title"] and "bilibili.com" in e["url"] for e in out):
        fill_bilibili(out)
    title = re.sub(r"^Album - ", "", info.get("title") or info.get("channel") or "")
    if not title and out and "bilibili.com" in url:   # B 站个人空间：列表没有名字，用 UP 主名
        title = out[0].get("uploader") or ""
    return title, out, is_album(url, info)


def fill_bilibili(entries):
    """B 站个人空间、合集的列表只有视频编号：用 B 站公开的视频信息接口（不用登录）补标题、UP 主、播放量，
    两次请求隔 0.3 秒。失败的留空。"""
    handlers = []
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        proxy = os.environ["OMNIHUB_YTDLP_PROXY"]
        handlers.append(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    opener = urllib.request.build_opener(*handlers)
    headers = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) omniHub", "Referer": "https://www.bilibili.com/"}
    for e in entries:
        m = re.search(r"(BV[0-9A-Za-z]{10})", e["url"])
        if e["title"] or not m:
            continue
        try:
            req = urllib.request.Request("https://api.bilibili.com/x/web-interface/view?bvid=" + m.group(1), headers=headers)
            with opener.open(req, timeout=20) as r:
                data = (json.load(r) or {}).get("data") or {}
            e["title"] = data.get("title") or ""
            e["uploader"] = (data.get("owner") or {}).get("name") or e.get("uploader") or ""
            e["views"] = (data.get("stat") or {}).get("view")
        except Exception as err:
            print(f"  B 站视频信息没取到（{m.group(1)}）：{err}", flush=True)
        time.sleep(0.3)


def guess_artist_track(d):
    """艺人与曲名：YouTube Music 的视频直接有；否则从「艺人 - 曲名」样式的标题里拆，再不行用频道名去掉 - Topic。"""
    if d.get("artist") and d.get("track"):
        return d["artist"], d["track"]
    noise = r"(official|visuali[sz]er|video|audio|lyric|lyrics|mv|m/v|hd|4k|live|performance|官方|歌词|完整版)"
    title = re.sub(r"[\(\[（【][^\)\]）】]*" + noise + r"[^\)\]）】]*[\)\]）】]", "", d["title"], flags=re.I)
    # 去掉结尾「– Official Visualizer」「| Lyrics」这类说明段
    title = re.sub(r"\s+[-–—|]\s+[^-–—|]*" + noise + r"[^-–—|]*$", "", title, flags=re.I).strip()
    m = re.match(r"^(.+?)\s+[-–—|]\s+(.+)$", title)
    if m:
        artist, track = m.group(1).strip(), m.group(2).strip()
        quoted = re.search(r"[\"“「『]([^\"”」』]+)[\"”」』]", track)   # 「… - "歌名"」取引号里的
        if quoted:
            track = quoted.group(1)
        elif re.search(r"\s+[-–—|]\s+", track):
            track = re.split(r"\s+[-–—|]\s+", track)[-1]
        return artist, track.strip(" \"'")
    artist = re.sub(r"\s*-\s*Topic$", "", d.get("uploader") or "")
    return artist, title


def lastfm(artist, track, key):
    """Last.fm track.getInfo（自动纠正拼写）：返回 (听众数, 总播放数) 或 None。"""
    info = lastfm_info(artist, track, key)
    return (info["listeners"], info["playcount"]) if info else None


_lastfm_lock = threading.Lock()
_lastfm_last = [0.0]


def lastfm_info(artist, track, key):
    """Last.fm track.getInfo：听众数、总播放数、所在专辑、风格标签。查不到返回 None。
    Last.fm 限制每秒请求数：多线程时排队，两次请求的开始至少隔 0.2 秒（每秒最多 5 次）；失败重试两次。"""
    q = urllib.parse.urlencode({"method": "track.getInfo", "api_key": key, "artist": artist, "track": track,
                                "autocorrect": 1, "format": "json"})
    handlers = []
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        proxy = os.environ["OMNIHUB_YTDLP_PROXY"]
        handlers.append(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    opener = urllib.request.build_opener(*handlers)
    data = None
    for attempt in range(3):
        with _lastfm_lock:
            wait = _lastfm_last[0] + 0.2 - time.time()
            if wait > 0:
                time.sleep(wait)
            _lastfm_last[0] = time.time()
        try:
            with opener.open("https://ws.audioscrobbler.com/2.0/?" + q, timeout=15) as r:
                data = json.load(r)
        except Exception as e:
            if attempt == 2:
                print(f"  Last.fm 请求失败：{e}", flush=True)
                return None
            time.sleep(1 + attempt)
            continue
        if data.get("error") == 29:   # 超过频率限制
            time.sleep(2 + attempt * 2)
            continue
        break
    if not data:
        return None
    t = data.get("track")
    if not t:
        return None
    try:
        listeners, playcount = int(t.get("listeners") or 0), int(t.get("playcount") or 0)
    except ValueError:
        return None
    tags = [x.get("name") for x in (t.get("toptags") or {}).get("tag") or [] if x.get("name")][:4]
    return {"listeners": listeners, "playcount": playcount, "lastfm_album": (t.get("album") or {}).get("title"),
            "tags": tags, "lastfm_name": f"{(t.get('artist') or {}).get('name', '')} - {t.get('name', '')}",
            "lastfm_artist": (t.get("artist") or {}).get("name", ""), "lastfm_track": t.get("name", "")}


def score(d, rule):
    if rule == "replay":
        if d.get("listeners") and d["listeners"] >= 50:   # 听众太少的比值不可靠
            return d["playcount"] / d["listeners"]
        return None
    if rule == "views":
        return d.get("views")
    return None


def fmt_count(n):
    if n is None:
        return "?"
    return f"{n / 1e8:.1f}亿" if n >= 1e8 else f"{n / 1e4:.1f}万" if n >= 1e4 else str(n)


def lastfm_many(pairs, key, workers=8):
    """并发查一批（艺人, 曲名）。Last.fm 单次要 2 秒左右，主要是等网络；总速率由 lastfm_info 限在每秒 5 次以内。"""
    with ThreadPoolExecutor(max_workers=workers) as pool:
        return list(pool.map(lambda p: lastfm_info(p[0], p[1], key) if p[0] and p[1] else None, pairs))


def pick(url, cookies, rule, top, done_ids, progress, log):
    """对一个链接做挑选：读一次列表页（标题、频道、播放量）→ 按规则排名取前 N 个。不逐个打开视频页。
    replay：用标题和频道名拆出艺人与曲名，并发查 Last.fm 的听众数与总播放数；views：用列表页自带的播放量。
    返回 (列表名, 选中的条目, 实际用的规则)。完整专辑整张返回、不排名。"""
    title, entries, album = extract_list(url, cookies, 0)
    log(f"== {title or url}：{'完整专辑，' if album else '列表里 '}{len(entries)} 首" if album else f"== {title or url}：列表里 {len(entries)} 个")
    fresh = [e for e in entries if e["id"] not in done_ids]
    if len(fresh) < len(entries):
        log(f"   其中 {len(entries) - len(fresh)} 个已下载过，跳过")
    if album:
        log("   完整专辑：整张下载，不排名")
        return title, fresh, "album"
    key = os.environ.get("OMNIHUB_LASTFM_KEY", "")
    if rule == "replay" and not key:
        log("   没有 Last.fm API key，改按播放量排名（在「下载 → 账号与 Cookie」里填 key）")
        rule = "views"
    if rule == "replay":
        progress(0, len(fresh), f"查 Last.fm 重播率（{len(fresh)} 首）")
        pairs = [guess_artist_track(e) for e in fresh]
        for e, (a, t), info in zip(fresh, pairs, lastfm_many(pairs, key)):
            e["lastfm_query"] = f"{a} - {t}"
            if info:
                e.update(info)
    scored, unscored = [], []
    for e in fresh:
        e["score"] = score(e, rule)
        (scored if e["score"] is not None else unscored).append(e)
    scored.sort(key=lambda e: e["score"], reverse=True)
    # 同一首歌的不同版本（音频、MV、歌词视频）在 Last.fm 上是同一曲目：只留排名最前的那个
    seen, unique = set(), []
    for e in scored:
        key_name = (e.get("lastfm_name") or "").lower()
        if key_name and key_name in seen:
            continue
        seen.add(key_name)
        unique.append(e)
    if len(unique) < len(scored):
        log(f"   {len(scored) - len(unique)} 个是同一首歌的其他版本（MV、歌词视频等），只留一个")
    scored = unique
    chosen = scored[:top] if top > 0 else scored
    names = {"replay": "重播率", "views": "播放量"}
    log(f"   按{names[rule]}排名，取前 {len(chosen)} 个：")
    for n, e in enumerate(scored[:max(len(chosen), 20)], 1):
        mark = "✓" if e in chosen else " "
        if rule == "replay":
            extra = f"重播率 {e['score']:.1f}（听众 {fmt_count(e.get('listeners'))}，播放 {fmt_count(e.get('playcount'))}）"
            album_name = f"  专辑《{e['lastfm_album']}》" if e.get("lastfm_album") else ""
            tags = f"  [{', '.join(e.get('tags') or [])}]" if e.get("tags") else ""
        else:
            extra, album_name, tags = f"播放 {fmt_count(e['score'])}", "", ""
        log(f"   {mark} {n:>3}. {extra}  {e['title'][:60]}{album_name}{tags}")
    if unscored:
        log(f"   {len(unscored)} 个没有分数（{'Last.fm 查不到或听众太少' if rule == 'replay' else '列表里没有播放量'}），不参与排名：" +
            "；".join(e["title"][:30] for e in unscored[:10]) + ("…" if len(unscored) > 10 else ""))
    return title, chosen, rule
