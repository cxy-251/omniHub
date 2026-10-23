"""按质量挑选：提取列表 → 逐个补充数据 → （音乐）查 Last.fm 重播率 → 排名 → 每个链接取前 N 个。由 general.py 调用。
音乐里的完整专辑（YouTube Music 专辑）整张下载、按原曲序，不排名；只有单曲（歌单、频道里的）才看重播率。

分数：
  replay  重播率 = Last.fm 总播放数 ÷ 听众数：听过的人平均听了几遍，反映「耐不耐听」，不受推广影响（只用于音乐，需要 API key）
  ratio   点赞率 = 点赞 ÷ 播放：听过/看过的人里有多少认可
  views   播放量
没有分数的条目（数据缺失、Last.fm 查不到）不参与排名，日志里列出来。
"""
import json
import os
import re
import threading
import time
import urllib.parse
import urllib.request

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
    if info.get("_type") not in ("playlist", "multi_video"):
        return info.get("title") or "", [{"id": str(info.get("id") or ""), "url": info.get("webpage_url") or url, "title": info.get("title") or ""}], False
    out = []
    for e in info.get("entries") or []:
        if not e:
            continue
        u = e.get("url") or e.get("webpage_url") or ""
        if u and not u.startswith("http") and e.get("ie_key") == "Youtube":
            u = "https://www.youtube.com/watch?v=" + u
        if u:
            out.append({"id": str(e.get("id") or ""), "url": u, "title": e.get("title") or ""})
    title = re.sub(r"^Album - ", "", info.get("title") or info.get("channel") or "")
    return title, out, is_album(url, info)


def details(url, cookies):
    """读取单个视频的完整信息（不下载）：播放量、点赞、艺人、曲名、专辑、日期。
    只要页面信息，不解析播放地址（跳过播放器脚本与格式列表）：每个从十几秒降到几秒。
    music.youtube.com 的链接换成 www.youtube.com（同一个视频，前者常读超时）。"""
    url = url.replace("://music.youtube.com/", "://www.youtube.com/")
    extra = {"ignore_no_formats_error": True, "socket_timeout": 15,
             "extractor_args": {"youtube": {"player_skip": ["js", "configs"], "skip": ["dash", "hls"]}}}
    with YoutubeDL(ydl_opts(cookies, extra)) as ydl:
        info = ydl.extract_info(url, download=False) or {}
    return {
        "views": info.get("view_count"), "likes": info.get("like_count"), "comments": info.get("comment_count"),
        "artist": info.get("artist") or (info.get("artists") or [None])[0] or info.get("creator"),
        "track": info.get("track"), "title": info.get("title") or "", "uploader": info.get("channel") or info.get("uploader") or "",
        "album": info.get("album"), "year": info.get("release_year") or (str(info.get("upload_date") or "")[:4] or None),
        "date": info.get("upload_date"), "duration": info.get("duration"),
    }


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
    Last.fm 限制每秒请求数：多线程时排队，两次请求至少隔 0.3 秒；失败重试两次。"""
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
            wait = _lastfm_last[0] + 0.3 - time.time()
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
            "tags": tags, "lastfm_name": f"{(t.get('artist') or {}).get('name', '')} - {t.get('name', '')}"}


def score(d, rule):
    if rule == "replay":
        if d.get("listeners") and d["listeners"] >= 50:   # 听众太少的比值不可靠
            return d["playcount"] / d["listeners"]
        return None
    if rule == "ratio":
        return d["likes"] / d["views"] if d.get("views") and d.get("likes") is not None else None
    if rule == "views":
        return d.get("views")
    return None


def fmt_count(n):
    if n is None:
        return "?"
    return f"{n / 1e8:.1f}亿" if n >= 1e8 else f"{n / 1e4:.1f}万" if n >= 1e4 else str(n)


def pick(url, cookies, rule, top, pool, done_ids, progress, log, delay=1.0):
    """对一个链接做完整的挑选流程，返回 (列表名, 选中的条目)，条目里带分数，供写进排名表。"""
    title, entries, album = extract_list(url, cookies, pool)
    log(f"== {title or url}：{'完整专辑，' if album else '列表里 '}{len(entries)} 首" if album else f"== {title or url}：列表里 {len(entries)} 个")
    fresh = [e for e in entries if e["id"] not in done_ids]
    if len(fresh) < len(entries):
        log(f"   其中 {len(entries) - len(fresh)} 个已下载过，跳过")
    if album and rule == "replay":
        # 完整专辑整张下（按原曲序），不按单曲重播率挑
        log("   完整专辑：整张下载，不排名")
        return title, fresh, "album"
    key = os.environ.get("OMNIHUB_LASTFM_KEY", "")
    if rule == "replay" and not key:
        log("   没有 Last.fm API key，改按点赞率排名（在「下载 → 账号与 Cookie」里填 key）")
        rule = "ratio"
    scored, unscored = [], []
    for i, e in enumerate(fresh):
        progress(i, len(fresh), f"补充数据 {i + 1}/{len(fresh)}：{e['title'][:40]}")
        d = details(e["url"], cookies)
        e.update(d)
        if rule == "replay":
            artist, track = guess_artist_track(d)
            e["lastfm_query"] = f"{artist} - {track}"
            r = lastfm(artist, track, key) if artist and track else None
            if r:
                e["listeners"], e["playcount"] = r
        s = score(e, rule)
        e["score"] = s
        (scored if s is not None else unscored).append(e)
        time.sleep(delay)   # 放慢一点，少触发网站限流
    scored.sort(key=lambda e: e["score"], reverse=True)
    chosen = scored[:top] if top > 0 else scored
    names = {"replay": "重播率", "ratio": "点赞率", "views": "播放量"}
    log(f"   按{names[rule]}排名，取前 {len(chosen)} 个：")
    for n, e in enumerate(scored[:max(len(chosen), 20)], 1):
        mark = "✓" if e in chosen else " "
        extra = f"（听众 {fmt_count(e.get('listeners'))}，播放 {fmt_count(e.get('playcount'))}）" if rule == "replay" else ""
        val = f"{e['score']:.2f}" if rule == "replay" else f"{e['score'] * 100:.2f}%" if rule == "ratio" else fmt_count(e["score"])
        log(f"   {mark} {n:>3}. {names[rule]} {val}{extra}  点赞 {fmt_count(e.get('likes'))} / 播放 {fmt_count(e.get('views'))}  {e['title'][:60]}")
    if unscored:
        log(f"   {len(unscored)} 个没有分数（数据缺失{'或 Last.fm 查不到' if rule == 'replay' else ''}），不参与排名：" +
            "；".join(e["title"][:30] for e in unscored[:10]) + ("…" if len(unscored) > 10 else ""))
    return title, chosen, rule
