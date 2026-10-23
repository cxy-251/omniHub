#!/usr/bin/env python3
"""读取列表：频道、播放列表、歌单、合集里有哪些条目（只读元数据，不下载）。由 omniHub 服务按需调用。

用 yt-dlp 的「平铺」模式，一次请求拿到列表里每一条的标题、时长、播放量等，不逐个打开视频页。
YouTube 频道地址（/@名字、/channel/…）没写分页时取「视频」页。
登录 Cookie 与代理同下载：OMNIHUB_COOKIES_FILE、OMNIHUB_YTDLP_PROXY。
输出一行 OMNIHUB_PROBE <json>：{title, uploader, url, entries:[{id, url, title, duration, views, likes, uploader, date}]}。
"""
import argparse
import json
import os
import re
import sys

from yt_dlp import YoutubeDL


class Quiet:
    def debug(self, msg):
        pass

    def info(self, msg):
        pass

    def warning(self, msg):
        print("警告：" + str(msg), file=sys.stderr)

    def error(self, msg):
        print("错误：" + str(msg), file=sys.stderr)


def normalize(url):
    # 频道首页是「视频 / Shorts / 直播」几个分页的列表；没指定分页时取视频页
    if re.search(r"youtube\.com/(@[^/?#]+|channel/[^/?#]+|c/[^/?#]+|user/[^/?#]+)/?(\?.*)?$", url):
        return re.sub(r"/?(\?.*)?$", r"/videos\1", url, count=1)
    return url


def entry(e):
    url = e.get("url") or e.get("webpage_url") or ""
    if url and not url.startswith("http") and e.get("ie_key") == "Youtube":
        url = "https://www.youtube.com/watch?v=" + url
    date = e.get("upload_date") or ""
    if not date and e.get("timestamp"):
        import datetime
        date = datetime.datetime.utcfromtimestamp(e["timestamp"]).strftime("%Y%m%d")
    return {
        "id": str(e.get("id") or ""),
        "url": url,
        "title": e.get("title") or "",
        "duration": e.get("duration"),
        "views": e.get("view_count"),
        "likes": e.get("like_count"),
        "uploader": e.get("channel") or e.get("uploader") or "",
        "date": date,
    }


def main():
    p = argparse.ArgumentParser()
    p.add_argument("url")
    p.add_argument("--limit", type=int, default=1000)
    a = p.parse_args()
    opts = {"extract_flat": "in_playlist", "skip_download": True, "quiet": True, "no_warnings": True,
            "logger": Quiet(), "playlistend": a.limit, "ignoreerrors": True}
    if os.environ.get("OMNIHUB_COOKIES_FILE"):
        opts["cookiefile"] = os.environ["OMNIHUB_COOKIES_FILE"]
    if os.environ.get("OMNIHUB_YTDLP_PROXY"):
        opts["proxy"] = os.environ["OMNIHUB_YTDLP_PROXY"]
    url = normalize(a.url)
    with YoutubeDL(opts) as ydl:
        info = ydl.extract_info(url, download=False)
    if not info:
        raise SystemExit("读取失败（链接无效、需要登录或网络不通）")
    if info.get("_type") in ("playlist", "multi_video"):
        entries = [entry(e) for e in info.get("entries") or [] if e]
    else:
        entries = [entry(info)]
    entries = [e for e in entries if e["url"]]
    out = {"title": info.get("title") or "", "uploader": info.get("channel") or info.get("uploader") or "",
           "url": url, "entries": entries}
    print("OMNIHUB_PROBE " + json.dumps(out, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
