#!/usr/bin/env python3
"""按方案下载到暂存区的一个批次（不直接进资源库）。由 omniHub 服务按需调用。

方案（--scheme）决定工具和去向：
  music     YouTube 的先换成 YouTube Music 上的纯音轨版（MV 有片头片尾，不下它的音频；见 ytmusic.py），
            yt-dlp 只取最好的音轨（不转码，嵌封面、歌词与标签）→ media_library/audio/music/<歌手>/<曲名>（单曲）
            或 …/<歌手>/<专辑>/<曲序 曲名>（属于 Album、EP 的歌，单独下的也归进专辑）
  youtube   yt-dlp 视频 → media_library/shortvideo/YouTube/<频道>/<日期>_<标题>_<id>
  bilibili  yt-dlp 视频 → media_library/shortvideo/B站/<UP主>/…
  x         gallery-dl → 只要作者本人的原图，进相册里单独的图库「X」（NSFW），文件名带作者（见 SOCIAL）
  other     按链接域名认平台（平台表见 OMNIHUB_PLATFORMS），视频 → media_library/shortvideo/<平台>/<作者>/
元数据（yt-dlp 的 .info.json、gallery-dl 的 .json）下完后精简、合成批次里的一个 download-meta-<时间>.json，
单个的删掉，不留一堆零碎文件；移入资源库时追加进 .omni_data/download-meta.jsonl，留给以后打分。
音乐另外把封面、歌词、各项数据写进音频文件。
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


# 文件名里不能用的字符换成样子相近的全角字符（「Song: Part 2」「bad idea right?」看着还是原样）
UNSAFE = str.maketrans({"\\": "＼", "/": "／", ":": "：", "*": "＊", "?": "？", '"': "＂", "<": "＜", ">": "＞", "|": "｜",
                        "\r": " ", "\n": " ", "\t": " "})


def safe(name):
    name = name.translate(UNSAFE).strip(" .")
    return name[:100] or "未分类"


def normalize(url):
    """YouTube 频道首页是「视频 / Shorts / 直播」几个分页；没指定分页时取视频页（与 probe.py 相同）。"""
    if re.search(r"youtube\.com/(@[^/?#]+|channel/[^/?#]+|c/[^/?#]+|user/[^/?#]+)/?(\?.*)?$", url):
        return re.sub(r"/?(\?.*)?$", r"/videos\1", url, count=1)
    return url


def ytdlp(urls, a, platform):
    """一个 yt-dlp 进程按顺序下全部链接（单个视频或整个列表）；返回 (下完的文件, 已在名单里跳过的数量, 出错的数量)。"""
    music = a.scheme == "music" and not a.mv_artist   # 歌手的 MV 也走音乐方案，但下成视频
    if not (music and platform["id"] == "youtube"):
        urls = [normalize(u) for u in urls]
    # 进度总数：每个链接取前 N 个时最多 链接数×N；否则按链接数（列表展开后会超过，随下随长）
    expected = len(urls) * a.per_list if a.per_list > 0 else len(urls)
    cmd = [sys.executable, "-m", "yt_dlp", "--newline", "--no-colors", "--ignore-errors", "--windows-filenames",
           "--write-info-json", "--no-write-playlist-metafiles", "--no-simulate",
           "--no-quiet",  # --print 默认会静音；跳过、进度信息要留在日志里
           "--print", "after_move:OMNIHUB_FILE %(filepath)s"]
    if a.archive:   # MV 单独一份名单：MV 的视频 ID 记在音乐名单里，以后再给这个 MV 链接时就不会去换纯音轨版了
        cmd += ["--download-archive", os.path.splitext(a.archive)[0] + "-mv.txt" if a.mv_artist else a.archive]
    cookies = cookie_file(platform["id"])
    if cookies:
        cmd += ["--cookies", cookies]
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        cmd += ["--proxy", os.environ["OMNIHUB_YTDLP_PROXY"]]
    if music and platform["id"] == "youtube" and not a.plan:
        urls, skipped0, errors0 = music_targets(urls, a, cookies)   # 已换成纯音轨版的单曲链接，已按每个列表取前 N 个
        expected = len(urls)
    elif a.per_list > 0:
        cmd += ["--playlist-items", f"1:{a.per_list}"]   # 每个列表/频道只取前 N 个（频道、播放列表按新→旧排列）
    elif a.items:
        cmd += ["--playlist-items", a.items]
    if music:
        # 先下到批次里的 .incoming/<视频 ID>，写完标签后由 organize_music 按「歌手 / 单曲、歌手 / 专辑 / 歌」归档
        # 纯音轨版的缩略图是专辑封面两边加色块（16:9），裁成中间的正方形再嵌进去
        cmd += ["-f", "bestaudio/best", "-x", "--audio-format", "best", "--embed-metadata", "--embed-thumbnail",
                "--convert-thumbnails", "jpg", "--ppa", "ThumbnailsConvertor:-q:v 2 -vf crop=\"'min(iw,ih)':'min(iw,ih)'\"",
                "-o", os.path.join(a.batch, ".incoming", "%(id)s.%(ext)s")]
    elif a.mode == "audio":   # 有声书 / 音频：音声/standard/<书名>/<标题>，书名默认用播放列表名
        album = safe(a.album) if a.album else "%(playlist_title,title)s"
        cmd += ["-f", "bestaudio/best", "-x", "--audio-format", "best", "--embed-metadata", "--embed-thumbnail",
                "--convert-thumbnails", "jpg",
                "-o", os.path.join(a.batch, "media_library", "audio", "standard", album, "%(title).100B.%(ext)s")]
    else:
        h = a.max_height
        fmt = "bv*+ba/b" if h <= 0 else f"bv*[height<={h}]+ba/b[height<={h}]/b"
        out = os.path.join(a.batch, "media_library", "shortvideo", platform["dir"], "%(channel,uploader,uploader_id|未知作者)s",
                           *([safe(a.group)] if a.group else []),   # MV 按艺人分：<公司频道>/<艺人>/
                           "%(upload_date>%Y-%m-%d)s_%(title).80B_%(id)s.%(ext)s")
        if a.mv_artist:
            out = os.path.join(a.batch, "media_library", "shortvideo", "MV", safe(a.mv_artist), "%(title).80B [%(id)s].%(ext)s")
        cmd += ["-f", fmt, "--merge-output-format", "mp4", "-o", out]
    files, skipped, errors = [], 0, 0
    if music and platform["id"] == "youtube" and not a.plan:
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
    信息记进 a.music_info[视频 ID]。MV 找不到纯音轨版的不下。
    歌手页（YouTube Music 歌手页或歌手的官方频道）：取全部专辑、EP、单曲（同一首只留最早那版），正式 MV 记进 a.mv_jobs 另外下成视频。
    返回 (要下的单曲链接, 已下载过跳过的数量, 出错的数量)。"""
    import yt_dlp
    import ytmusic
    done = archived(a)
    ids, preset, errors = [], {}, 0
    opts = {"extract_flat": "in_playlist", "quiet": True, "no_warnings": True, "skip_download": True}
    if cookies:
        opts["cookiefile"] = cookies
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        opts["proxy"] = os.environ["OMNIHUB_YTDLP_PROXY"]
    for i, url in enumerate(urls):
        progress(i, len(urls), f"读第 {i + 1}/{len(urls)} 个链接：{url}")
        m = re.search(r"[?&]list=(OLAK5uy_[\w-]+)", url)
        try:
            cid = channel_id(url, opts)
            page = ytmusic.artist(cid, log, progress) if cid else None
            if cid and not page:
                log(f"{url} 不是 YouTube Music 上的歌手，按普通频道展开")
            if page:
                songs, mvs = pick_items(page["songs"], a), pick_items(page["mvs"], a)
                log(f"歌手 {page['name']}：歌 {len(songs)} 首（同一首只留最早发行的那版），正式 MV {len(mvs)} 个")
                ids += [vid for vid, _ in songs]
                preset.update(songs)
                if mvs:
                    a.mv_jobs.append((page["name"], mvs))
                continue
            if m:   # 整张专辑：直接取 YouTube Music 上的曲目（YouTube 网页版的专辑列表里常混着 MV）
                album, songs = ytmusic.album_tracks(m.group(1), log)
                songs = pick_items(songs, a)
                log(f"专辑《{album.get('title')}》{len(album.get('tracks') or [])} 首，要下的纯音轨版 {len(songs)} 首")
                ids += [vid for vid, _ in songs]
                preset.update(songs)
                continue
            with yt_dlp.YoutubeDL(opts) as ydl:
                info = ydl.extract_info(url if "music.youtube.com" in url else normalize(url), download=False)
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
            a.music_done.append({"id": vid, **{k: preset.get(vid, {}).get(k) for k in ("title", "artists")}})
            continue
        progress(n, len(ids), f"确认第 {n + 1}/{len(ids)} 首：" + (preset[vid].get("title") if vid in preset else f"{vid}（找纯音轨版和专辑）"))
        try:
            target, info, note = (vid, preset[vid], "") if vid in preset else ytmusic.resolve(vid)
        except Exception as e:
            log(f"ERROR: YouTube Music 查询失败 {vid}：{e}")
            errors += 1
            continue
        if note:
            log(f"{vid}（{' / '.join(info.get('artists') or [])} - {info.get('title', '')}）：{note}")
        if not target:
            a.music_skipped.append({"id": vid, "title": info.get("title"), "artists": info.get("artists"), "reason": note})
            continue
        if target in done or target in a.music_info:
            skipped += 1
            if target in done:
                a.music_done.append({"id": target, "title": info.get("title"), "artists": info.get("artists")})
            continue
        # 来源：纯音轨原样 / 原视频换成了纯音轨 / 没有纯音轨用的官方音频
        info["source"] = ("官方音频" if info.get("kind") == "OMV" else "纯音轨") if target == vid else \
            ("MV 或官方上传换成纯音轨" if info.get("kind") == "OMV" else "用户上传换成纯音轨")
        info["from"] = vid
        a.music_info[target] = info
        targets.append("https://www.youtube.com/watch?v=" + target)
    log(f"要下 {len(targets)} 首（{len(ids)} 个视频里，已下载过 {skipped} 个，出错 {errors} 个）")
    return targets, skipped, errors


