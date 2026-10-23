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
    """读取单个视频的完整信息（不下载）：播放量、点赞、艺人、曲名。"""
    with YoutubeDL(ydl_opts(cookies, {})) as ydl:
        info = ydl.extract_info(url, download=False) or {}
    return {
        "views": info.get("view_count"), "likes": info.get("like_count"), "comments": info.get("comment_count"),
        "artist": info.get("artist") or (info.get("artists") or [None])[0] or info.get("creator"),
        "track": info.get("track"), "title": info.get("title") or "", "uploader": info.get("channel") or info.get("uploader") or "",
    }


def guess_artist_track(d):
    """艺人与曲名：YouTube Music 的视频直接有；否则从「艺人 - 曲名」样式的标题里拆，再不行用频道名去掉 - Topic。"""
    if d.get("artist") and d.get("track"):
        return d["artist"], d["track"]
    title = re.sub(r"[\(\[（【][^\)\]）】]*(official|video|audio|lyric|mv|hd|4k|官方|歌词)[^\)\]）】]*[\)\]）】]", "", d["title"], flags=re.I).strip()
    m = re.match(r"^(.+?)\s+[-–—|]\s+(.+)$", title)
    if m:
        return m.group(1).strip(), m.group(2).strip()
    artist = re.sub(r"\s*-\s*Topic$", "", d.get("uploader") or "")
    return artist, title


def lastfm(artist, track, key):
    """Last.fm track.getInfo（自动纠正拼写）：返回 (听众数, 总播放数) 或 None。"""
    q = urllib.parse.urlencode({"method": "track.getInfo", "api_key": key, "artist": artist, "track": track,
                                "autocorrect": 1, "format": "json"})
    handlers = []
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        proxy = os.environ["OMNIHUB_YTDLP_PROXY"]
        handlers.append(urllib.request.ProxyHandler({"http": proxy, "https": proxy}))
    opener = urllib.request.build_opener(*handlers)
    try:
        with opener.open("https://ws.audioscrobbler.com/2.0/?" + q, timeout=15) as r:
            data = json.load(r)
    except Exception as e:
        print(f"  Last.fm 请求失败：{e}", flush=True)
        return None
    t = data.get("track")
    if not t:
        return None
    try:
        return int(t.get("listeners") or 0), int(t.get("playcount") or 0)
    except ValueError:
        return None


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
