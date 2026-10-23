#!/usr/bin/env python3
"""补充数据：逐个读视频的完整信息（不下载），列表里「自己勾选」时用。由 omniHub 服务按需调用。

每个链接输出一行 OMNIHUB_DETAIL <json>：{url, views, likes, comments, artist, track, album, year, date, duration}；
有 Last.fm key（OMNIHUB_LASTFM_KEY）且 --music 时再查重播率：{listeners, playcount, replay, lastfm_album, tags, lastfm_name}。
登录 Cookie 与代理：OMNIHUB_COOKIES_FILE、OMNIHUB_YTDLP_PROXY。
"""
import argparse
import json
import os
from concurrent.futures import ThreadPoolExecutor

import rank


def main():
    p = argparse.ArgumentParser()
    p.add_argument("urls", nargs="+")
    p.add_argument("--music", action="store_true", help="同时查 Last.fm（艺人、曲名、重播率、标签）")
    a = p.parse_args()
    cookies = os.environ.get("OMNIHUB_COOKIES_FILE", "")
    key = os.environ.get("OMNIHUB_LASTFM_KEY", "")
    # 4 个一起读（页面信息很小，主要是等网络），读完一个输出一个
    with ThreadPoolExecutor(max_workers=4) as pool:
        for line in pool.map(lambda u: one(u, a, cookies, key), a.urls):
            print("OMNIHUB_DETAIL " + json.dumps(line, ensure_ascii=False), flush=True)


def one(url, a, cookies, key):
    out = {"url": url}
    try:
        d = rank.details(url, cookies)
        out.update(d)
        if a.music and key:
            artist, track = rank.guess_artist_track(d)
            info = rank.lastfm_info(artist, track, key) if artist and track else None
            out["lastfm_query"] = f"{artist} - {track}"
            if info:
                out.update(info)
                if info["listeners"] >= 50:
                    out["replay"] = info["playcount"] / info["listeners"]
    except Exception as e:
        out["error"] = str(e)[:200]
    return out


if __name__ == "__main__":
    main()