def planned_path(info):
    """按 YouTube Music 信息算出的归档位置（与 enrich_music 归档时的规则相同，只是没有 yt-dlp 的信息可参考）。"""
    artist, album, number, track = music_place({}, None, (None, None), None, info)
    name = (f"{int(number):02d} " if number else "") + safe(track)
    return "/".join([safe(artist)] + ([safe(album)] if album else []) + [name])


# 视频方案的模式（YouTube 三种，B 站只有普通视频）：挑什么、怎么排、默认勾哪些、放哪里都由模式决定，界面上只选模式
#   video  普通视频：全部，按播放量排；短视频/<平台>/<发布者>/
#   girls      女团 MV：只留正式 MV（按标题，见 ytmusic.is_mv），从标题认艺人，只要女团和女团成员（个人作品也算；Wikidata 判断，
#              见 wikidata.py）；短视频/<平台>/<公司频道>/<艺人>/，4K。和音乐方案歌手页下的「音乐 MV」（短视频/MV/<歌手>/）是两类
#   girlvideo  女团相关视频：不限 MV（dingo 这种频道的现场、竖屏 live 等），同样只要女团和女团成员；按发布者放，4K
#   audio  有声书 / 音频：只要声音，按列表顺序；音声/standard/<书名>/，书名默认是播放列表名
PICK_ALL_UP_TO = 50   # 候选不多于这么多时默认全勾；多了：普通视频勾播放量前 PICK_TOP 个（女团 MV 确认是女团的都勾）
PICK_TOP = 30
UNKNOWN_ARTIST = "未认出艺人"


def wikidata_norm(name):
    import wikidata
    return wikidata.norm(name)


def is_channel(url):
    """频道首页、UP 主空间（整个频道）；合集、系列、收藏夹、播放列表、单个视频不算。"""
    if re.search(r"youtube\.com/(@[^/?#]+|channel/[^/?#]+|c/[^/?#]+|user/[^/?#]+)(/(videos|featured)?)?/?(\?.*)?$", url):
        return True
    return bool(re.search(r"space\.bilibili\.com/\d+(/video|/upload/video)?/?(\?.*)?$", url))


def mv_song(title):
    """MV 标题里引号中的歌名（「IVE 아이브 'LOVE DIVE' MV」→ LOVE DIVE），没有就用整个标题。"""
    m = re.search(r"[\"'‘“「『【]([^\"'’”」』】]+)[\"'’”」』】]", title or "")
    return m.group(1).strip() if m else title


