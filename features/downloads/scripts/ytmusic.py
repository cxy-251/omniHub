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

_client = None
_albums = {}   # 专辑 browseId → get_album 结果


def client():
    global _client
    if _client is None:
        from ytmusicapi import YTMusic   # 只判断标题时（YouTube 视频方案）用不到它
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


# 官方再版/音质标注：去重时视为同一首歌，且不被当作非原版过滤掉
_REMASTER_TAIL = re.compile(
    r"\s+[-–—]\s+(?:\d{4}\s+)?(?:remaster(?:ed)?|album\s+version|single\s+version|original\s+mix|mono|stereo|explicit|clean)(?:\s+\d{4})?\s*$",
    re.I,
)


def _core(title):
    """去掉括号里的说明（Official Video、Remastered、feat. …）及尾部的再版标注后的曲名。"""
    s = re.sub(r"[(\[（【][^)\]）】]*[)\]）】]", "", title or "")
    s = _REMASTER_TAIL.sub("", s)
    return _norm(s) or _norm(title)


def _split_artists(names):
    out = set()
    for name in names or ():
        for part in re.split(r"\s*,\s*|\s+&\s+|\s+[xX×]\s+|\s+(?:feat\.?|ft\.?|with)\s+", name or "", flags=re.I):
            part = re.sub(r"\s*-\s*Topic$|VEVO$", "", part).strip()
            if _norm(part):
                out.add(_norm(part))
    return out


def _same_artist(a, b):
    sa, sb = _split_artists(a), _split_artists(b)
    if sa & sb:
        return True
    # 仅对较长名字允许完整词边界匹配，避免 "IU"/"AI"/"V" 等短名误匹配翻唱艺人
    return any(len(x) >= 4 and len(y) >= 4 and (x in y or y in x) for x in sa for y in sb)


# 标题里出现即判定为非原版录音室发行（混音、现场、翻唱、变速、伴奏、串烧、白噪音等）
NON_ORIGINAL_ANYWHERE = re.compile(
    r"\bremix(?:es|ed)?\b|\bmegamix\b|\bmemegamix\b|\bmashup\b|\bbootleg\b|\brework\b|"
    r"\b(?:club|dj|extended|festival|dub|vip|dance|house|trap|drill|phonk|techno|trance|lofi|lo-fi|8d|mega|workout|party|gym|sleep|study)\s*mix\b|"
    r"\b(?:extended|club|dj|remix)\s*edit\b|"
    r"\bsped\s*up\b|\bspeed\s*up\b|\bslowed\b|\bnightcore\b|\bdaycore\b|\b8d\s*audio\b|\b8-?bit\b|\btype\s*beat\b|"
    r"\bsoundscape\b|\binstrumental\b|\bcommentary\b|\btrack\s*by\s*track\b|\bloop\s*station\s*ver\.?\b|"
    r"\bkaraoke\b|\bktv\b|\ba\s*capp?ella\b|\bacappella\b|\bmusic\s*box\b|\blullaby\s*(?:version|rendition|cover)\b|"
    r"\b(?:piano|guitar|violin|cello|orchestral|acoustic|vocal|drum|bass|sax|flute|harp|ukulele|lo-?fi|synth|chiptune|anime|metal|rock|jazz|chill|bossa)\s+(?:cover|version|rendition|tribute)\b|"
    r"\btribute\s+to\b|\bcover\s+by\b|\bcovered\s+by\b|\boriginally\s+performed\s+by\b|\bmade\s+famous\s+by\b|\bin\s+the\s+style\s+of\b|"
    r"\bunplugged\b|\bin\s+concert\b|\blive\s+(?:at|from|in|on)\b|\btour\s+live\b|\blive\s+tour\b|\bsoundcheck\b|\brehearsal\b|\blive\s*$|\bver\.\s*$|"
    r"现场|現場|演唱会|演唱會|音乐会|音樂會|不插电|不插電|翻唱|伴奏|纯音乐版|純音樂版|卡拉OK|消音|清唱|八音盒|钢琴版|鋼琴版|吉他版|"
    r"加速版|变速版|變速版|升调|降调|混音|电音版|慢摇|串烧|DJ版|라이브|콘서트|리믹스|커버|반주|가라오케|ライブ",
    re.I,
)

