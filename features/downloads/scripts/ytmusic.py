"""音乐下载前，用 YouTube Music（ytmusicapi，不用登录）把要下的歌换成纯音轨版并查出所属专辑。由 general.py 调用。

YouTube 上同一首歌有几种视频：
  ATV  纯音轨（「Provided to YouTube by …」，封面是专辑图）——要的就是这种
  OMV  官方 MV、官方 Audio 上传等，常有片头片尾——不下它的音频，换成同名的 ATV
  UGC  普通用户上传（现场、翻唱、剪辑）——能找到同名 ATV 就换，找不到就按原视频下
ATV 在 YouTube Music 上都属于一张发行：Album、EP 或 Single。Album、EP 归进 <歌手>/<专辑>/，Single 算单曲。
"""
import os
import re

from ytmusicapi import YTMusic

_client = None
_albums = {}   # 专辑 browseId → get_album 结果


def client():
    global _client
    if _client is None:
        proxy = os.environ.get("OMNIHUB_YTDLP_PROXY")
        _client = YTMusic(proxies={"http": proxy, "https": proxy} if proxy else None)
    return _client


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
        _albums[browse_id] = client().get_album(browse_id)
    return _albums[browse_id]


def album_tracks(playlist_id):
    """整张专辑（OLAK5uy_ 列表）：返回 (专辑, [(视频 ID, 曲序)], browseId)，只取有视频 ID 的曲目。"""
    browse_id = client().get_album_browse_id(playlist_id)
    if not browse_id:
        raise RuntimeError("YouTube Music 上找不到这张专辑")
    a = album(browse_id)
    return a, [(t["videoId"], t.get("trackNumber") or n) for n, t in enumerate(a["tracks"], 1) if t.get("videoId")], browse_id


def place_of(a, video_id, title):
    """一首 ATV 在发行里的位置：返回 {album, album_type, number, year, album_artists}。"""
    number = next((t.get("trackNumber") or n for n, t in enumerate(a["tracks"], 1) if t.get("videoId") == video_id), None)
    if number is None:   # 地区不同时视频 ID 可能对不上，按曲名找
        number = next((t.get("trackNumber") or n for n, t in enumerate(a["tracks"], 1) if _core(t.get("title")) == _core(title)), None)
    return {"album": a.get("title") or "", "album_type": a.get("type") or "", "number": number,
            "year": a.get("year") or "", "album_artists": [x["name"] for x in a.get("artists") or []]}


def resolve(video_id):
    """一个视频 → 要下的纯音轨版。返回 (视频 ID 或 None, 信息, 说明)。
    信息：{title, artists, album, album_type, number, year, album_artists, kind}，kind 是原视频的类型（ATV/OMV/UGC）。"""
    track = client().get_watch_playlist(video_id, limit=1)["tracks"][0]
    kind = (track.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_") or "UGC"
    title = track.get("title") or ""
    artists = [x["name"] for x in track.get("artists") or []]
    target, note = video_id, ""
    if kind != "ATV":
        target = None
        query = " ".join(artists[:1] + [title])
        for s in client().search(query, filter="songs", limit=5)[:5]:
            if s.get("videoId") and _core(s.get("title")) == _core(title) and _same_artist([x["name"] for x in s.get("artists") or []], artists):
                target, track = s["videoId"], s
                note = f"{kind} 换成纯音轨版 {target}"
                break
        if not target:
            if kind == "OMV":
                return None, {}, "是 MV / 官方上传，YouTube Music 上没找到同名的纯音轨版，没下"
            return video_id, {"title": title, "artists": artists, "kind": kind}, "没找到纯音轨版，按原视频下"
    info = {"title": track.get("title") or title, "artists": [x["name"] for x in track.get("artists") or []] or artists, "kind": kind}
    browse_id = (track.get("album") or {}).get("id")
    if browse_id:
        info.update(place_of(album(browse_id), target, info["title"]))
    return target, info, note