def list_videos(a):
    """视频方案读列表（只读列表页，不逐个打开视频）：按模式筛选、排序、定默认勾选。返回列表（plan）。"""
    import rank
    import ytmusic
    done, videos, skipped, ndone, errors, other_count, shorts = archived(a), [], [], 0, 0, 0, 0
    book = ""
    for i, url in enumerate(a.urls):
        url = normalize(url)
        progress(i, len(a.urls), f"读第 {i + 1}/{len(a.urls)} 个链接：{url}")
        try:
            title, entries, _ = rank.extract_list(url, cookie_file(platform_of(url)["id"]), 0)
        except Exception as e:
            log(f"ERROR: 读取失败 {url}：{e}")
            errors += 1
            continue
        log(f"== {title or url}：列表里 {len(entries)} 个")
        whole_channel = is_channel(url)   # 频道首页 / UP 主空间：太多时只默认勾一部分；合集、收藏夹、播放列表是挑好的，全勾
        book = book or (title if len(entries) > 1 else (entries[0]["title"] if entries else ""))
        for k, e in enumerate(entries):
            if e["id"] in done:
                ndone += 1
                continue
            if "/shorts/" in (e.get("url") or "") or "/shorts/" in (e.get("source") or ""):   # 不下 Shorts（Shorts 分页、单个 Shorts 链接）
                shorts += 1
                continue
            channel = e.get("uploader") or re.sub(r"\s+-\s+(Videos|Shorts|Live|Home|Playlists)$", "", title or "")   # 「HYBE LABELS - Videos」→ 频道名
            item = {"id": e["id"], "url": e["url"], "title": e["title"], "channel": channel, "whole": not whole_channel,
                    "views": e.get("views"), "list": title, "index": k + 1, "group": ""}
            if a.mode == "girlvideo":
                item["group"] = ytmusic.mv_artist(e["title"]) or UNKNOWN_ARTIST
            if a.mode == "girls":
                if not ytmusic.is_mv(e["title"]):
                    if ytmusic.MV_TITLE.search(e["title"]):   # 带 MV 字样但不是正式 MV 的列出来，其余只记数量
                        skipped.append({"id": e["id"], "title": e["title"], "artists": [], "reason": "预告、练习室、舞台、花絮、反应视频等"})
                    else:
                        other_count += 1
                    continue
                item["group"] = ytmusic.mv_artist(e["title"]) or UNKNOWN_ARTIST
            videos.append(item)
    if a.mode in ("girls", "girlvideo"):
        for v in videos:   # 「…ㅣdingo Music」截出来的「Music」是频道名，不是艺人
            if v["group"] != UNKNOWN_ARTIST and wikidata_norm(v["group"]) in wikidata_norm(v["channel"]):
                v["group"] = UNKNOWN_ARTIST
    if a.mode in ("girls", "girlvideo"):   # 同一艺人大小写不同（Hyolyn / HYOLYN）并成一组，用出现最多的写法
        spell = {}
        for v in videos:
            spell.setdefault(v["group"].casefold(), []).append(v["group"])
        for v in videos:
            names = spell[v["group"].casefold()]
            v["group"] = max(set(names), key=names.count)
        # 女团和女团成员默认勾上；其余（男团、其他歌手、查不到、认不出艺人的）列在「其他艺人」里，默认不勾
        import wikidata
        names = sorted({v["group"] for v in videos if v["group"] != UNKNOWN_ARTIST})
        progress(0, 1, f"按 Wikidata 的女团名字表比对 {len(names)} 个艺人")
        kinds = wikidata.classify(names, log)
        for v in videos:
            v["kind"], v["entity"] = kinds.get(v["group"], ("other", ""))
        # 同一个艺人的不同写法（SUZY / 수지、WJSN LIVE → WJSN）并成一组，用出现最多的英文写法
        spellings = {}
        for v in videos:
            if v["entity"]:
                spellings.setdefault(v["entity"], []).append(v["group"])
        for v in videos:
            if v["entity"]:
                names_ = spellings[v["entity"]]
                latin = [x for x in names_ if re.search(r"[A-Za-z]", x) and not re.search(r"[\uac00-\ud7a3]", x)] or names_
                v["group"] = min(set(latin), key=lambda x: (-latin.count(x), len(x)))
        n_girls = len({v["group"] for v in videos if v["kind"] == "girl"})
        log(f"   {len(names)} 个艺人里女团和女团成员 {n_girls} 个")
        videos.sort(key=lambda v: (v["kind"] != "girl", v["group"] == UNKNOWN_ARTIST, v["group"].casefold(), -(v["views"] or -1)))
    elif a.mode == "video":
        videos.sort(key=lambda v: -(v["views"] if v["views"] is not None else -1))
    from_channels = [v for v in videos if not v["whole"]]
    many = len(from_channels) > PICK_ALL_UP_TO
    top = {v["id"] for v in from_channels[:PICK_TOP]}   # 已按播放量排好
    for v in videos:
        v["picked"] = (v.get("kind") == "girl") if a.mode in ("girls", "girlvideo") else \
            (v["whole"] or not many or a.mode == "audio" or v["id"] in top)
    plan = {"urls": a.urls, "mode": a.mode, "videos": videos, "book": book, "skipped": skipped, "other_count": other_count,
            "done_count": ndone, "shorts": shorts, "songs": [], "mvs": [], "done": [], "other": [], "errors": errors,
            "pick_note": f"频道里视频太多：默认勾播放量前 {PICK_TOP} 个（合集、收藏夹、播放列表全勾）" if many and a.mode == "video" else ""}
    log(f"列表：候选 {len(videos)} 个（默认勾上 {sum(v['picked'] for v in videos)} 个）；带 MV 字样但不要的 {len(skipped)} 个；"
        f"其他视频 {other_count} 个；Shorts {shorts} 个（不下）；已下载过 {ndone} 个；出错 {errors} 个")
    return plan


def write_video_plan(a):
    save_plan(a, list_videos(a))


def save_plan(a, plan):
    os.makedirs(os.path.dirname(a.plan_out), exist_ok=True)
    with open(a.plan_out + ".tmp", "w", encoding="utf-8") as f:
        json.dump(plan, f, ensure_ascii=False)
    os.replace(a.plan_out + ".tmp", a.plan_out)


def write_plan(a):
    """只读取、不下载：把会下什么写进 a.plan_out（JSON），给界面列出来勾选。
    songs 要下的歌（含归档位置、来源）；mvs 歌手的 MV；skipped 不下的及原因；done 已下载过的；other 现在总是空的（音乐只收 YouTube）。"""
    youtube = [u for u in a.urls if platform_of(u)["id"] == "youtube"]
    other = [u for u in a.urls if platform_of(u)["id"] != "youtube"]
    targets, _, errors = music_targets(youtube, a, cookie_file("youtube")) if youtube else ([], 0, 0)
    mv_done = set()
    if a.archive and os.path.isfile(os.path.splitext(a.archive)[0] + "-mv.txt"):
        with open(os.path.splitext(a.archive)[0] + "-mv.txt", encoding="utf-8", errors="replace") as f:
            mv_done = {line.split(" ", 1)[1].strip() for line in f if " " in line}
    songs = [{"id": vid, "path": planned_path(info), **{k: info.get(k) for k in
              ("title", "artists", "album", "album_type", "number", "year", "source", "from")}}
             for vid, info in a.music_info.items()]
    mvs = [{"id": vid, "title": title, "artist": name, "path": "MV/" + safe(name) + "/", "done": vid in mv_done}
           for name, items in a.mv_jobs for vid, title in items]
    plan = {"urls": a.urls, "songs": songs, "mvs": mvs, "skipped": a.music_skipped, "done": a.music_done,
            "other": other, "errors": errors}
    save_plan(a, plan)
    log(f"列表：要下 {len(songs)} 首、MV {sum(1 for m in mvs if not m['done'])} 个；不下 {len(a.music_skipped)} 首；"
        f"已下载过 {len(a.music_done)} 首；其他网站链接 {len(other)} 个；出错 {errors} 个")