# 仅在括号内或分隔符（- / | :）后才算版本限定，避免误伤 "Live Forever"、"Cover Me" 等正常歌名
_QUALIFIER_WORDS = (
    r"live|concert|sessions?|performance|encore|stage|festival|"
    r"cover|tribute|instrumental|inst\.?|mr|acoustic|demo|commentary|voice\s*memo|skit|outtake|"
    r"reverb|lofi|lo-fi|piano|orchestral|symphonic|stripped|vip|dub|flip|mix|extended|"
    r"alt\.?\s*version|alternate\s*(?:version|take|mix)|early\s*(?:version|mix)|rough\s*mix|work\s*tape|"
    r"backing\s*track|minus\s*one|off\s*vocal|vocal\s*only"
)
_BRACKET_QUALIFIER = re.compile(r"[(\[（【][^)\]）】]*\b(?:" + _QUALIFIER_WORDS + r")\b[^)\]）】]*[)\]）】]", re.I)
_SEP_QUALIFIER = re.compile(r"(?:\s[-–—/|:]\s*|\s+[-–—/|:]\s*)[^-–—|]*\b(?:" + _QUALIFIER_WORDS + r")\b", re.I)

# 整张专辑即判定为非原版发行的关键词（允许 "Song (Remixes)" 中的第 1 首原版曲目通过，仅屏蔽明确非录音室专辑）
_NON_ORIGINAL_ALBUM = re.compile(
    r"\bsoundscape\b|\bcommentary\b|\btrack\s*by\s*track\b|\bunplugged\b|\bin\s+concert\b|\blive\s+(?:at|from|in|on)\b|"
    r"\bliving\s+room\s+sessions?\b|\bkaraoke\b|\btribute\b|\bsped\s*up\b|\bnightcore\b|\blofi\b|\blo-fi\b|"
    r"[(\[（【][^)\]）】]*\b(?:live|sessions?|unplugged|instrumental|karaoke|commentary|acoustic|demo)\b[^)\]）】]*[)\]）】]|"
    r"现场|現場|演唱会|演唱會|不插电|不插電|翻唱|伴奏|卡拉OK|라이브|콘서트",
    re.I,
)

# 常见非原版翻唱/伴奏/致敬发行方或虚拟艺人名称
_UNOFFICIAL_ARTIST = re.compile(
    r"\b(?:karaoke|tribute|cover\s*band|covers|lullaby|ringtone|music\s*box|sped\s*up|nightcore|8-?bit|soundalike|party\s*tyme|kidz\s*bop|ameritz)\b|"
    r"\bpiano\s*(?:tribute|crew|project|dream)\b",
    re.I,
)
_SPAM_COMPILATION = re.compile(
    r"\b(?:hits|top\s*\d+|workout|running|gym|party|sleep|study|relax|chill|gaming|tiktok|viral|chart|100\s*best|best\s*of)\b",
    re.I,
)


def is_non_original(title):
    """是否为混音、现场、翻唱、伴奏、变速、试听等非原版录音室曲目/专辑名。"""
    t = title or ""
    if not t.strip():
        return False
    return bool(NON_ORIGINAL_ANYWHERE.search(t) or _BRACKET_QUALIFIER.search(t) or _SEP_QUALIFIER.search(t))


def is_unofficial_artist(artists):
    """艺人名本身为翻唱/伴奏/致敬/加速频道等非正版发行主体。"""
    return any(is_non_original(a) or _UNOFFICIAL_ARTIST.search(a or "") for a in (artists or ()) if a)


