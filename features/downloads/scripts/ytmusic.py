"""音乐下载前，用 YouTube Music（ytmusicapi，不用登录）把要下的歌换成纯音轨版并查出所属专辑。由 general.py 调用。

YouTube 上同一首歌有几种视频（YouTube Music 的分类）：
  ATV  纯音轨（「Provided to YouTube by …」，封面是专辑图）——要的就是这种
  OMV  官方上传：MV、官方音频、歌词视频、官方现场都算——先换成同名的 ATV；找不到时，
       只有标题写着 Official Audio 的（录音室音轨配静态图）按原视频下，MV、歌词视频、现场都不下
  UGC  普通用户上传（现场、翻唱、剪辑）——能找到同名 ATV 就换，找不到就不下
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
    曲目表里有 MV 的歌给的常是 MV，换成纯音轨版（优先同一张专辑里的）；找不到时只收官方音频，其余不收。
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
            if found:
                vid = found["videoId"]
            elif is_official_audio(youtube_title(vid)):
                info["kind"] = "OMV"
                log(f"  {info['title']}：没找到纯音轨版，按官方音频（{youtube_title(vid)}）下")
            else:
                log(f"  {info['title']}：没找到纯音轨版（曲目表里是 {youtube_title(vid)}），不下")
                continue
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


# 按 YouTube 上的原标题分（YouTube Music 给的标题去掉了「(Official Video)」这类说明，看不出来）：
# MV 是有剧情画面的音乐录影带，标题写着 Music Video、Official (HD) Video、MV、M/V
MV_TITLE = re.compile(r"music\s*video|official\s*(?:hd\s*|4k\s*)?video|(?<![a-z])m/?v(?![a-z])", re.I)
# 官方音频：录音室音轨配静态图，找不到纯音轨版时可以用它
AUDIO_TITLE = re.compile(r"official\s*audio|[(\[]audio[)\]]|音频|音訊|音源", re.I)
# 不要的：歌词视频、现场、翻唱、彩排、黑胶转盘视频、预告、花絮等
NOT_WANTED = re.compile(r"lyric|\blive\b|concert|performance|rehearsal|cover|vinyl|visuali[sz]er|teaser|trailer|"
                        r"behind|making|karaoke|ktv|interview|reaction|demo|歌词|歌詞|现场|現場|演唱会|演唱會|花絮|预告|預告|伴奏|翻唱", re.I)
_titles = {}


def ydl_opts(**extra):
    opts = {"quiet": True, "no_warnings": True, "skip_download": True, **extra}
    cookies = os.path.join(os.environ.get("OMNIHUB_COOKIES_DIR", ""), "youtube.txt")
    if os.path.isfile(cookies):
        opts["cookiefile"] = cookies
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        opts["proxy"] = os.environ["OMNIHUB_YTDLP_PROXY"]
    return opts


def is_mv(title):
    return bool(MV_TITLE.search(title)) and not NOT_WANTED.search(title)


def is_official_audio(title):
    return bool(AUDIO_TITLE.search(title)) and not NOT_WANTED.search(title)


def youtube_title(video_id):
    """YouTube 上的原标题。"""
    if video_id not in _titles:
        import yt_dlp
        with yt_dlp.YoutubeDL(ydl_opts()) as ydl:
            _titles[video_id] = ydl.extract_info("https://www.youtube.com/watch?v=" + video_id, download=False, process=False).get("title") or ""
    return _titles[video_id]


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
            info = {"title": title, "artists": artists, "kind": kind}
            if kind == "OMV" and is_official_audio(youtube_title(video_id)):
                return video_id, info, f"没找到纯音轨版，原视频是官方音频（{youtube_title(video_id)}），按它下"
            return None, info, f"没找到纯音轨版，原视频不是官方音频（{youtube_title(video_id)}），没下"
    info = {"title": track.get("title") or title, "artists": [x["name"] for x in track.get("artists") or []] or artists, "kind": kind}
    browse_id = (track.get("album") or {}).get("id")
    if browse_id:
        info.update(place_of(album(browse_id), target, info["title"]))
    return target, info, note


def artist(channel_id, log):
    """歌手页（YouTube Music 歌手 ID，或歌手的官方 YouTube 频道 ID）。不是歌手页时返回 None；网络出错时抛出异常。
    返回 {name, songs: [(视频 ID, 信息)], mvs: [(视频 ID, 标题)]}：
    songs 是全部专辑、EP、单曲里的歌，按发行年份从早到晚，同一首（去掉括号说明后曲名相同）只留最早那版；
    mvs 是视频列表里的 MV：YouTube 原标题写着 Music Video / Official Video / MV，且不是歌词视频、现场、翻唱等（is_mv）。"""
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
    mvs, tracks = [], []
    vids = page.get("videos") or {}
    if str(vids.get("browseId") or "").startswith("VL"):   # 视频列表在 YouTube 上是个播放列表，从那里拿原标题
        import yt_dlp
        with yt_dlp.YoutubeDL(ydl_opts(extract_flat="in_playlist")) as ydl:
            tracks = [e for e in ydl.extract_info("https://www.youtube.com/playlist?list=" + vids["browseId"][2:], download=False)["entries"] if e]
    mvs = [(t["id"], t.get("title") or "") for t in tracks if t.get("id") and is_mv(t.get("title") or "")]
    mvs = list({v: (v, title) for v, title in mvs}.values())
    log(f"视频列表 {len(tracks)} 个，其中 MV {len(mvs)} 个（歌词视频、现场、翻唱等不要）")
    name = max(set(names), key=names.count) if names else page.get("name") or ""
    return {"name": name, "songs": songs, "mvs": mvs}
