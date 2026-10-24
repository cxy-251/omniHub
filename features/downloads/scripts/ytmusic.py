"""音乐下载前，用 YouTube Music（ytmusicapi，不用登录）把要下的歌换成纯音轨版并查出所属专辑。由 general.py 调用。

YouTube 上同一首歌有几种视频：
  ATV  纯音轨（「Provided to YouTube by …」，封面是专辑图）——要的就是这种
  OMV  官方 MV、官方 Audio 上传等，常有片头片尾——不下它的音频，换成同名的 ATV
  UGC  普通用户上传（现场、翻唱、剪辑）——能找到同名 ATV 就换，找不到就按原视频下
ATV 在 YouTube Music 上都属于一张发行：Album、EP 或 Single。Album、EP 归进 <歌手>/<专辑>/，Single 算单曲。
"""
import os
import re
import time

from ytmusicapi import YTMusic

_client = None
_albums = {}   # 专辑 browseId → get_album 结果


def client():
    global _client
    if _client is None:
        proxy = os.environ.get("OMNIHUB_YTDLP_PROXY")
        _client = YTMusic(proxies={"http": proxy, "https": proxy} if proxy else None)
    return _client


def call(method, *args, **kwargs):
    """调 YouTube Music 接口；网络出错（超时、断线、服务器忙）重试 3 次，仍失败抛出 requests 的异常。
    其他异常（页面结构对不上，比如频道不是歌手）直接抛出。"""
    import requests
    for attempt in range(3):
        try:
            return getattr(client(), method)(*args, **kwargs)
        except requests.exceptions.RequestException:
            if attempt == 2:
                raise
            time.sleep(3 + attempt * 5)


def _norm(s):
    return "".join(ch for ch in (s or "").casefold() if ch.isalnum())


def _core(title):
    """去掉括号里的说明（Official Video、Remastered、feat. …）后的曲名。"""
    return _norm(re.sub(r"[(\[（【][^)\]）】]*[)\]）】]", "", title or "")) or _norm(title)


def _same_artist(a, b):
    a, b = {_norm(x) for x in a if _norm(x)}, {_norm(x) for x in b if _norm(x)}
    return any(x in y or y in x for x in a for y in b)


def album(browse_id):
    if browse_id not in _albums:
        _albums[browse_id] = call("get_album", browse_id)
    return _albums[browse_id]


def album_tracks(playlist_id, log):
    """整张专辑（OLAK5uy_ 列表）：返回 (专辑, [(纯音轨版视频 ID, 信息)])。"""
    browse_id = call("get_album_browse_id", playlist_id)
    if not browse_id:
        raise RuntimeError("YouTube Music 上找不到这张专辑")
    a = album(browse_id)
    return a, album_songs(a, browse_id, log)


