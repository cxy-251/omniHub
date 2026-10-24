#!/usr/bin/env python3
"""按方案下载到暂存区的一个批次（不直接进资源库）。由 omniHub 服务按需调用。

方案（--scheme）决定工具和去向：
  music     YouTube 的先换成 YouTube Music 上的纯音轨版（MV 有片头片尾，不下它的音频；见 ytmusic.py），
            yt-dlp 只取最好的音轨（不转码，嵌封面、歌词与标签）→ media_library/audio/music/<歌手>/<曲名>（单曲）
            或 …/<歌手>/<专辑>/<曲序 曲名>（属于 Album、EP 的歌，单独下的也归进专辑）
  youtube   yt-dlp 视频 → media_library/shortvideo/YouTube/<频道>/<日期>_<标题>_<id>
  bilibili  yt-dlp 视频 → media_library/shortvideo/B站/<UP主>/…
  x         gallery-dl → 图片 gallery/X/<用户>/（移入资源库时并进相册），视频 media_library/shortvideo/X/<用户>/
  other     按链接域名认平台（平台表见 OMNIHUB_PLATFORMS），视频 → media_library/shortvideo/<平台>/<作者>/
每个文件旁边保存元数据（.info.json / .json），移入资源库时收进 .omni_data/download-meta，留给以后打分。
音乐例外：封面、歌词、各项数据都写进音频文件，元数据合成批次里的一个 download-meta-<时间>.json。
--archive 是这个资源库的「已下载」名单：下过的（包括在暂存区删掉的）不再下。
Cookie 从 OMNIHUB_COOKIES_DIR/<平台>.txt 读，代理从 OMNIHUB_YTDLP_PROXY 读。
进度与结果以 OMNIHUB_PROGRESS <完成> <总数> <说明>、OMNIHUB_RESULT <json> 行输出。
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
from urllib.parse import urlparse

# 平台表由服务端给出（与「账号与 Cookie」页同一份）：[{"id","hosts":[…],"dir","tool"}]
PLATFORMS = json.loads(os.environ.get("OMNIHUB_PLATFORMS", "[]"))
SCHEME_PLATFORM = {"music": "youtube", "youtube": "youtube", "bilibili": "bilibili", "x": "x"}
VIDEO_EXT = (".mp4", ".mov", ".webm", ".mkv", ".m4v")


def platform_of(url):
    host = (urlparse(url).hostname or "").lower()
    for p in PLATFORMS:
        if any(host == h or host.endswith("." + h) for h in p["hosts"]):
            return p
    return {"id": "", "dir": host.removeprefix("www.") or "其他", "tool": "yt-dlp"}


def progress(done, total, text):
    print(f"OMNIHUB_PROGRESS {done} {total} {text}", flush=True)


def log(msg):
    print(msg, flush=True)


def cookie_file(pid):
    path = os.path.join(os.environ.get("OMNIHUB_COOKIES_DIR", ""), pid + ".txt") if pid else ""
    return path if path and os.path.isfile(path) else ""


def safe(name):
    name = re.sub(r'[\\/:*?"<>|\r\n\t]', "_", name).strip(" .")
    return name[:100] or "未分类"


def normalize(url):
    """YouTube 频道首页是「视频 / Shorts / 直播」几个分页；没指定分页时取视频页（与 probe.py 相同）。"""
    if re.search(r"youtube\.com/(@[^/?#]+|channel/[^/?#]+|c/[^/?#]+|user/[^/?#]+)/?(\?.*)?$", url):
        return re.sub(r"/?(\?.*)?$", r"/videos\1", url, count=1)
    return url


def ytdlp(urls, a, platform):
    """一个 yt-dlp 进程按顺序下全部链接（单个视频或整个列表）；返回 (下完的文件, 已在名单里跳过的数量, 出错的数量)。"""
    urls = [normalize(u) for u in urls]
    # 进度总数：每个链接取前 N 个时最多 链接数×N；否则按链接数（列表展开后会超过，随下随长）
    expected = len(urls) * a.per_list if a.per_list > 0 else len(urls)
    cmd = [sys.executable, "-m", "yt_dlp", "--newline", "--no-colors", "--ignore-errors", "--windows-filenames",
           "--write-info-json", "--no-write-playlist-metafiles", "--no-simulate",
           "--no-quiet",  # --print 默认会静音；跳过、进度信息要留在日志里
           "--print", "after_move:OMNIHUB_FILE %(filepath)s"]
    if a.archive:
        cmd += ["--download-archive", a.archive]
    cookies = cookie_file(platform["id"])
    if cookies:
        cmd += ["--cookies", cookies]
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        cmd += ["--proxy", os.environ["OMNIHUB_YTDLP_PROXY"]]
    if a.scheme == "music" and platform["id"] == "youtube":
        urls, skipped0, errors0 = music_targets(urls, a, cookies)   # 已换成纯音轨版的单曲链接，已按每个列表取前 N 个
        expected = len(urls)
    elif a.per_list > 0:
        cmd += ["--playlist-items", f"1:{a.per_list}"]   # 每个列表/频道只取前 N 个（频道、播放列表按新→旧排列）
    elif a.items:
        cmd += ["--playlist-items", a.items]
    if a.scheme == "music":
        # 先下到批次里的 .incoming/<视频 ID>，写完标签后由 organize_music 按「歌手 / 单曲、歌手 / 专辑 / 歌」归档
        # 纯音轨版的缩略图是专辑封面两边加色块（16:9），裁成中间的正方形再嵌进去
        cmd += ["-f", "bestaudio/best", "-x", "--audio-format", "best", "--embed-metadata", "--embed-thumbnail",
                "--convert-thumbnails", "jpg", "--ppa", "ThumbnailsConvertor:-q:v 2 -vf crop=\"'min(iw,ih)':'min(iw,ih)'\"",
                "-o", os.path.join(a.batch, ".incoming", "%(id)s.%(ext)s")]
    else:
        h = a.max_height
        fmt = "bv*+ba/b" if h <= 0 else f"bv*[height<={h}]+ba/b[height<={h}]/b"
        out = os.path.join(a.batch, "media_library", "shortvideo", platform["dir"],
                           "%(channel,uploader,uploader_id|未知作者)s", "%(upload_date>%Y-%m-%d)s_%(title).80B_%(id)s.%(ext)s")
        cmd += ["-f", fmt, "--merge-output-format", "mp4", "-o", out]
    files, skipped, errors = [], 0, 0
    if a.scheme == "music" and platform["id"] == "youtube":
        skipped, errors = skipped0, errors0
        if not urls:
            return files, skipped, errors
    proc = subprocess.Popen(cmd + urls, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
    for line in proc.stdout:
        line = line.rstrip("\n")
        if line.startswith("OMNIHUB_FILE "):
            files.append(line[13:])
            progress(len(files) + skipped, max(expected, len(files) + skipped), os.path.basename(files[-1]))
            continue
        if "has already been recorded in the archive" in line:
            skipped += 1
            progress(len(files) + skipped, max(expected, len(files) + skipped), "已下载过，跳过")
        elif line.startswith("ERROR:"):
            errors += 1
            for key, hint in LOGIN_HINTS.items():
                if key in line:
                    hints.add(hint.format(platform["dir"]))
        log(line)
    proc.wait()
    if a.album:   # 指定了专辑（按质量挑时的整张专辑）：按下载顺序就是曲序
        for n, f in enumerate(files, 1):
            a.album_of[f] = (a.album, n)
    return files, skipped, errors


def archived(a):
    """已下载名单里的视频 ID（yt-dlp 的格式是「extractor ID」一行一个）。"""
    if not a.archive or not os.path.isfile(a.archive):
        return set()
    with open(a.archive, encoding="utf-8", errors="replace") as f:
        return {line.split(" ", 1)[1].strip() for line in f if " " in line}


def pick_items(entries, a):
    """按 --per-list / --items 取列表里的一部分（序号从 1 起）。"""
    if a.per_list > 0:
        return entries[:a.per_list]
    if not a.items:
        return entries
    from yt_dlp.utils import PlaylistEntries
    out = []
    for part in PlaylistEntries.parse_playlist_items(a.items):
        if isinstance(part, slice):
            out += entries[part.start - 1:int(part.stop) if part.stop is not None else None]
        elif 0 < part <= len(entries):
            out.append(entries[part - 1])
    return out


def music_targets(urls, a, cookies):
    """YouTube 音乐：展开链接（列表、频道、专辑）→ 每首换成纯音轨版、查出所属专辑与曲序（见 ytmusic.py），
    信息记进 a.music_info[视频 ID]。MV 找不到纯音轨版的不下。返回 (要下的单曲链接, 已下载过跳过的数量, 出错的数量)。"""
    import yt_dlp
    import ytmusic
    done = archived(a)
    ids, errors = [], 0
    for i, url in enumerate(urls):
        progress(i, len(urls), "展开链接 " + url)
        m = re.search(r"[?&]list=(OLAK5uy_[\w-]+)", url)
        try:
            if m:   # 整张专辑：直接取 YouTube Music 上的曲目（YouTube 网页版的专辑列表里常混着 MV）
                album, tracks, _ = ytmusic.album_tracks(m.group(1))
                log(f"专辑《{album.get('title')}》{len(tracks)} 首")
                ids += pick_items([vid for vid, _ in tracks], a)
                continue
            opts = {"extract_flat": "in_playlist", "quiet": True, "no_warnings": True, "skip_download": True}
            if cookies:
                opts["cookiefile"] = cookies
            if os.environ.get("OMNIHUB_YTDLP_PROXY"):
                opts["proxy"] = os.environ["OMNIHUB_YTDLP_PROXY"]
            with yt_dlp.YoutubeDL(opts) as ydl:
                info = ydl.extract_info(url, download=False)
            entries = info.get("entries")
            ids += pick_items([e["id"] for e in entries if e and e.get("id")], a) if entries is not None else [info["id"]]
        except Exception as e:
            log(f"ERROR: 展开失败 {url}：{e}")
            errors += 1
    targets, skipped = [], 0
    ids = list(dict.fromkeys(ids))
    for n, vid in enumerate(ids):
        if vid in done:
            skipped += 1
            continue
        progress(n, len(ids), f"找纯音轨版与专辑 {vid}")
        try:
            target, info, note = ytmusic.resolve(vid)
        except Exception as e:
            log(f"ERROR: YouTube Music 查询失败 {vid}：{e}")
            errors += 1
            continue
        if note:
            log(f"{vid}（{' / '.join(info.get('artists') or [])} - {info.get('title', '')}）：{note}")
        if not target:
            continue
        if target in done or target in a.music_info:
            skipped += 1
            continue
        a.music_info[target] = info
        targets.append("https://www.youtube.com/watch?v=" + target)
    log(f"要下 {len(targets)} 首（{len(ids)} 个视频里，已下载过 {skipped} 个，出错 {errors} 个）")
    return targets, skipped, errors


# 网站要求登录时给出的中文提示（日志最后一行）
LOGIN_HINTS = {
    "Sign in to confirm": "{} 要求登录才能下载：先在 Chrome 里登录，再到「下载 → 账号与 Cookie」点「从 Chrome 读取」。",
    "cookies": "{} 需要登录 Cookie：到「下载 → 账号与 Cookie」读取。",
    "login required": "{} 需要登录：到「下载 → 账号与 Cookie」读取。",
}
hints = set()


def gallerydl(urls, a):
    """X：每个用户一个 gallery-dl 进程；图片留在 gallery/X/<用户>/，视频移到 shortvideo/X/<用户>/。"""
    files, skipped, errors = [], 0, 0
    for i, url in enumerate(urls):
        url = url.strip()
        user = (urlparse(url).path.strip("/").split("/") or [""])[0] if "://" in url else url.lstrip("@")
        if "://" not in url:
            url = f"https://x.com/{user}/media"
        user = safe(user or "未知用户")
        target = os.path.join(a.batch, "gallery", "X", user)
        cmd = [sys.executable, "-m", "gallery_dl", "--write-metadata", "-D", target]
        if a.archive:
            cmd += ["--download-archive", a.archive + ".gallery-dl.sqlite3"]
        cookies = cookie_file("x")
        if cookies:
            cmd += ["--cookies", cookies]
        if os.environ.get("OMNIHUB_YTDLP_PROXY"):
            cmd += ["--proxy", os.environ["OMNIHUB_YTDLP_PROXY"]]
        if a.x_limit > 0:
            cmd += ["--range", f"1-{a.x_limit}"]
        if a.x_media == "image":
            cmd += ["--filter", "extension not in ('mp4', 'mov', 'webm', 'm3u8')"]
        elif a.x_media == "video":
            cmd += ["--filter", "extension in ('mp4', 'mov', 'webm', 'm3u8')"]
        progress(i, len(urls), f"X · @{user}")
        proc = subprocess.Popen(cmd + [url], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
        for line in proc.stdout:
            line = line.rstrip("\n")
            if line.startswith("# "):
                skipped += 1
            elif line and os.path.isfile(line):
                files.append(line)
            elif "[error]" in line:
                errors += 1
            log(line)
        proc.wait()
        # 视频放到短视频里（相册只收图片）
        videos = os.path.join(a.batch, "media_library", "shortvideo", "X", user)
        for name in os.listdir(target) if os.path.isdir(target) else []:
            if name.lower().endswith(VIDEO_EXT):
                os.makedirs(videos, exist_ok=True)
                shutil.move(os.path.join(target, name), os.path.join(videos, name))
                meta = os.path.join(target, name + ".json")
                if os.path.isfile(meta):
                    shutil.move(meta, os.path.join(videos, name + ".json"))
    return files, skipped, errors


def source_fields(meta):
    """来源网站的数据（.info.json 里有的才写）：播放数、点赞数、发布日期、下载日期、原链接。"""
    fields = {"DOWNLOAD_DATE": time.strftime("%Y-%m-%d")}
    for key, name in (("view_count", "SOURCE_VIEWS"), ("like_count", "SOURCE_LIKES")):
        if isinstance(meta.get(key), int):
            fields[name] = str(meta[key])
    day = meta.get("release_date") or meta.get("upload_date") or ""
    if re.fullmatch(r"\d{8}", day):
        fields["RELEASE_DATE"] = f"{day[:4]}-{day[4:6]}-{day[6:]}"
    elif meta.get("release_year"):
        fields["RELEASE_DATE"] = str(meta["release_year"])
    if meta.get("webpage_url"):
        fields["SOURCE_URL"] = meta["webpage_url"]
    return fields


def lastfm_fields(info):
    """Last.fm：听众、总播放、重播率（总播放 ÷ 听众，听众太少时不写）、Last.fm 上的专辑。"""
    fields = {"LASTFM_LISTENERS": str(info["listeners"]), "LASTFM_PLAYCOUNT": str(info["playcount"])}
    if info.get("listeners", 0) >= 50:
        fields["LASTFM_REPLAY"] = f"{info['playcount'] / info['listeners']:.2f}"
    if info.get("lastfm_album"):
        fields["LASTFM_ALBUM"] = info["lastfm_album"]
    return fields


def music_place(meta, info, guess, album_hint, ytm=None):
    """一首歌的归档位置与标准标签：返回 (歌手, 专辑或空, 曲序或 None, 曲名)。
    歌手：YouTube Music / yt-dlp 给的 → Last.fm 纠正过的 → 从标题拆的 → 频道名（去掉 - Topic、VEVO）；
    yt-dlp 与 Last.fm 只差大小写时用 Last.fm 的写法。
    专辑：按 YouTube Music 上这首所属的发行（ytm，见 ytmusic.py）——Album、EP 进专辑文件夹，Single 算单曲；
    没查到时，整张专辑下载（列表 ID 以 OLAK5uy_ 开头）的进专辑文件夹，其余算单曲。"""
    info, ytm = info or {}, ytm or {}
    yt = (ytm.get("artists") or meta.get("artists") or [None])[0] or meta.get("artist")
    lf = info.get("lastfm_artist")
    channel = re.sub(r"\s*-\s*Topic$|VEVO$", "", meta.get("channel") or meta.get("uploader") or "").strip()
    artist = lf if yt and lf and yt.casefold() == lf.casefold() else (yt or lf or guess[0] or channel or "未知歌手")
    # 合作曲（「A, B」「A & B」「A feat. B」）归到第一位歌手名下；Last.fm 认这个整体名字（如 Simon & Garfunkel）时不拆
    if not (lf and lf.casefold() == artist.casefold()):
        artist = re.split(r",\s*|\s+&\s+|\s+(?:feat\.?|ft\.?|with|x)\s+", artist, flags=re.I)[0].strip() or artist
    track = ytm.get("title") or meta.get("track") or info.get("lastfm_track") or guess[1] or meta.get("title") or meta.get("id") or "未知曲目"
    album, number = "", None
    if album_hint:
        album, number = album_hint
    elif ytm.get("album"):
        if ytm.get("album_type") != "Single":
            album, number = ytm["album"], ytm.get("number")
            artist = (ytm.get("album_artists") or [artist])[0]
        return artist, album, number, track
    elif str(meta.get("playlist_id") or "").startswith("OLAK5uy_"):
        album = meta.get("album") or re.sub(r"^Album - ", "", meta.get("playlist_title") or "")
        number = meta.get("playlist_index")
    if album:
        artist = (meta.get("album_artists") or [None])[0] or artist   # 专辑放在专辑歌手名下（合作曲不拆散专辑）
    return artist, album, number, track


def move_music(batch, f, artist, album, number, track, video_id):
    """移到 media_library/audio/music/<歌手>/[<专辑>/][曲序 ]<曲名>.<扩展名>；重名时名字后加视频 ID。返回新路径。"""
    folder = os.path.join(batch, "media_library", "audio", "music", safe(artist), *([safe(album)] if album else []))
    os.makedirs(folder, exist_ok=True)
    ext = os.path.splitext(f)[1]
    name = (f"{int(number):02d} " if number else "") + safe(track)
    dest = os.path.join(folder, name + ext)
    if os.path.exists(dest):
        dest = os.path.join(folder, f"{name} [{video_id}]{ext}")
    shutil.move(f, dest)
    return dest


def write_tags(path, fields, genre="", lyrics="", std=None):
    """把自定义字段写进音频文件：风格写标准 GENRE 字段（多个用 ; 分隔），歌词写标准歌词字段
    （Vorbis LYRICS、MP4 ©lyr、ID3 USLT；带时间轴的就是 LRC 文本），std 是标准的歌手、曲名、专辑、曲序、年份，其余按格式写
    MP4 自由格式（----:com.apple.iTunes:键）、ID3 TXXX、Vorbis 注释（Opus、FLAC）。"""
    import mutagen
    from mutagen.id3 import ID3, TALB, TCON, TDRC, TIT2, TPE1, TRCK, TXXX, USLT
    from mutagen.mp4 import MP4, MP4FreeForm
    f = mutagen.File(path)
    if f is None:
        return False
    std = {k: v for k, v in (std or {}).items() if v}
    # 风格只用 Last.fm 的标签；yt-dlp 写进去的是 YouTube 分类（Music、Entertainment），不是风格，没有就清掉
    if isinstance(f, MP4):
        if genre:
            f["\xa9gen"] = [genre]
        else:
            f.pop("\xa9gen", None)
        if lyrics:
            f["\xa9lyr"] = [lyrics]
        for k, atom in (("artist", "\xa9ART"), ("title", "\xa9nam"), ("album", "\xa9alb"), ("date", "\xa9day")):
            if k in std:
                f[atom] = [std[k]]
        if "number" in std:
            f["trkn"] = [(int(std["number"]), 0)]
        for k, v in fields.items():
            f["----:com.apple.iTunes:" + k] = [MP4FreeForm(v.encode())]
    elif isinstance(f.tags, ID3):
        if genre:
            f.tags.add(TCON(encoding=3, text=[genre]))
        else:
            f.tags.delall("TCON")
        if lyrics:
            f.tags.delall("USLT")
            f.tags.add(USLT(encoding=3, lang="XXX", desc="", text=lyrics))
        for k, frame in (("artist", TPE1), ("title", TIT2), ("album", TALB), ("number", TRCK), ("date", TDRC)):
            if k in std:
                f.tags.add(frame(encoding=3, text=[str(std[k])]))
        for k, v in fields.items():
            f.tags.add(TXXX(encoding=3, desc=k, text=[v]))
    else:   # Opus、Vorbis、FLAC：Vorbis 注释
        if f.tags is None:
            f.add_tags()
        if genre:
            f.tags["GENRE"] = genre
        elif "GENRE" in f.tags:
            del f.tags["GENRE"]
        if lyrics:
            f.tags["LYRICS"] = lyrics
        for k, key in (("artist", "ARTIST"), ("title", "TITLE"), ("album", "ALBUM"), ("number", "TRACKNUMBER"), ("date", "DATE")):
            if k in std:
                f.tags[key] = str(std[k])
        for k, v in fields.items():
            f.tags[k] = v
    f.save()
    return True


# .info.json 里体积大、以后用不上的部分（格式列表、缩略图列表、字幕链接等）
META_DROP = ("formats", "requested_formats", "requested_downloads", "thumbnails", "automatic_captions", "subtitles",
             "heatmap", "http_headers", "_format_sort_fields", "fragments", "_version")


def enrich_music(files, known, batch, album_of, music_info):
    """音乐下完后给每首写标签，不做筛选：
      来源网站的播放数、点赞数、发布日期、下载日期、原链接；
      Last.fm 的风格、听众、总播放、重播率、专辑（按 .info.json 里的标题、艺人、频道查；排名时查过的直接用；没有 key 跳过）；
      歌词（LRCLIB，优先带时间轴）和歌词状态 LYRICS_STATUS。
    封面 yt-dlp 已嵌进文件。每首的 .info.json 精简后合成批次里的一个 download-meta-<时间>.json，单个的删掉，不留一堆小文件。"""
    import lyrics
    import rank
    key = os.environ.get("OMNIHUB_LASTFM_KEY", "")
    audio = [f for f in files if not f.endswith(".json") and os.path.isfile(f)]
    if not audio:
        return
    metas, pairs = [], []
    for f in audio:
        meta_path = os.path.splitext(f)[0] + ".info.json"
        meta = {}
        if os.path.isfile(meta_path):
            with open(meta_path, encoding="utf-8") as fh:
                meta = json.load(fh)
        metas.append((meta_path, meta))
        pairs.append(rank.guess_artist_track({"title": meta.get("title") or os.path.basename(f), "artist": meta.get("artist"),
                                              "track": meta.get("track"), "uploader": meta.get("channel") or meta.get("uploader") or ""}))
    todo = [i for i, (_, m) in enumerate(metas) if m.get("id") not in known] if key else []
    if todo:
        progress(0, len(audio), f"查 Last.fm（{len(todo)} 首）")
    looked = dict(zip(todo, rank.lastfm_many([pairs[i] for i in todo], key))) if todo else {}
    infos = [known.get(m.get("id")) or looked.get(i) for i, (_, m) in enumerate(metas)]

    # 歌词：先用 Last.fm 纠正过的艺人与曲名，再用从标题拆的
    progress(0, len(audio), f"查歌词（{len(audio)} 首）")
    queries = []
    for (_, meta), pair, info in zip(metas, pairs, infos):
        names = [(info.get("lastfm_artist"), info.get("lastfm_track"))] if info else []
        queries.append((names + [pair], meta.get("duration")))
    found = lyrics.find_many(queries)

    tagged, with_lastfm, records = 0, 0, []
    counts = dict.fromkeys(("synced", "plain", "instrumental", "none", "error"), 0)
    places = [music_place(m, i, p, album_of.get(f), music_info.get(m.get("id"))) for f, (_, m), i, p in zip(audio, metas, infos, pairs)]
    # 同一张专辑放在同一位歌手名下：取专辑里出现最多的歌手（合作曲不把专辑拆散）
    votes = {}
    for artist, album, _, _ in places:
        if album:
            votes.setdefault(album, []).append(artist)
    owner = {album: max(set(names), key=names.count) for album, names in votes.items()}
    places = [(owner[album] if album else artist, album, number, track) for artist, album, number, track in places]
    placed = 0
    for f, (meta_path, meta), info, lyr, place in zip(audio, metas, infos, found, places):
        artist, album, number, track = place
        fields = source_fields(meta)
        ytm = music_info.get(meta.get("id")) or {}
        # 年份用发行年份（yt-dlp 写进 DATE 的是视频上传日期）
        std = {"artist": artist, "title": track, "album": album, "number": number, "date": str(ytm.get("year") or "")}
        if ytm.get("year"):   # 纯音轨版的上传日期不是发行日期，用专辑的发行年份
            fields["RELEASE_DATE"] = str(ytm["year"])
        if ytm.get("album") and ytm.get("album_type"):
            fields["RELEASE_TYPE"] = ytm["album_type"]
        if info:
            fields.update(lastfm_fields(info))
            with_lastfm += 1
        fields["LYRICS_STATUS"] = lyr["status"]
        if lyr["match"]:
            fields["LYRICS_MATCH"] = lyr["match"]
        counts[lyr["status"]] += 1
        try:
            if write_tags(f, fields, "; ".join((info or {}).get("tags") or []), lyr["text"], std):
                tagged += 1
        except Exception as e:
            log(f"写入标签失败 {os.path.basename(f)}：{e}")
        try:
            f = move_music(batch, f, artist, album, number, track, meta.get("id") or "")
            placed += 1
        except OSError as e:
            log(f"归档失败 {os.path.basename(f)}：{e}")
        if meta:
            rec = {k: v for k, v in meta.items() if k not in META_DROP}
            rec["omnihub_file"] = os.path.relpath(f, batch)
            if info:
                rec["lastfm"] = {k: info.get(k) for k in ("listeners", "playcount", "lastfm_album", "tags", "lastfm_name")}
            rec["lyrics_status"], rec["lyrics_match"] = lyr["status"], lyr["match"]
            if ytm:
                rec["youtube_music"] = ytm
            records.append(rec)
        if os.path.isfile(meta_path):
            os.remove(meta_path)
    if placed == len(audio):
        shutil.rmtree(os.path.join(batch, ".incoming"), ignore_errors=True)
    if records:
        with open(os.path.join(batch, time.strftime("download-meta-%Y%m%d-%H%M%S.json")), "w", encoding="utf-8") as fh:
            json.dump(records, fh, ensure_ascii=False)
    log(f"标签已写进 {tagged}/{len(audio)} 首（播放、点赞、发布日期、下载日期）；其中 {with_lastfm} 首有 Last.fm 信息"
        + ("" if key else "（没有 Last.fm API key，跳过）"))
    log(f"歌词：带时间轴 {counts['synced']}，只有纯文本 {counts['plain']}，纯音乐 {counts['instrumental']}，"
        f"查不到 {counts['none']}，查询失败 {counts['error']}（状态写在标签 LYRICS_STATUS 里，可在音声里筛出来）")


def ranked(a):
    """按质量挑：每个链接依次 读列表页 →（音乐）查 Last.fm 重播率 → 排名取前 N → 下载这些单个视频。
    排名表写进日志，也存进批次的 ranking-<时间>.json（移入资源库时收进下载元数据）。"""
    import rank
    done = set()
    if a.archive and os.path.isfile(a.archive):
        with open(a.archive, encoding="utf-8", errors="replace") as f:
            done = {line.split(" ", 1)[1].strip() for line in f if " " in line}
    files, skipped, errors, report = [], 0, 0, []
    for n, url in enumerate(a.urls):
        url = normalize(url)
        platform = platform_of(url)
        cookies = cookie_file(platform["id"])
        log(f"[{n + 1}/{len(a.urls)}] {url}")
        try:
            title, chosen, rule = rank.pick(url, cookies, a.rank, a.top, done, progress, log)
        except Exception as e:
            log(f"   提取或排名失败：{e}")
            errors += 1
            continue
        report.append({"url": url, "title": title, "rule": rule, "chosen": chosen})
        for c in chosen:
            if c.get("listeners") is not None:
                a.lastfm_known[c["id"]] = {k: c.get(k) for k in ("listeners", "playcount", "lastfm_album", "tags", "lastfm_name")}
        if not chosen:
            continue
        sub = argparse.Namespace(**vars(a))
        sub.per_list, sub.items = 0, ""
        if a.scheme == "music" and rule == "album" and not a.album:
            sub.album = title   # 完整专辑进自己的文件夹（下载顺序就是原曲序）
        f, s, e = ytdlp([c["url"] for c in chosen], sub, platform)
        files, skipped, errors = files + f, skipped + s, errors + e
    if report:
        name = os.path.join(a.batch, time.strftime("ranking-%Y%m%d-%H%M%S.json"))
        with open(name, "w", encoding="utf-8") as f:
            json.dump(report, f, ensure_ascii=False, indent=1)
    return files, skipped, errors


def main():
    p = argparse.ArgumentParser(description="按方案下载到暂存区")
    p.add_argument("urls", nargs="+")
    p.add_argument("--batch", required=True, help="暂存区批次目录")
    p.add_argument("--scheme", choices=["music", "youtube", "bilibili", "x", "other"], default="other")
    p.add_argument("--max-height", type=int, default=1080, help="视频最高分辨率（高），0 表示不限")
    p.add_argument("--album", default="", help="音乐：专辑名（不给时用播放列表名）")
    p.add_argument("--items", default="", help="只下播放列表里的这些序号，如 1-10,15")
    p.add_argument("--per-list", type=int, default=0, help="每个列表/频道只取前 N 个，0 表示全部")
    p.add_argument("--rank", choices=["none", "replay", "views"], default="none",
                   help="按质量挑：replay 重播率（Last.fm）、views 播放量；none 不挑，全下")
    p.add_argument("--top", type=int, default=30, help="按质量挑时每个链接取前多少个")
    p.add_argument("--archive", default="", help="已下载名单文件（下过的不再下）")
    p.add_argument("--x-media", choices=["all", "image", "video"], default="all")
    p.add_argument("--x-limit", type=int, default=0, help="X：每个用户最多下最近这么多个文件，0 表示不限")
    a = p.parse_args()
    a.lastfm_known = {}   # 排名时查到的 Last.fm 信息（按视频 ID），下载后写进标签时复用
    a.album_of = {}       # 文件 → (专辑名, 曲序)：指定了专辑时由 ytdlp() 记下
    a.music_info = {}     # 视频 ID → YouTube Music 上的曲名、歌手、专辑、曲序：music_targets() 查到的

    if a.scheme == "x":
        files, skipped, errors = gallerydl(a.urls, a)
    elif a.scheme == "other":
        files, skipped, errors = [], 0, 0
        groups = {}
        for u in a.urls:
            groups.setdefault(platform_of(u)["id"] or urlparse(u).hostname or "", []).append(u)
        for urls in groups.values():
            f, s, e = ytdlp(urls, a, platform_of(urls[0]))
            files, skipped, errors = files + f, skipped + s, errors + e
    elif a.rank != "none":
        files, skipped, errors = ranked(a)
    else:
        # 音乐也可能来自 B 站等：按第一个链接认平台（决定用哪份 Cookie）
        platform = platform_of(a.urls[0]) if a.scheme == "music" else next(
            (p for p in PLATFORMS if p["id"] == SCHEME_PLATFORM[a.scheme]), platform_of(a.urls[0]))
        files, skipped, errors = ytdlp(a.urls, a, platform)
    if a.scheme == "music":
        enrich_music(files, a.lastfm_known, a.batch, a.album_of, a.music_info)
    progress(len(a.urls), len(a.urls), f"完成：新下载 {len(files)} 个文件，跳过已下载 {skipped} 个，出错 {errors} 个")
    for h in sorted(hints):
        log("提示：" + h)
    print("OMNIHUB_RESULT " + json.dumps({"files": len(files), "skipped": skipped, "errors": errors, "path": a.batch, "hints": sorted(hints)}, ensure_ascii=False), flush=True)
    if not files and errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