def is_unofficial_album(a):
    """专辑本身为混音/现场/翻唱/伴奏/致敬/白噪音/解说或群星杂烩合辑。"""
    if not a:
        return False
    title = a.get("title") or ""
    if _NON_ORIGINAL_ALBUM.search(title):
        return True
    album_artists = [x.get("name") or "" for x in a.get("artists") or []]
    if is_unofficial_artist(album_artists):
        return True
    if any((x or "").casefold() == "various artists" for x in album_artists) and _SPAM_COMPILATION.search(title):
        return True
    return False


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


def album_songs(a, browse_id, log, seen=None, keep=None):
    """一张发行里的歌：[(纯音轨版视频 ID, 信息)]，信息里是这张发行的名字、类型、曲序。
    曲目表里有 MV 的歌给的常是 MV，换成纯音轨版（优先同一张专辑里的）；找不到时只收官方音频，其余不收。
    seen 是已收过的曲名（去掉括号说明），给了就跳过已收过的、并把这次收的加进去；keep(曲目, 发行) 为假的不收。"""
    if is_unofficial_album(a):
        log(f"  {a.get('title') or browse_id}：非原版录音室发行（混音/现场/翻唱/杂烩合辑），跳过")
        return []
    songs = []
    for t in a.get("tracks") or []:
        title = t.get("title") or ""
        key = _core(title)
        if not t.get("videoId") or not key or (seen is not None and key in seen) or (keep and not keep(t, a)):
            continue
        artists = [x["name"] for x in t.get("artists") or a.get("artists") or []]
        if is_non_original(title) or is_unofficial_artist(artists):
            log(f"  {title}：非原版录音室曲目（混音/现场/翻唱等），跳过")
            continue
        info = {"title": title, "kind": "ATV", "artists": artists}
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


