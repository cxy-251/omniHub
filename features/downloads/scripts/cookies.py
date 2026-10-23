#!/usr/bin/env python3
"""从本机 Chrome 读出某个平台的 Cookie，存成 Netscape 格式的 cookies.txt（yt-dlp、gallery-dl 都认）。由 omniHub 服务按需调用。

用 yt-dlp 自带的浏览器 Cookie 读取（处理 Chrome 的加密；--password-store=basic 时不需要系统密钥环）。
只保留 --domain 给出的域名（及其子域名）的 Cookie；输出文件权限 600。
标准输出只打印一行 OMNIHUB_RESULT <json>：数量、登录用 Cookie 是否存在、最早过期时间——不打印 Cookie 的值。
"""
import argparse
import json
import os
import sys
import time

from yt_dlp.cookies import YoutubeDLCookieJar, extract_cookies_from_browser


class QuietLogger:
    def debug(self, msg):
        pass

    def info(self, msg):
        pass

    def warning(self, msg, only_once=False):
        print("警告：" + str(msg), file=sys.stderr)

    def error(self, msg):
        print("错误：" + str(msg), file=sys.stderr)


def matches(domain, wanted):
    d = domain.lstrip(".").lower()
    return any(d == w or d.endswith("." + w) for w in wanted)


def main():
    p = argparse.ArgumentParser(description="从 Chrome 导出某个平台的 Cookie")
    p.add_argument("--profile", required=True, help="Chrome 用户数据目录（含 Default/Cookies）")
    p.add_argument("--domain", action="append", required=True, help="要保留的域名，如 youtube.com；可重复")
    p.add_argument("--login", default="", help="表示已登录的 Cookie 名，逗号分隔")
    p.add_argument("--out", required=True)
    a = p.parse_args()

    wanted = [d.lower().lstrip(".") for d in a.domain]
    login_names = {n for n in a.login.split(",") if n}
    jar = extract_cookies_from_browser("chrome", a.profile, QuietLogger())
    out = YoutubeDLCookieJar(a.out)
    count, login_found, login_expiry = 0, False, None
    now = time.time()
    for c in jar:
        if not matches(c.domain, wanted):
            continue
        if c.expires and c.expires < now:
            continue
        out.set_cookie(c)
        count += 1
        if c.name in login_names:
            login_found = True
            if c.expires:
                login_expiry = c.expires if login_expiry is None else min(login_expiry, c.expires)
    tmp = a.out + ".part"
    out.filename = tmp
    out.save(ignore_discard=True, ignore_expires=True)
    os.chmod(tmp, 0o600)
    os.replace(tmp, a.out)
    print("OMNIHUB_RESULT " + json.dumps({"count": count, "logged_in": login_found, "expires": login_expiry}), flush=True)


if __name__ == "__main__":
    main()