def load_plan(a):
    """按读取好的列表下：去掉 --exclude 的，歌的信息直接用列表里的，不再查 YouTube Music。返回 (歌的链接, 其他网站链接)。"""
    with open(a.plan, encoding="utf-8") as f:
        plan = json.load(f)
    skip = set(filter(None, a.exclude.split(",")))
    for s in plan["songs"]:
        if s["id"] not in skip:
            a.music_info[s["id"]] = {k: v for k, v in s.items() if k not in ("id", "path")}
    mvs = {}
    for m in plan["mvs"]:
        if m["id"] not in skip and not m["done"]:
            mvs.setdefault(m["artist"], []).append((m["id"], m["title"]))
    a.mv_jobs = list(mvs.items())
    return ["https://www.youtube.com/watch?v=" + v for v in a.music_info], plan.get("other") or []


def download_videos(a, plan, skip, platform):
    """视频方案按列表下（去掉 skip 里的）：MV 按艺人、有声书按书分组，每组一个 yt-dlp 进程，一个一个下。
    每个视频要写进文件的元信息（模式、公司、艺人、歌名、书名、集数）记在 a.video_extra[视频 ID]。"""
    groups = {}
    for v in plan["videos"]:
        if v["id"] in skip:
            continue
        if a.mode == "audio":
            key = ("", a.album or plan.get("book") or v["list"])
            a.video_extra[v["id"]] = {"fields": {"OMNIHUB_MODE": "audio"}, "std": {"album": key[1], "title": v["title"], "number": v["index"]}}
        elif a.mode == "girls":
            key = (v["group"] if v["group"] != UNKNOWN_ARTIST else "", "")
            a.video_extra[v["id"]] = {"fields": {"OMNIHUB_MODE": "girl_group_mv", "COMPANY": v["channel"]},
                                      "std": {"artist": key[0], "title": mv_song(v["title"])}}
        elif a.mode == "girlvideo":   # 按发布者放，艺人写进标签
            key = ("", "")
            a.video_extra[v["id"]] = {"fields": {"OMNIHUB_MODE": "girl_group_video"},
                                      "std": {"artist": v["group"] if v["group"] != UNKNOWN_ARTIST else ""}}
        else:
            key = ("", "")
            a.video_extra[v["id"]] = {"fields": {"OMNIHUB_MODE": "video"}, "std": {}}
        groups.setdefault(key, []).append(v["url"])
    files, skipped, errors = [], 0, 0
    for (group, album), urls in groups.items():
        sub = argparse.Namespace(**vars(a))
        sub.per_list, sub.items, sub.group, sub.album = 0, "", group, album
        f, s_, e = ytdlp(urls, sub, platform)
        files, skipped, errors = files + f, skipped + s_, errors + e
    return files, skipped, errors


def channel_id(url, opts):
    """频道链接（/channel/UC…、/@名字、/c/…、/user/…）的频道 ID；不是频道链接时返回空。"""
    import yt_dlp
    m = re.search(r"youtube\.com/channel/(UC[\w-]+)", url)
    if m:
        return m.group(1)
    if not re.search(r"youtube\.com/(@[^/?#]+|c/[^/?#]+|user/[^/?#]+)/?([/?#]|$)", url):
        return ""
    with yt_dlp.YoutubeDL({**opts, "playlist_items": "1"}) as ydl:
        return ydl.extract_info(normalize(url), download=False).get("channel_id") or ""


def download_mvs(a, platform):
    """歌手的正式 MV 下成视频：media_library/shortvideo/MV/<歌手>/<标题> [ID].mp4。"""
    files, skipped, errors = [], 0, 0
    for name, ids in a.mv_jobs:
        log(f"下载 {name} 的 MV（{len(ids)} 个）")
        sub = argparse.Namespace(**vars(a))
        sub.mv_artist, sub.per_list, sub.items = name, 0, ""
        for v, _ in ids:   # 音乐方案歌手页下的 MV（欧美等音乐 MV），和 YouTube 的女团 MV 分开标记
            a.video_extra[v] = {"fields": {"OMNIHUB_MODE": "music_mv"}, "std": {"artist": name}}
        f, s, e = ytdlp(["https://www.youtube.com/watch?v=" + v for v, _ in ids], sub, platform)
        files, skipped, errors = files + f, skipped + s, errors + e
    return files, skipped, errors


# 网站要求登录时给出的中文提示（日志最后一行）
LOGIN_HINTS = {
    "Sign in to confirm": "{} 要求登录才能下载：先在 Chrome 里登录，再到「下载 → 账号与 Cookie」点「从 Chrome 读取」。",
    "cookies": "{} 需要登录 Cookie：到「下载 → 账号与 Cookie」读取。",
    "login required": "{} 需要登录：到「下载 → 账号与 Cookie」读取。",
}
hints = set()


# 图文平台（gallery-dl）：每个作者一个 gallery-dl 进程，先下到批次的 .incoming/<平台>/，再按帖子整理：
#   图片 → 相册里这个平台单独的图库（批次的 gallery/<平台>/<作者>/<日期>_<正文>_<帖子编号>_NN.jpg；
#          移入资源库时进图库「<平台>」，可标 NSFW，见 internal/gallery/collections.go）
#   视频 → 短视频/<平台>/<作者>/<日期>_<正文>_<帖子编号>.mp4（X 要选「图片和视频」才下；平台目录标 NSFW，见 collection.json）
# 增量：下载名单里有的跳过。一个作者完整下完过一次（gallery-dl 正常结束）才记下来，之后再下这个作者，
# 连续 20 个都下过就停、只补新帖子；没完整下完过的（第一次中断了）每次都翻到底，下过的跳过，直到补全。
SOCIAL = {
    # X：只要作者本人发的图片（原图），--x-videos 时连视频、GIF（gallery-dl 取最高码率的 mp4）；
    #    作者主页链接一律读「媒体」页（主页时间线里可能混着转发；媒体页里有作者回复里带图的帖子）。
    #    作者在自己推文下面接着回复补图（一条发不下）的也要（replies=self），整理时按会话合成一个图集
    "x": {"dir": "X", "cookie": "x", "user_url": "https://x.com/{}/media", "nsfw": True,
          "profile": r"^https?://(?:www\.|mobile\.)?(?:x|twitter)\.com/([A-Za-z0-9_]{1,15})(?:/(?:media|with_replies|photo)?)?/?(?:\?.*)?$",
          "filter": "not retweet_id", "types": ("photo",), "video_types": ("video", "animated_gif"),
          "options": ["retweets=false", "quoted=false", "replies=self", "size=orig"]},
}