def _valid_search_song(s, title, artists):
    if not s.get("videoId") or not s.get("isAvailable", True):
        return False
    if s.get("resultType") not in (None, "song"):
        return False
    vtype = (s.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_")
    if vtype not in ("ATV", ""):
        return False
    stitle = s.get("title") or ""
    if is_non_original(stitle) or _core(stitle) != _core(title):
        return False
    alb = s.get("album") or {}
    if is_non_original(alb.get("name") or ""):
        return False
    sartists = [x.get("name") or "" for x in s.get("artists") or []]
    if is_unofficial_artist(sartists) or not _same_artist(sartists, artists):
        return False
    return True


def audio_version(title, artists, album_id=""):
    """按曲名、歌手在 YouTube Music 搜纯音轨版：过滤混音/现场/翻唱，曲名与歌手严格对得上；优先完全同名与同一专辑。
    找不到返回 None。"""
    if is_non_original(title) or is_unofficial_artist(artists):
        return None
    found = [s for s in call("search", " ".join(artists[:1] + [title]), filter="songs", limit=8)[:8]
             if _valid_search_song(s, title, artists)]
    if not found:
        return None
    exact = [s for s in found if _norm(s.get("title")) == _norm(title)]
    pool = exact or found
    return next((s for s in pool if (s.get("album") or {}).get("id") == album_id), pool[0])


# 按 YouTube 上的原标题分（YouTube Music 给的标题去掉了「(Official Video)」这类说明，看不出来）：
# MV 是有剧情画面的音乐录影带，标题写着 Music Video、Official (HD) Video、MV、M/V
MV_TITLE = re.compile(r"music\s*video|official\s*(?:hd\s*|4k\s*)?video|(?<![a-z])m/?v(?![a-z])", re.I)
# 官方音频：录音室音轨配静态图，找不到纯音轨版时可以用它
AUDIO_TITLE = re.compile(r"official\s*audio|[(\[]audio[)\]]|音频|音訊|音源", re.I)
# 不要的：歌词视频、现场、翻唱、彩排、黑胶转盘视频、预告、花絮等
NOT_WANTED = re.compile(r"lyric|vinyl|visuali[sz]er|teaser|trailer|"
                        r"behind|making|interview|reaction|practice|choreograph|highlight|"
                        r"#shorts|\bshorts\b|relay|challenge|fancam|extra cut|sketch|"
                        r"m/?v\s+bts\b|\bep\.?\s*\d|self-?cam|\bvlog\b|현장|"
                        r"歌词|歌詞|花絮|预告|預告|티저|비하인드|메이킹|직캠|안무|연습", re.I)
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
    return bool(MV_TITLE.search(title)) and not NOT_WANTED.search(title) and not is_non_original(title)


def is_official_audio(title):
    return bool(AUDIO_TITLE.search(title)) and not NOT_WANTED.search(title) and not is_non_original(title)


def youtube_title(video_id):
    """YouTube 上的原标题。"""
    if video_id not in _titles:
        import yt_dlp
        with yt_dlp.YoutubeDL(ydl_opts()) as ydl:
            _titles[video_id] = ydl.extract_info("https://www.youtube.com/watch?v=" + video_id, download=False, process=False).get("title") or ""
    return _titles[video_id]


def _album_ok(track):
    browse_id = (track.get("album") or {}).get("id")
    if not browse_id:
        return None, True
    alb = album(browse_id)
    return alb, not is_unofficial_album(alb)


def resolve(video_id):
    """一个视频 → 要下的纯音轨版。返回 (视频 ID 或 None, 信息, 说明)。
    信息：{title, artists, album, album_type, number, year, album_artists, kind}，kind 是原视频的类型（ATV/OMV/UGC）。"""
    track = call("get_watch_playlist", video_id, limit=1)["tracks"][0]
    kind = (track.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_") or "UGC"
    title = track.get("title") or ""
    artists = [x["name"] for x in track.get("artists") or []]
    if kind not in ("ATV", "OMV", "UGC", "OFFICIAL_SOURCE_MUSIC"):
        return None, {"title": title or youtube_title(video_id), "artists": artists, "kind": kind}, f"非音乐曲目类型（{kind}），不下"
    if is_non_original(title) or is_unofficial_artist(artists):
        return None, {"title": title, "artists": artists, "kind": kind}, "非原版录音室版本（混音/现场/翻唱等），不下"
    target, note = video_id, ""
    if kind != "ATV":
        yt_title = youtube_title(video_id)
        if is_non_original(yt_title):
            return None, {"title": yt_title, "artists": artists, "kind": kind}, "原视频为混音/现场/翻唱等非原版，不下"
        target = None
        # 先看 YouTube Music 直接关联的纯音轨版（counterpart），比搜索更准确
        cp = track.get("counterpart")
        if cp and (cp.get("videoType") or "").removeprefix("MUSIC_VIDEO_TYPE_") == "ATV":
            cp_artists = [x["name"] for x in cp.get("artists") or []]
            if not is_non_original(cp.get("title") or "") and not is_unofficial_artist(cp_artists):
                _, ok = _album_ok(cp)
                if ok:
                    target, track = cp["videoId"], cp
                    note = f"{kind} 换成纯音轨版 {target}"
        if not target:
            found = audio_version(title, artists)
            if found:
                _, ok = _album_ok(found)
                if ok:
                    target, track = found["videoId"], found
                    note = f"{kind} 换成纯音轨版 {target}"
        if not target:
            if kind == "OMV" and artists and is_official_audio(yt_title):
                info = {"title": title, "artists": artists, "kind": kind}
                return video_id, info, f"没找到纯音轨版，原视频是官方音频（{yt_title}），按它下"
            # 列表里显示 YouTube 原标题，看得出是 MV、现场还是歌词视频
            info = {"title": yt_title, "artists": [], "kind": kind}
            return None, info, "没有纯音轨版，原视频也不是官方音频" if kind == "OMV" else "没有纯音轨版，原视频是用户上传"
    final_title = track.get("title") or title
    final_artists = [x["name"] for x in track.get("artists") or []] or artists
    if not final_artists or is_non_original(final_title) or is_unofficial_artist(final_artists):
        return None, {"title": final_title, "artists": final_artists, "kind": kind}, "非原版或缺少正版艺人信息，不下"
    alb, ok = _album_ok(track)
    if not ok:
        return None, {"title": final_title, "artists": final_artists, "kind": kind}, f"所属专辑为非原版发行（{alb.get('title')}），不下"
    info = {"title": final_title, "artists": final_artists, "kind": kind}
    if alb:
        info.update(place_of(alb, target, info["title"]))
    return target, info, note


def artist(channel_id, log, progress=lambda done, total, text: None):
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
    log(f"歌手 {page.get('name')}：{len(releases)} 张专辑、EP、单曲，逐张读曲目")
    albums = []
    for n, r in enumerate(releases, 1):
        if _NON_ORIGINAL_ALBUM.search(r.get("title") or ""):
            log(f"  {r.get('title')}：非原版录音室专辑（现场/伴奏/解说/白噪音等），跳过")
            continue
        progress(n - 1, len(releases), f"读 {page.get('name')} 的专辑和单曲 {n}/{len(releases)}：{r.get('title')}（{r.get('year') or '年份不详'}）")
        albums.append((r, album(r["browseId"])))
    # 这位歌手在曲目里的身份：各张发行第一位专辑歌手里出现最多的那个（歌手页名字可能是英文，曲目里是中文名）
    firsts = [x for _, a in albums for x in (a.get("artists") or [])[:1] if x.get("id")]
    owner = max(firsts, key=lambda x: sum(y["id"] == x["id"] for y in firsts)) if firsts else {"id": channel_id, "name": page.get("name") or ""}
    names = {_norm(owner["name"]), _norm(page.get("name"))}

    def keep(t, a):
        """只收这位歌手参与的（合辑、原声带里别人唱的不收），现场/混音/翻唱等非原版不收。"""
        who = t.get("artists") or a.get("artists") or []
        return any(x.get("id") == owner["id"] or _norm(x.get("name")) in names for x in who) and not is_non_original(t.get("title") or "")
    songs, seen = [], set()
    for r, a in albums:
        if is_unofficial_album(a):
            log(f"  {r.get('title')}：非原版发行，跳过")
            continue
        got = album_songs(a, r["browseId"], log, seen, keep)
        songs += got
        if not got:
            log(f"  {r.get('title')}：没有新的歌（前面的发行里都有了、不是这位歌手唱的、现场/混音版或只有 MV），整张跳过")
    for _, info in songs:   # 都放在这位歌手名下：合唱的单曲、合辑里的歌不散到别的歌手文件夹
        info["owner"] = owner["name"]
    progress(len(releases), len(releases), f"读 {page.get('name')} 的视频列表，挑出 MV")
    mvs, tracks = [], []
    vids = page.get("videos") or {}
    if str(vids.get("browseId") or "").startswith("VL"):   # 视频列表在 YouTube 上是个播放列表，从那里拿原标题
        import yt_dlp
        with yt_dlp.YoutubeDL(ydl_opts(extract_flat="in_playlist")) as ydl:
            tracks = [e for e in ydl.extract_info("https://www.youtube.com/playlist?list=" + vids["browseId"][2:], download=False)["entries"] if e]
    mvs = [(t["id"], t.get("title") or "") for t in tracks if t.get("id") and is_mv(t.get("title") or "")]
    mvs = list({v: (v, title) for v, title in mvs}.values())
    log(f"视频列表 {len(tracks)} 个，其中 MV {len(mvs)} 个（歌词视频、现场、翻唱等不要）")
    return {"name": owner["name"], "songs": songs, "mvs": mvs}


_HANGUL = re.compile(r"[가-힣]")


def _latin_name(text):
    return bool(text) and re.search(r"[A-Za-z]", text) and not _HANGUL.search(text)


def mv_artist(title):
    """从 MV 标题认艺人（公司频道里的韩团 MV 等）。按常见写法依次试：
      「한승우(HAN SEUNGWOO) …」「케이윌(K.will) - …」     韩文名(英文名) → 括号里的英文名
      「IVE 아이브 'LOVE DIVE' MV」「BTS (방탄소년단) ‘…’」 歌名引号或分隔符（ - 、_、【）之前，去掉韩文 → IVE、BTS
      「IVE, David Guetta - …」「소유(SOYOU) X Francis」  合作的取第一位（MONSTA X、&TEAM、(G)I-DLE 这种名字不拆）
      「[MV] 정세운 - Say yes (JEONG SEWOON)」              只写了韩文名：用标题里全大写的英文括号
    认不出（只有韩文、OST 合辑等）返回空。"""
    import unicodedata
    t = unicodedata.normalize("NFKC", title or "")   # 𝗧𝗛𝗘 𝟴 这种花体字当成普通字母
    t = "".join(ch for ch in t if not unicodedata.category(ch).startswith("S") or ch in "&+'’")   # 去掉 💖🎊 这类表情符号
    t = re.sub(r"^\s*(?:[\[【][^\]】]*[\]】]\s*)+", "", t).strip()   # 开头的 [MV]、【官方】
    m = re.match(r"^[\uac00-\ud7a3][\uac00-\ud7a3 ]*?\s*[(（]([^)）]*)[)）]", t)
    if m and _latin_name(m.group(1)):
        return m.group(1).strip()
    # 艺人在歌名（开引号）或分隔符之前；撇号（Can't）不算引号；_ 只在括号、韩文后面或前后有空格时算分隔（fromis_9 不拆）
    m = re.search(r"(?:^|\s)[\"'‘’‛“「『]|【|\s[-–—|]\s|\s[-–—|](?=\S)|(?<=[)\uac00-\ud7a3\s])_|_(?=\s)|[ᅵㅣ｜|]|\s[Il]\s|ー", t)   # dingo 等频道还用 ᅵ ㅣ ｜ 「 I 」「 l 」 ー 当竖线
    head = t[:m.start()] if m else t
    before = head   # 分隔符之前的整段（下面会拆合作、去括号）
    head = re.split(r"\s*,\s*|\s+&\s+|\s+[xX×]\s+(?=[^\s가-힣])|(?<=[)\uac00-\ud7a3])[xX×](?=[(\uac00-\ud7a3])|\s+(?:feat\.?|ft\.?|with)\s+",
                    head, flags=re.I)[0]
    m = re.search(r"[\uac00-\ud7a3]\s*[(（]([^)）]*)[)）]", head)
    if m and _latin_name(m.group(1)):
        return m.group(1).strip()
    if head.lstrip().startswith(("(", "（")):   # (G)I-DLE：括号是名字的一部分
        name = head
    else:
        name = re.sub(r"[(（][^)）]*[)）]", " ", head)
    name = " ".join(w for w in name.split() if not _HANGUL.search(w))
    name = re.sub(MV_TITLE.pattern + r"|\bofficial\b|\bHD\b", " ", name, flags=re.I)
    name = " ".join(name.split()).strip(" -–—|_,.")
    if name in ("X", "x", "×", "&") or re.search(r"\bOST\b", name, re.I):
        name = ""
    if not name:   # 只写了韩文名：用标题里全大写的英文括号（JEONG SEWOON）
        name = next((p.strip() for p in re.findall(r"[(（]([^)）]*)[)）]", t)
                     if _latin_name(p.strip()) and p.strip() == p.strip().upper()), "")
    if not name:   # 还是没有：用韩文名（「선우정아 - 욕심」；「한영애 X 정인」取第一位）
        first = re.split(r"\s*(?:,|&|-|–|\s[xX×]\s|(?<=[\uac00-\ud7a3])[xX×](?=[\uac00-\ud7a3]))\s*", before)[0]
        words = re.sub(r"[(（][^)）]*[)）]", " ", first).split()
        name = " ".join(words) if words and all(_HANGUL.search(w) for w in words) and len(words) <= 3 else ""
    return name
