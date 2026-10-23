#!/usr/bin/env python3
"""通用下载：把链接下载到暂存区的一个批次里（不直接进资源库）。由 omniHub 服务按需调用。

按链接的域名选工具与登录 Cookie：
  - X（推特）、Pixiv、Instagram 等图站 → gallery-dl，存到 <批次>/gallery/<平台>/…（移入资源库时并进相册）；
  - 其余（YouTube、B 站、抖音……）→ yt-dlp：
      视频 → <批次>/media_library/shortvideo/<平台>/<频道>/<日期>_<标题>_<id>.<扩展名>
      音频 → <批次>/media_library/audio/music/<播放列表或专辑或作者>/<序号 标题>.<扩展名>（取最好的音轨，不转码，嵌入封面与标签）
每个文件旁边保存元数据（yt-dlp 的 .info.json、gallery-dl 的 .json：标题、作者、时长、播放量、点赞、标签……），
移入资源库时它们收进 .omni_data/download-meta，留给以后打分、筛选。
Cookie 从 OMNIHUB_COOKIES_DIR/<平台>.txt 读（「下载 → 账号与 Cookie」从 Chrome 读出的），代理从 OMNIHUB_YTDLP_PROXY 读。
进度与结果以 OMNIHUB_PROGRESS <完成> <总数> <说明>、OMNIHUB_RESULT <json> 行输出。
"""
import argparse
import json
import os
import subprocess
import sys
from urllib.parse import urlparse

# 平台表由服务端经环境变量 OMNIHUB_PLATFORMS 给出（与「账号与 Cookie」页同一份）：
# [{"id","hosts":[域名…],"dir":短视频/相册里的平台目录名,"tool":"yt-dlp"|"gallery-dl"}]
PLATFORMS = json.loads(os.environ.get("OMNIHUB_PLATFORMS", "[]"))


def platform_of(url):
    """返回 (平台 ID, 目录名, 工具)；不认识的站点用域名当目录名、用 yt-dlp。"""
    host = (urlparse(url).hostname or "").lower()
    for p in PLATFORMS:
        if any(host == h or host.endswith("." + h) for h in p["hosts"]):
            return p["id"], p["dir"], p["tool"]
    return "", host.removeprefix("www.") or "其他", "yt-dlp"


def progress(done, total, text):
    print(f"OMNIHUB_PROGRESS {done} {total} {text}", flush=True)


def log(msg):
    print(msg, flush=True)


def cookie_args(pid, flag):
    path = os.path.join(os.environ.get("OMNIHUB_COOKIES_DIR", ""), pid + ".txt") if pid else ""
    return [flag, path] if path and os.path.isfile(path) else []


def run(cmd, marker=None):
    """运行下载工具，日志原样转出；marker 非空时收集以它开头的行（下载完成的文件路径）。"""
    files = []
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
    for line in proc.stdout:
        line = line.rstrip("\n")
        if marker is None:
            # gallery-dl 每下完一个文件打印一行路径（已存在跳过的前面带 #）
            if line and not line.startswith("#") and os.path.isfile(line):
                files.append(line)
            log(line)
        elif line.startswith(marker):
            files.append(line[len(marker):])
        else:
            log(line)
    return proc.wait(), files


def ytdlp(url, label, pid, a):
    proxy = os.environ.get("OMNIHUB_YTDLP_PROXY", "")
    cmd = [sys.executable, "-m", "yt_dlp", "--newline", "--no-colors", "--ignore-errors", "--windows-filenames",
           "--write-info-json", "--no-write-playlist-metafiles", "--no-simulate",
           "--print", "after_move:OMNIHUB_FILE %(filepath)s"] + cookie_args(pid, "--cookies")
    if proxy:
        cmd += ["--proxy", proxy]
    if a.items:
        cmd += ["--playlist-items", a.items]
    if a.mode == "audio":
        out = os.path.join(a.batch, "media_library", "audio", "music",
                           "%(playlist_title,album,artist,uploader|未分类)s", "%(playlist_index&{:02d} |)s%(title).100B.%(ext)s")
        cmd += ["-f", "bestaudio/best", "-x", "--audio-format", "best", "--embed-metadata", "--embed-thumbnail", "-o", out]
    else:
        h = a.max_height
        out = os.path.join(a.batch, "media_library", "shortvideo", label,
                           "%(channel,uploader,uploader_id|未知作者)s", "%(upload_date>%Y-%m-%d)s_%(title).80B_%(id)s.%(ext)s")
        fmt = "bv*+ba/b" if h <= 0 else f"bv*[height<={h}]+ba/b[height<={h}]/b"
        cmd += ["-f", fmt, "--merge-output-format", "mp4", "-o", out]
    return run(cmd + [url], "OMNIHUB_FILE ")


def gallerydl(url, label, pid, a):
    cmd = [sys.executable, "-m", "gallery_dl", "--write-metadata", "-d", os.path.join(a.batch, "gallery", label)]
    cmd += cookie_args(pid, "--cookies")
    proxy = os.environ.get("OMNIHUB_YTDLP_PROXY", "")
    if proxy:
        cmd += ["--proxy", proxy]
    if a.items:
        cmd += ["--range", a.items]
    return run(cmd + [url])


def main():
    p = argparse.ArgumentParser(description="通用下载到暂存区")
    p.add_argument("urls", nargs="+")
    p.add_argument("--batch", required=True, help="暂存区批次目录")
    p.add_argument("--mode", choices=["video", "audio"], default="video", help="yt-dlp 下视频还是只要音频")
    p.add_argument("--max-height", type=int, default=1080, help="视频最高分辨率（高），0 表示不限")
    p.add_argument("--items", default="", help="只下播放列表/合集里的这些序号，如 1-10,15")
    a = p.parse_args()

    total, failed = 0, []
    for i, url in enumerate(a.urls):
        pid, label, tool_name = platform_of(url)
        tool = gallerydl if tool_name == "gallery-dl" else ytdlp
        has_cookie = bool(cookie_args(pid, "-"))
        progress(i, len(a.urls), f"{label} · {'gallery-dl' if tool is gallerydl else 'yt-dlp'}{'（带登录 Cookie）' if has_cookie else ''} · {url}")
        try:
            code, files = tool(url, label, pid, a)
        except OSError as e:
            code, files = 1, []
            log(f"启动失败：{e}")
        total += len(files)
        if code != 0 and not files:
            failed.append(url)
        log(f"== {url}：{len(files)} 个文件" + ("（有出错，见上方日志）" if code != 0 else ""))
    progress(len(a.urls), len(a.urls), f"完成：{total} 个文件")
    print("OMNIHUB_RESULT " + json.dumps({"files": total, "failed": failed, "path": a.batch}, ensure_ascii=False), flush=True)
    if total == 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
