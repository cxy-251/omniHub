#!/usr/bin/env python3
"""按方案下载到暂存区的一个批次（不直接进资源库）。由 omniHub 服务按需调用。

方案（--scheme）决定工具和去向：
  music     yt-dlp 只取最好的音轨（不转码，嵌封面与标签）→ media_library/audio/music/<专辑>/<序号 标题>
  youtube   yt-dlp 视频 → media_library/shortvideo/YouTube/<频道>/<日期>_<标题>_<id>
  bilibili  yt-dlp 视频 → media_library/shortvideo/B站/<UP主>/…
  x         gallery-dl → 图片 gallery/X/<用户>/（移入资源库时并进相册），视频 media_library/shortvideo/X/<用户>/
  other     按链接域名认平台（平台表见 OMNIHUB_PLATFORMS），视频 → media_library/shortvideo/<平台>/<作者>/
每个文件旁边保存元数据（.info.json / .json），移入资源库时收进 .omni_data/download-meta，留给以后打分。
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


def ytdlp(urls, a, platform):
    """一个 yt-dlp 进程下全部链接；返回 (下完的文件, 已在名单里跳过的数量, 出错的数量)。"""
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
    if a.items:
        cmd += ["--playlist-items", a.items]
    if a.scheme == "music":
        album = safe(a.album) if a.album else "%(playlist_title,album,artist,uploader|未分类)s"
        name = "%(autonumber)02d %(title).100B.%(ext)s" if a.album else "%(playlist_index&{:02d} |)s%(title).100B.%(ext)s"
        cmd += ["-f", "bestaudio/best", "-x", "--audio-format", "best", "--embed-metadata", "--embed-thumbnail",
                "-o", os.path.join(a.batch, "media_library", "audio", "music", album, name)]
    else:
        h = a.max_height
        fmt = "bv*+ba/b" if h <= 0 else f"bv*[height<={h}]+ba/b[height<={h}]/b"
        out = os.path.join(a.batch, "media_library", "shortvideo", platform["dir"],
                           "%(channel,uploader,uploader_id|未知作者)s", "%(upload_date>%Y-%m-%d)s_%(title).80B_%(id)s.%(ext)s")
        cmd += ["-f", fmt, "--merge-output-format", "mp4", "-o", out]
    files, skipped, errors = [], 0, 0
    proc = subprocess.Popen(cmd + urls, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
    for line in proc.stdout:
        line = line.rstrip("\n")
        if line.startswith("OMNIHUB_FILE "):
            files.append(line[13:])
            progress(len(files) + skipped, len(urls), os.path.basename(files[-1]))
            continue
        if "has already been recorded in the archive" in line:
            skipped += 1
            progress(len(files) + skipped, len(urls), "已下载过，跳过")
        elif line.startswith("ERROR:"):
            errors += 1
            for key, hint in LOGIN_HINTS.items():
                if key in line:
                    hints.add(hint.format(platform["dir"]))
        log(line)
    proc.wait()
    return files, skipped, errors


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


def main():
    p = argparse.ArgumentParser(description="按方案下载到暂存区")
    p.add_argument("urls", nargs="+")
    p.add_argument("--batch", required=True, help="暂存区批次目录")
    p.add_argument("--scheme", choices=["music", "youtube", "bilibili", "x", "other"], default="other")
    p.add_argument("--max-height", type=int, default=1080, help="视频最高分辨率（高），0 表示不限")
    p.add_argument("--album", default="", help="音乐：专辑名（不给时用播放列表名）")
    p.add_argument("--items", default="", help="只下播放列表里的这些序号，如 1-10,15")
    p.add_argument("--archive", default="", help="已下载名单文件（下过的不再下）")
    p.add_argument("--x-media", choices=["all", "image", "video"], default="all")
    p.add_argument("--x-limit", type=int, default=0, help="X：每个用户最多下最近这么多个文件，0 表示不限")
    a = p.parse_args()

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
    else:
        # 音乐也可能来自 B 站等：按第一个链接认平台（决定用哪份 Cookie）
        platform = platform_of(a.urls[0]) if a.scheme == "music" else next(
            (p for p in PLATFORMS if p["id"] == SCHEME_PLATFORM[a.scheme]), platform_of(a.urls[0]))
        files, skipped, errors = ytdlp(a.urls, a, platform)
    progress(len(a.urls), len(a.urls), f"完成：新下载 {len(files)} 个文件，跳过已下载 {skipped} 个，出错 {errors} 个")
    for h in sorted(hints):
        log("提示：" + h)
    print("OMNIHUB_RESULT " + json.dumps({"files": len(files), "skipped": skipped, "errors": errors, "path": a.batch, "hints": sorted(hints)}, ensure_ascii=False), flush=True)
    if not files and errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