def complete_path(a):
    return os.path.splitext(a.archive)[0] + "-complete.json" if a.archive else ""


def load_complete(a):
    """完整下完过的作者（「平台:链接」→ 日期），和下载名单放在一起。"""
    try:
        with open(complete_path(a), encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def save_complete(a, complete):
    if not complete_path(a):
        return
    with open(complete_path(a) + ".tmp", "w", encoding="utf-8") as f:
        json.dump(complete, f, ensure_ascii=False, indent=1)
    os.replace(complete_path(a) + ".tmp", complete_path(a))


def post_info(meta):
    """gallery-dl 元数据里的作者、帖子编号、日期、正文（X、Pixiv、Instagram 的字段名不同）。"""
    who = meta.get("author") or meta.get("user") or {}
    author = (who.get("name") if isinstance(who, dict) else who) or meta.get("username") or meta.get("owner") or "未知作者"
    # X 的同一串（作者自己回复补图）用会话编号，合成一个帖子
    pid = meta.get("conversation_id") or meta.get("tweet_id") or meta.get("post_id") or meta.get("post_shortcode") or meta.get("id") or ""
    text = meta.get("content") or meta.get("description") or meta.get("title") or ""
    text = re.sub(r"https?://\S+|#\S+|@\S+", " ", str(text))   # 去掉链接、话题、@
    text = " ".join(text.split())[:40] or "无标题"
    day = str(meta.get("date") or "")[:10] or "0000-00-00"
    return str(author), str(pid), day, text


def social_url(url, conf):
    """用户名或作者主页链接 → 要读的页面（X 是「媒体」页）；其他链接原样。"""
    url = url.strip()
    m = re.match(conf.get("profile") or r"^$", url)
    if "://" not in url or m:
        return conf["user_url"].format(m.group(1) if m else url.lstrip("@"))
    return url


def gallerydl_base(a, conf, filt=None):
    """gallery-dl 的公共参数：平台的过滤与选项、Cookie、代理、下载名单。filt 是额外的过滤条件（按月份）。"""
    cmd = [sys.executable, "-m", "gallery_dl"]
    types = tuple(conf.get("types") or ()) + (tuple(conf.get("video_types") or ()) if a.x_videos else ())
    conds = [c for c in (f"type in {types!r}" if types else "", conf.get("filter"), filt) if c]
    if conds:
        cmd += ["--filter", " and ".join(f"({c})" for c in conds)]
    for opt in conf.get("options") or []:
        cmd += ["-o", opt]
    if a.archive:
        cmd += ["--download-archive", a.archive + ".gallery-dl.sqlite3"]
    cookies = cookie_file(conf["cookie"])
    if cookies:
        cmd += ["--cookies", cookies]
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        cmd += ["--proxy", os.environ["OMNIHUB_YTDLP_PROXY"]]
    return cmd


def month_filter(months):
    """只要这些月份（YYYY-MM）的帖子；翻到比最早那个月更老的就停（不再往下翻，买来的号几万条老帖子不用读）。"""
    first = min(months)
    y, m = int(first[:4]), int(first[5:7])
    return f"date.strftime('%Y-%m') in {set(months)!r} or (date < datetime({y}, {m}, 1) and abort())"


def list_window(before):
    """读列表一次读 12 个月：before（YYYY-MM，不含；空是下个月）往前 12 个月。返回 (起始月, 过滤条件)：
    比 before 新的跳过，比起始月更老的翻到就停。"""
    if before:
        y, m = int(before[:4]), int(before[5:7])
    else:
        t = time.localtime()
        y, m = t.tm_year + t.tm_mon // 12, t.tm_mon % 12 + 1
    sy, sm = y - 1, m
    filt = f"(date >= datetime({sy}, {sm}, 1) and date < datetime({y}, {m}, 1)) or (date < datetime({sy}, {sm}, 1) and abort())"
    return f"{sy:04d}-{sm:02d}", filt


def write_social_plan(a, scheme):
    """图文平台读取列表：每个作者按月份列出帖子数、图片数、已下载过的张数和几条正文样例，给用户按月勾选（只读不下载）。
    一次只读最近 12 个月（--list-before 给出时读那之前的 12 个月，接在 --list-from 的列表后面），大号不用翻完整个媒体页。"""
    conf = SOCIAL[scheme]
    start, window = list_window(a.list_before)
    old = {}
    if a.list_from:
        with open(a.list_from, encoding="utf-8") as f:
            old = json.load(f)
    done = set()
    db = a.archive + ".gallery-dl.sqlite3" if a.archive else ""
    if db and os.path.isfile(db):
        import sqlite3
        with sqlite3.connect(f"file:{db}?mode=ro", uri=True) as con:
            done = {r[0] for r in con.execute("SELECT entry FROM archive")}
    new_months, errors = [], 0
    for i, url in enumerate(a.urls):
        url = social_url(url, conf)
        progress(i, len(a.urls), f"读 {url}")
        proc = subprocess.run(gallerydl_base(a, conf, window) + ["--simulate", "-j", url], capture_output=True, text=True)
        try:
            rows = [x[-1] for x in json.loads(proc.stdout or "[]") if isinstance(x, list) and len(x) >= 3 and isinstance(x[-1], dict)]
        except ValueError:
            rows = []
        if proc.returncode != 0 and not rows:
            log(f"ERROR: 读取失败 {url}：{(proc.stderr or '').strip()[-300:]}")
            errors += 1
            continue
        by_month = {}
        for meta in rows:
            author, pid, day, text = post_info(meta)
            mo = by_month.setdefault(day[:7], {"posts": set(), "images": 0, "videos": 0, "done": 0, "samples": [], "author": author})
            mo["posts"].add(pid)
            mo["videos" if meta.get("type") in conf.get("video_types", ()) else "images"] += 1
            if f"{meta.get('category', scheme)}{meta.get('tweet_id')}_{meta.get('retweet_id') or 0}_{meta.get('num')}" in done:
                mo["done"] += 1
            if text not in mo["samples"] and len(mo["samples"]) < 3:
                mo["samples"].append(text)
        log(f"{url}：{start} 起的 12 个月里 {len(rows)} 张，{len(by_month)} 个月有图")
        for month in sorted(by_month, reverse=True):
            mo = by_month[month]
            new_months.append({"id": f"u{i}-{month}", "url": url, "author": mo["author"], "month": month, "posts": len(mo["posts"]),
                           "images": mo["images"], "videos": mo["videos"], "done": mo["done"], "samples": mo["samples"], "picked": True})
    months = (old.get("months") or []) + new_months
    # list_start：已经读到哪个月（再往前读时从这里接着）；older_empty：这次往前读的 12 个月一张都没有
    plan = {"urls": a.urls, "mode": "social", "months": months, "list_start": start, "older_empty": bool(old) and not new_months,
            "songs": [], "mvs": [], "videos": [], "skipped": [], "done": [], "other": [], "errors": errors}
    save_plan(a, plan)
    videos = sum(m.get("videos", 0) for m in months)
    log(f"列表：{len(months)} 个月，共 {sum(m['images'] for m in months)} 张图" + (f"、{videos} 个视频" if videos else "")
        + f"（已下载过 {sum(m['done'] for m in months)} 个），读到 {start}")


def gallerydl_plan(a, scheme):
    """按读好的列表下：每个作者只下勾了的月份（去掉 --exclude 里的）。"""
    with open(a.plan, encoding="utf-8") as f:
        plan = json.load(f)
    skip = set(filter(None, a.exclude.split(",")))
    picked = {}
    for m in plan.get("months") or []:
        if m["id"] not in skip:
            picked.setdefault(m["url"], []).append(m["month"])
    if not picked:
        return [], 0, 0
    return gallerydl(list(picked), a, scheme, {u: month_filter(ms) for u, ms in picked.items()})


def gallerydl(urls, a, scheme="x", url_filters=None):
    """图文平台下载（见 SOCIAL）。url_filters：按读好的列表下时，每个链接只要哪些月份。
    返回 (整理好的文件, 已下载过跳过的数量, 出错的数量)。"""
    conf = SOCIAL[scheme]
    files, skipped, errors = [], 0, 0
    incoming = os.path.join(a.batch, ".incoming", conf["dir"])
    for i, url in enumerate(urls):
        url = social_url(url, conf)
        progress(i, len(urls), f"{conf['dir']} · {url}")
        os.makedirs(incoming, exist_ok=True)
        complete = load_complete(a)
        key = f"{scheme}:{url.lower()}"
        filt = (url_filters or {}).get(url)
        cmd = gallerydl_base(a, conf, filt) + ["--write-metadata", "-D", incoming]
        if filt:
            log(f"{url}：只下勾了的月份")
        elif complete.get(key):
            cmd += ["-A", "20"]   # 完整下过：只补新帖子
        else:
            log(f"{url}：还没完整下完过，这次翻到最早，下过的跳过")
        if a.x_limit > 0:   # 测试用：只取前 N 个
            cmd += ["--range", f"1-{a.x_limit}"]
        proc = subprocess.Popen(cmd + [url], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
        for line in proc.stdout:
            line = line.rstrip("\n")
            if line.startswith("# "):
                skipped += 1
            elif "[error]" in line:
                errors += 1
                for word, hint in LOGIN_HINTS.items():
                    if word.lower() in line.lower():
                        hints.add(hint.format(conf["dir"]))
            log(line)
        proc.wait()
        if proc.returncode == 0 and a.x_limit <= 0 and not filt and not complete.get(key):   # 正常结束、没限数量：记为完整下完
            complete[key] = time.strftime("%Y-%m-%d")
            save_complete(a, complete)
            log(f"{url}：已完整下完，以后再下只补新帖子")
    # 按帖子整理
    posts = {}
    for name in sorted(os.listdir(incoming)) if os.path.isdir(incoming) else []:
        path = os.path.join(incoming, name)
        if name.endswith(".json") or not os.path.isfile(path):
            continue
        meta = {}
        if os.path.isfile(path + ".json"):
            with open(path + ".json", encoding="utf-8") as f:
                meta = json.load(f)
            os.remove(path + ".json")
        author, pid, _, _ = post_info(meta)
        posts.setdefault((author, pid), []).append((path, meta))
    root = os.path.join(a.batch, "media_library", "shortvideo", conf["dir"])
    for (author, pid), items in posts.items():
        # 同一串按发出的先后、每条里的第几张排；日期和正文用第一条的
        items.sort(key=lambda x: (int(x[1].get("tweet_id") or 0), int(x[1].get("num") or 0)))
        _, _, day, text = post_info(items[0][1])
        base = f"{day}_{safe(text)}_{pid}"
        folder = os.path.join(root, safe(author))
        images = [(p, m) for p, m in items if not p.lower().endswith(VIDEO_EXT)]
        videos = [(p, m) for p, m in items if p.lower().endswith(VIDEO_EXT)]
        if videos and conf.get("nsfw") and not os.path.isfile(os.path.join(root, "collection.json")):   # 平台目录标 NSFW（默认仅管理员可见）
            os.makedirs(root, exist_ok=True)
            with open(os.path.join(root, "collection.json"), "w", encoding="utf-8") as f:
                json.dump({"name": conf["dir"], "nsfw": True}, f, ensure_ascii=False)
        for k, (path, meta) in enumerate(videos, 1):
            # 一条推几个视频时序号放在编号前面（短视频按「日期_标题_编号」认文件名）
            name = f"{day}_{safe(text)}（{k}）_{pid}" if len(videos) > 1 else base
            dest = os.path.join(folder, name + os.path.splitext(path)[1])
            os.makedirs(folder, exist_ok=True)
            shutil.move(path, dest)
            rec = {kk: v for kk, v in meta.items() if kk not in META_DROP}
            if not embed_info(dest, rec, {"OMNIHUB_MODE": "social"}, {"artist": author, "title": text}):
                a.meta_records.append({**rec, "omnihub_file": os.path.relpath(dest, a.batch)})
            files.append(dest)
        if images:   # 进这个平台的图库：<作者>/<日期>_<正文>_<编号>_NN.jpg；帖子的记录收进批次元数据（图片写不进标签）
            gallery_dir = os.path.join(a.batch, "gallery", conf["dir"])
            author_dir = os.path.join(gallery_dir, safe(author))
            os.makedirs(author_dir, exist_ok=True)
            if not os.path.isfile(os.path.join(gallery_dir, "collection.json")):
                with open(os.path.join(gallery_dir, "collection.json"), "w", encoding="utf-8") as f:
                    json.dump({"name": conf["dir"], "nsfw": bool(conf.get("nsfw"))}, f, ensure_ascii=False)
            names = []
            for k, (path, meta) in enumerate(images, 1):
                dest = os.path.join(author_dir, f"{base}_{k:02d}{os.path.splitext(path)[1].lower()}")
                shutil.move(path, dest)
                files.append(dest)
                names.append(os.path.relpath(dest, gallery_dir))
            rec = {kk: v for kk, v in images[0][1].items() if kk not in META_DROP and kk not in ("num", "filename", "extension", "width", "height")}
            a.meta_records.append({**rec, "omnihub_files": names, "omnihub_gallery": conf["dir"]})
    shutil.rmtree(incoming, ignore_errors=True)
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
        return ytm.get("owner") or artist, album, number, track   # 下歌手页时都放在这位歌手名下
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


def write_tags(path, fields, genre="", lyrics="", std=None, keep_genre=False):
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
        if f.tags is None:   # yt-dlp 下的视频没有嵌标签
            f.add_tags()
        if genre:
            f["\xa9gen"] = [genre]
        elif not keep_genre:
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
        elif not keep_genre:
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
        elif "GENRE" in f.tags and not keep_genre:
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
    封面 yt-dlp 已嵌进文件。返回每首精简后的元数据（由 main 合成批次里的一个 download-meta 文件），单个的 .info.json 删掉。"""
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
        if ytm.get("owner"):
            fields["MUSIC_REQUESTED_ARTIST"] = ytm["owner"]
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
        rec = {}
        if meta:   # 完整的下载记录（精简过）也写进文件：SOURCE_INFO 标签，JSON
            rec = {k: v for k, v in meta.items() if k not in META_DROP}
            if info:
                rec["lastfm"] = {k: info.get(k) for k in ("listeners", "playcount", "lastfm_album", "tags", "lastfm_name")}
            rec["lyrics_status"], rec["lyrics_match"] = lyr["status"], lyr["match"]
            if ytm:
                rec["youtube_music"] = ytm
            fields[INFO_TAG] = json.dumps(rec, ensure_ascii=False)
        embedded = False
        try:
            if write_tags(f, fields, "; ".join((info or {}).get("tags") or []), lyr["text"], std):
                tagged += 1
                embedded = True
        except Exception as e:
            log(f"写入标签失败 {os.path.basename(f)}：{e}")
        try:
            f = move_music(batch, f, artist, album, number, track, meta.get("id") or "")
            placed += 1
        except OSError as e:
            log(f"归档失败 {os.path.basename(f)}：{e}")
        if rec and not embedded:   # 写不进文件的才留在批次的元数据文件里
            rec["omnihub_file"] = os.path.relpath(f, batch)
            records.append(rec)
        if os.path.isfile(meta_path):
            os.remove(meta_path)
    if placed == len(audio):
        shutil.rmtree(os.path.join(batch, ".incoming"), ignore_errors=True)
    log(f"标签已写进 {tagged}/{len(audio)} 首（播放、点赞、发布日期、下载日期）；其中 {with_lastfm} 首有 Last.fm 信息"
        + ("" if key else "（没有 Last.fm API key，跳过）"))
    log(f"歌词：带时间轴 {counts['synced']}，只有纯文本 {counts['plain']}，纯音乐 {counts['instrumental']}，"
        f"查不到 {counts['none']}，查询失败 {counts['error']}（状态写在标签 LYRICS_STATUS 里，可在音声里筛出来）")
    return records


def ranked(a):
    """按质量挑：每个链接依次 读列表页 →（音乐）查 Last.fm 重播率 → 排名取前 N → 下载这些单个视频。
    排名表写进日志，也记进批次的下载元数据（a.meta_records）。"""
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
        a.meta_records.append({"omnihub_ranking": report})
    return files, skipped, errors


# 完整下载记录写进媒体文件的标签名（JSON）：Opus/FLAC 的 Vorbis 注释、MP3 的 TXXX、MP4/M4A 的自由格式
INFO_TAG = "SOURCE_INFO"
EMBEDDABLE = (".mp4", ".m4a", ".m4b", ".mov", ".opus", ".ogg", ".flac", ".mp3")


def embed_info(path, rec, fields=None, std=None):
    """把下载记录（和模式给的字段、标准的歌手/曲名/专辑/曲序）写进媒体文件自己的标签；
    文件格式不支持（webm、mkv、图片）或写失败时返回 False。"""
    if not path.lower().endswith(EMBEDDABLE):
        return False
    try:
        return write_tags(path, {**(fields or {}), INFO_TAG: json.dumps(rec, ensure_ascii=False)}, std=std, keep_genre=True)
    except Exception as e:
        log(f"下载记录写不进 {os.path.basename(path)}：{e}")
        return False


def collect_sidecars(batch, extra=None):
    """批次里每个文件旁边的元数据（yt-dlp 的 <名字>.info.json、gallery-dl 的 <文件>.json）：精简后写进对应的媒体文件
    （embed_info）；写不进的返回，由 main 合成批次的一个元数据文件（每条加 omnihub_file）。单个的 .json 都删掉。"""
    records = []
    for folder, _, names in os.walk(batch):
        for name in names:
            if not name.endswith(".json") or folder == batch or name == "collection.json":   # batch.json、download-meta、图库说明不动
                continue
            path = os.path.join(folder, name)
            try:
                with open(path, encoding="utf-8") as fh:
                    meta = json.load(fh)
            except (OSError, ValueError) as e:
                log(f"元数据读不了，留着：{os.path.relpath(path, batch)}（{e}）")
                continue
            rec = {k: v for k, v in meta.items() if k not in META_DROP} if isinstance(meta, dict) else {"value": meta}
            stem = name[:-len(".info.json")] if name.endswith(".info.json") else name[:-len(".json")]
            media = next((os.path.join(folder, n) for n in names if n != name and (n == stem or os.path.splitext(n)[0] == stem)), path)
            more = (extra or {}).get(rec.get("id")) or {}
            for k, v in (more.get("fields") or {}).items():
                rec[k.lower()] = v
            if media == path or not embed_info(media, rec, more.get("fields"), more.get("std")):
                rec["omnihub_file"] = os.path.relpath(media, batch)
                records.append(rec)
            os.remove(path)
    return records


def write_meta(batch, records):
    if records:
        with open(os.path.join(batch, time.strftime("download-meta-%Y%m%d-%H%M%S.json")), "w", encoding="utf-8") as fh:
            json.dump(records, fh, ensure_ascii=False)


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
    p.add_argument("--x-limit", type=int, default=0, help="X：每个用户最多下最近这么多个文件，0 表示不限")
    p.add_argument("--plan-out", default="", help="音乐：只读取列表，把会下什么写进这个 JSON 文件，不下载")
    p.add_argument("--plan", default="", help="音乐：按读取好的列表（--plan-out 写的文件）下载")
    p.add_argument("--x-videos", action="store_true", help="X：图片之外也下视频、GIF")
    p.add_argument("--list-before", default="", help="图文平台读取列表：读这个月（YYYY-MM）以前的 12 个月，空是最近 12 个月")
    p.add_argument("--list-from", default="", help="图文平台读取列表：接在这个列表文件后面（往前多读一年）")
    p.add_argument("--exclude", default="", help="按列表下载时不要的视频 ID，逗号分隔")
    p.add_argument("--mode", choices=["video", "girls", "girlvideo", "audio"], default="video",
                   help="视频方案的模式：video 普通视频、girls 女团 MV（按公司/艺人分）、girlvideo 女团相关视频（按发布者）、"
                        "audio 有声书/音频（B 站只有 video）")
    a = p.parse_args()
    a.lastfm_known = {}   # 排名时查到的 Last.fm 信息（按视频 ID），下载后写进标签时复用
    a.album_of = {}       # 文件 → (专辑名, 曲序)：指定了专辑时由 ytdlp() 记下
    a.music_info = {}     # 视频 ID → YouTube Music 上的曲名、歌手、专辑、曲序：music_targets() 查到的
    a.mv_jobs = []        # [(歌手, [(MV 视频 ID, 标题)])]：给的是歌手页时由 music_targets() 记下，或从列表读
    a.mv_artist = ""      # 正在下谁的 MV（download_mvs 设）
    a.group = ""          # MV 按艺人分的子文件夹（download_videos 设）
    a.video_extra = {}    # 视频 ID → 写进文件的模式字段与标准标签（download_videos 记下）
    if a.scheme == "bilibili":
        a.mode = "video"
    if a.mode in ("girls", "girlvideo") and a.max_height == 1080:   # 女团的要高画质：有 4K 就要
        a.max_height = 2160
    a.meta_records = []   # 批次的下载元数据：音乐每首、排名表；最后连同其他文件旁边的 .json 合成一个文件
    a.music_skipped = []  # 不下的歌及原因、已下载过的：读取列表时列出来
    a.music_done = []

    if a.scheme == "music":   # 音乐只从 YouTube / YouTube Music 来（其他网站的多是搬运）
        dropped = [u for u in a.urls if platform_of(u)["id"] != "youtube"]
        for u in dropped:
            log(f"音乐只从 YouTube / YouTube Music 下，跳过：{u}")
        a.urls = [u for u in a.urls if u not in dropped]
        if not a.urls:
            print("OMNIHUB_RESULT " + json.dumps({"files": 0, "skipped": 0, "errors": len(dropped), "path": a.batch, "hints": []}, ensure_ascii=False), flush=True)
            sys.exit(1)
    if a.plan_out and a.scheme in SOCIAL:
        write_social_plan(a, a.scheme)
        print("OMNIHUB_RESULT " + json.dumps({"plan": a.plan_out}, ensure_ascii=False), flush=True)
        return
    if a.plan_out and a.scheme in ("youtube", "bilibili"):
        write_video_plan(a)
        print("OMNIHUB_RESULT " + json.dumps({"plan": a.plan_out}, ensure_ascii=False), flush=True)
        return
    if a.scheme == "music" and a.plan_out:
        write_plan(a)
        print("OMNIHUB_RESULT " + json.dumps({"plan": a.plan_out}, ensure_ascii=False), flush=True)
        return

    if a.scheme in SOCIAL:
        files, skipped, errors = gallerydl_plan(a, a.scheme) if a.plan else gallerydl(a.urls, a, a.scheme)
    elif a.scheme == "other":
        files, skipped, errors = [], 0, 0
        groups = {}
        for u in a.urls:
            groups.setdefault(platform_of(u)["id"] or urlparse(u).hostname or "", []).append(u)
        for urls in groups.values():
            f, s, e = ytdlp(urls, a, platform_of(urls[0]))
            files, skipped, errors = files + f, skipped + s, errors + e
    elif a.scheme in ("youtube", "bilibili"):   # 按读取好的列表下（去掉没勾的）；没给列表时先读、按默认勾选下
        if a.plan:
            with open(a.plan, encoding="utf-8") as f:
                plan = json.load(f)
            skip = set(filter(None, a.exclude.split(",")))
        else:
            plan = list_videos(a)
            skip = {v["id"] for v in plan["videos"] if not v["picked"]}
        a.mode = plan.get("mode") or a.mode
        if a.mode in ("girls", "girlvideo") and a.max_height == 1080:
            a.max_height = 2160
        platform = next((p for p in PLATFORMS if p["id"] == SCHEME_PLATFORM[a.scheme]), {"id": a.scheme, "dir": a.scheme})
        files, skipped, errors = download_videos(a, plan, skip, platform)
    elif a.scheme == "music" and a.plan:
        songs, other = load_plan(a)
        youtube = next((p for p in PLATFORMS if p["id"] == "youtube"), platform_of(songs[0]) if songs else {"id": "youtube", "dir": "YouTube"})
        files, skipped, errors = ytdlp(songs, a, youtube) if songs else ([], 0, 0)
        for u in other:
            f, s, e = ytdlp([u], a, platform_of(u))
            files, skipped, errors = files + f, skipped + s, errors + e
    elif a.rank != "none":
        files, skipped, errors = ranked(a)
    else:
        # 按第一个链接认平台（决定用哪份 Cookie；音乐只会是 YouTube）
        platform = platform_of(a.urls[0]) if a.scheme == "music" else next(
            (p for p in PLATFORMS if p["id"] == SCHEME_PLATFORM[a.scheme]), platform_of(a.urls[0]))
        files, skipped, errors = ytdlp(a.urls, a, platform)
    if a.scheme == "music":
        a.meta_records += enrich_music(files, a.lastfm_known, a.batch, a.album_of, a.music_info) or []
        if a.mv_jobs:
            f, s, e = download_mvs(a, next((p for p in PLATFORMS if p["id"] == "youtube"), platform_of(a.urls[0])))
            files, skipped, errors = files + f, skipped + s, errors + e
    write_meta(a.batch, a.meta_records + collect_sidecars(a.batch, a.video_extra))
    progress(len(a.urls), len(a.urls), f"完成：新下载 {len(files)} 个文件，跳过已下载 {skipped} 个，出错 {errors} 个")
    for h in sorted(hints):
        log("提示：" + h)
    print("OMNIHUB_RESULT " + json.dumps({"files": len(files), "skipped": skipped, "errors": errors, "path": a.batch, "hints": sorted(hints)}, ensure_ascii=False), flush=True)
    if not files and errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