def album_songs(a, browse_id, log, seen=None):
    """一张发行里的歌：[(纯音轨版视频 ID, 信息)]，信息里是这张发行的名字、类型、曲序。
    曲目表里有 MV 的歌给的常是 MV，换成纯音轨版（优先同一张专辑里的）；找不到就不收。
    seen 是已收过的曲名（去掉括号说明），给了就跳过已收过的、并把这次收的加进去。"""
    songs = []
    for t in a.get("tracks") or []:
        key = _core(t.get("title"))
        if not t.get("videoId") or not key or (seen is not None and key in seen):
            continue
        info = {"title": t.get("title") or "", "kind": "ATV",
                "artists": [x["name"] for x in t.get("artists") or a.get("artists") or []]}
        vid = t["videoId"]
        if (t.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_") not in ("ATV", ""):
            found = audio_version(info["title"], info["artists"], browse_id)
            if not found:
                log(f"  {info['title']}：只有 MV，没找到纯音轨版，不下")
                continue
            vid = found["videoId"]
        if seen is not None:
            seen.add(key)
        info.update(place_of(a, t["videoId"], info["title"]))
        songs.append((vid, info))
    return songs


def place_of(a, video_id, title):
    """一首 ATV 在发行里的位置：返回 {album, album_type, number, year, album_artists}。"""
    number = next((t.get("trackNumber") or n for n, t in enumerate(a["tracks"], 1) if t.get("videoId") == video_id), None)
    if number is None:   # 地区不同时视频 ID 可能对不上，按曲名找
        number = next((t.get("trackNumber") or n for n, t in enumerate(a["tracks"], 1) if _core(t.get("title")) == _core(title)), None)
    return {"album": a.get("title") or "", "album_type": a.get("type") or "", "number": number,
            "year": a.get("year") or "", "album_artists": [x["name"] for x in a.get("artists") or []]}


def audio_version(title, artists, album_id=""):
    """按曲名、歌手在 YouTube Music 搜纯音轨版：曲名（去掉括号说明）相同、歌手对得上；有几个时优先同一张专辑里的。
    返回搜索结果里的那一项，找不到返回 None。"""
    found = [s for s in call("search", " ".join(artists[:1] + [title]), filter="songs", limit=5)[:5]
             if s.get("videoId") and _core(s.get("title")) == _core(title)
             and _same_artist([x["name"] for x in s.get("artists") or []], artists)]
    return next((s for s in found if (s.get("album") or {}).get("id") == album_id), found[0] if found else None)


def resolve(video_id):
    """一个视频 → 要下的纯音轨版。返回 (视频 ID 或 None, 信息, 说明)。
    信息：{title, artists, album, album_type, number, year, album_artists, kind}，kind 是原视频的类型（ATV/OMV/UGC）。"""
    track = call("get_watch_playlist", video_id, limit=1)["tracks"][0]
    kind = (track.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_") or "UGC"
    title = track.get("title") or ""
    artists = [x["name"] for x in track.get("artists") or []]
    target, note = video_id, ""
    if kind != "ATV":
        target = None
        found = audio_version(title, artists)
        if found:
            target, track = found["videoId"], found
            note = f"{kind} 换成纯音轨版 {target}"
        if not target:
            if kind == "OMV":
                return None, {"title": title, "artists": artists, "kind": kind}, "是 MV / 官方上传，YouTube Music 上没找到同名的纯音轨版，没下"
            return video_id, {"title": title, "artists": artists, "kind": kind}, "没找到纯音轨版，按原视频下"
    info = {"title": track.get("title") or title, "artists": [x["name"] for x in track.get("artists") or []] or artists, "kind": kind}
    browse_id = (track.get("album") or {}).get("id")
    if browse_id:
        info.update(place_of(album(browse_id), target, info["title"]))
    return target, info, note


# 歌手的视频列表里混着歌词视频、官方音频、现场等，标题里有这些词的不算 MV
# （「(Musikladen, 1975)」这种括号里带年份的是电视节目现场）
NOT_MV = re.compile(r"lyric|audio|visuali[sz]er|\blive\b|remaster|teaser|trailer|behind|making|karaoke|ktv|interview|"
                    r"reaction|concert|performance|demo|alternate|unreleased|\bhits\b|\([^)]*\b(?:19|20)\d\d\)|"
                    r"歌词|歌詞|现场|現場|演唱会|演唱會|花絮|预告|預告|伴奏", re.I)


def artist(channel_id, log):
    """歌手页（YouTube Music 歌手 ID，或歌手的官方 YouTube 频道 ID）。不是歌手页时返回 None；网络出错时抛出异常。
    返回 {name, songs: [(视频 ID, 信息)], mvs: [(视频 ID, 标题)]}：
    songs 是全部专辑、EP、单曲里的歌，按发行年份从早到晚，同一首（去掉括号说明后曲名相同）只留最早那版；
    mvs 是视频列表里的正式 MV（按标题排除歌词视频、官方音频、现场等，同名的只留一个）。"""
    import requests
    try:
        page = call("get_artist", channel_id)
    except requests.exceptions.RequestException:
        raise
    except Exception:   # 普通频道（不是歌手）的页面没有歌手页的结构
        return None
    if not (page.get("albums") or page.get("singles")):
        return None
    releases = []
    for key in ("albums", "singles"):
        sec = page.get(key) or {}
        items = call("get_artist_albums", sec["browseId"], sec["params"], limit=None) if sec.get("params") else sec.get("results") or []
        releases += [r for r in items if r.get("browseId")]
    releases.sort(key=lambda r: str(r.get("year") or "9999"))   # 稳定排序：同一年里专辑在单曲前
    songs, seen, names = [], set(), []
    for n, r in enumerate(releases, 1):
        log(f"读发行 {n}/{len(releases)}：{r.get('title')}（{r.get('year') or '年份不详'}）")
        a = album(r["browseId"])
        names += [x["name"] for x in a.get("artists") or []][:1]
        got = album_songs(a, r["browseId"], log, seen)
        songs += got
        if not got:
            log("  没有新的歌（前面的发行里都有了，或只有 MV），整张跳过")
    mvs, seen_mv = [], set()
    vids = page.get("videos") or {}
    if str(vids.get("browseId") or "").startswith("VL"):
        tracks = call("get_playlist", vids["browseId"][2:], limit=None)["tracks"]
    else:
        tracks = vids.get("results") or []
    for t in tracks:
        key = _core(t.get("title"))
        if t.get("videoId") and key and key not in seen_mv and not NOT_MV.search(t.get("title") or ""):
            seen_mv.add(key)
            mvs.append((t["videoId"], t.get("title") or ""))
    log(f"视频列表 {len(tracks)} 个，其中正式 MV {len(mvs)} 个")
    name = max(set(names), key=names.count) if names else page.get("name") or ""
    return {"name": name, "songs": songs, "mvs": mvs}
