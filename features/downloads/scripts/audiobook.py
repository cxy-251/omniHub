#!/usr/bin/env python3
"""视频站音频 → 有声书（YouTube / Bilibili / 其它 yt-dlp 支持的站点）。由 omniHub 服务按需调用。

一个 URL 可以是单个视频、播放列表或 B 站多 P 视频，统一展开成条目列表再下载：
  --mode files  每个条目存成一个 m4a（默认）
  --mode m4b    全部条目合成一个带章节的 m4b（单个视频自带章节时用视频章节）

URL 可写成「URL|文件名」指定保存名。--root 是音声分类目录（如 media_library/audio/standard），专辑目录建在它下面；
不给 --album 时用播放列表或第一个视频的标题当专辑名。--existing-root 给出其它资源库的同一分类目录，
那里同一专辑已有同名文件（且不小于 --min-mb）就跳过。
进度与结果以 OMNIHUB_PROGRESS <完成> <总数> <说明>、OMNIHUB_RESULT <json> 行输出，供界面显示。
代理与 Cookie 从环境变量 OMNIHUB_YTDLP_PROXY、OMNIHUB_YTDLP_COOKIES（cookies.txt 路径）读取，不出现在命令行与日志里。
移植自 omni-deck tools/crawlers/media_fetch.py。
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

AUDIO_EXTS = (".m4a", ".m4b", ".mp3", ".opus", ".aac")


def log(msg):
    print(msg, flush=True)


def progress(done, total, text):
    print(f"OMNIHUB_PROGRESS {done} {total} {text}", flush=True)


def result(**fields):
    print("OMNIHUB_RESULT " + json.dumps(fields, ensure_ascii=False), flush=True)


def ytdlp():
    return [sys.executable, "-m", "yt_dlp"]


def auth_args():
    args = []
    cookies = os.environ.get("OMNIHUB_YTDLP_COOKIES", "")
    if cookies:
        if not os.path.isfile(cookies):
            raise SystemExit(f"Cookie 文件不存在：{cookies}")
        args += ["--cookies", cookies]
    proxy = os.environ.get("OMNIHUB_YTDLP_PROXY", "")
    if proxy:
        args += ["--proxy", proxy]
    return args


def safe_name(name, max_len=120):
    name = re.sub(r'[\\/:*?"<>|\r\n\t]', "_", str(name)).strip(" .")
    return (name or "未命名")[:max_len]


def parse_ranges(text):
    out = set()
    for part in filter(None, (p.strip() for p in text.split(","))):
        a, _, b = part.partition("-")
        out.update(range(int(a), int(b or a) + 1))
    return out


def is_single_video(url):
    u = url.lower()
    return ("youtube.com/watch" in u or "youtu.be/" in u) and "list=" not in u


def expand(url, auth, items):
    """URL -> [(条目URL, 标题, 序号)]；播放列表/多P按 --items 过滤。"""
    res = subprocess.run(ytdlp() + ["--flat-playlist", "-J", "--socket-timeout", "60"] + auth + [url],
                         capture_output=True, text=True)
    if res.returncode != 0:
        raise RuntimeError(res.stderr.strip().splitlines()[-1] if res.stderr.strip() else "yt-dlp 解析失败")
    info = json.loads(res.stdout)
    expand.last_title = info.get("title") or ""
    if info.get("_type") != "playlist":
        return [(url, info.get("title") or url, 1)]
    out = []
    for idx, e in enumerate(info.get("entries") or [], 1):
        if items and idx not in items:
            continue
        e_url = e.get("url") or e.get("webpage_url") or e.get("id")
        if e_url and not e_url.startswith("http") and "youtube" in (info.get("extractor") or "").lower():
            e_url = "https://www.youtube.com/watch?v=" + e_url
        out.append((e_url, e.get("title") or f"{info.get('title', '')} P{idx}", idx))
    return out


def download(entry_url, out_base, fmt, auth, retries=3):
    """下载单个条目的音频到 out_base.<ext>，返回实际文件路径。"""
    cmd = ytdlp() + ["-f", fmt, "-x", "--audio-format", "m4a", "--no-playlist", "--continue",
                     "--retries", "10", "--fragment-retries", "10", "--socket-timeout", "60",
                     "--newline", "--progress-template", "download:    %(progress._percent_str)s %(progress._speed_str)s 剩余 %(progress._eta_str)s",
                     "-o", out_base + ".%(ext)s"] + auth + [entry_url]
    for attempt in range(1, retries + 1):
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        last_progress = 0.0
        tail = []
        for line in proc.stdout:
            line = line.rstrip()
            if line.startswith("    ") and "%" in line:
                # 进度行最多每 5 秒打印一次，日志不刷屏。
                if time.time() - last_progress >= 5:
                    log(line)
                    last_progress = time.time()
            elif line:
                tail = (tail + [line])[-5:]
        proc.wait()
        for ext in AUDIO_EXTS:
            if proc.returncode == 0 and os.path.exists(out_base + ext):
                return out_base + ext
        err = tail[-1] if tail else "未知错误"
        log(f"    重试 {attempt}/{retries}：{err}")
        if any("Sign in to confirm" in t for t in tail):
            log("    提示：YouTube 要求登录，请在 omniHub 配置里设置 cookies.txt")
        time.sleep(3)
    raise RuntimeError(f"下载失败：{entry_url}")


def probe_duration(path):
    out = subprocess.check_output(["ffprobe", "-v", "error", "-show_entries", "format=duration",
                                   "-of", "default=noprint_wrappers=1:nokey=1", path], text=True)
    return float(out.strip() or 0)


def video_chapters(entry_url, auth):
    res = subprocess.run(ytdlp() + ["-J", "--no-playlist", "--socket-timeout", "60"] + auth + [entry_url],
                         capture_output=True, text=True)
    try:
        return json.loads(res.stdout).get("chapters") or []
    except ValueError:
        return []


def existing(args, filename):
    for d in [args.dest] + [os.path.join(r, args.album) for r in args.existing_root]:
        p = os.path.join(d, filename)
        if os.path.isfile(p) and os.path.getsize(p) >= args.min_mb * 1048576:
            return p
    return None


def run_files(entries, args, auth):
    ok, failed = 0, []
    for n, (url, title, idx) in enumerate(entries, 1):
        name = safe_name(args.names.get(url) or args.name.format(title=title, index=idx, album=args.album))
        if any(existing(args, name + ext) for ext in AUDIO_EXTS):
            log(f"[{n}/{len(entries)}] 已存在，跳过：{name}")
            progress(n, len(entries), "已存在：" + name)
            ok += 1
            continue
        log(f"[{n}/{len(entries)}] 下载 {name}")
        progress(n - 1, len(entries), "下载：" + name)
        t0 = time.time()
        try:
            path = download(url, os.path.join(args.dest, name), args.format, auth)
            log(f"    完成 {os.path.getsize(path) / 1048576:.1f} MB，用时 {time.time() - t0:.0f} 秒")
            ok += 1
        except RuntimeError as e:
            log(f"    失败：{e}")
            failed.append(name)
    log(f"\n{ok}/{len(entries)} 成功" + (f"；失败：{', '.join(failed)}" if failed else ""))
    return not failed


def run_m4b(entries, args, auth):
    out_name = safe_name(args.output or f"{args.album}.m4b")
    if not out_name.endswith(".m4b"):
        out_name += ".m4b"
    if existing(args, out_name):
        log(f"已存在，跳过：{out_name}")
        return True
    if not shutil.which("ffmpeg"):
        raise SystemExit("合成 m4b 需要 ffmpeg")
    with tempfile.TemporaryDirectory(prefix=".omnihub-m4b-", dir=args.dest) as tmp:
        parts = []
        for n, (url, title, _idx) in enumerate(entries, 1):
            log(f"[{n}/{len(entries)}] 下载 {title}")
            progress(n - 1, len(entries), "下载：" + title)
            path = download(url, os.path.join(tmp, f"part_{n:03d}"), args.format, auth)
            parts.append((title, path, probe_duration(path)))
        chapters, t = [], 0.0
        if len(parts) == 1:
            for i, ch in enumerate(video_chapters(entries[0][0], auth), 1):
                st, et = float(ch.get("start_time", 0)), float(ch.get("end_time", parts[0][2]))
                chapters.append({"title": ch.get("title") or f"第{i}回", "start": st, "end": et})
        if not chapters:
            for title, _p, dur in parts:
                chapters.append({"title": title, "start": t, "end": t + dur})
                t += dur
        concat = os.path.join(tmp, "concat.txt")
        with open(concat, "w", encoding="utf-8") as f:
            for _t, p, _d in parts:
                f.write("file '%s'\n" % p.replace("'", "'\\''"))
        meta = os.path.join(tmp, "meta.txt")
        with open(meta, "w", encoding="utf-8") as f:
            f.write(f";FFMETADATA1\ntitle={args.album}\nalbum={args.album}\ngenre=有声书\n\n")
            for ch in chapters:
                f.write(f"[CHAPTER]\nTIMEBASE=1/1000\nSTART={int(ch['start'] * 1000)}\nEND={int(ch['end'] * 1000)}\ntitle={ch['title']}\n\n")
        final = os.path.join(args.dest, out_name)
        partial = os.path.join(tmp, out_name)
        base = ["ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", concat, "-i", meta, "-map_metadata", "1"]
        if subprocess.run(base + ["-c", "copy", "-movflags", "+faststart", partial], capture_output=True).returncode != 0:
            subprocess.run(base + ["-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", partial], check=True, capture_output=True)
        os.replace(partial, final)
    log(f"完成：{final}（{os.path.getsize(final) / 1048576:.1f} MB，{len(chapters)} 个章节）")
    return True


def main():
    p = argparse.ArgumentParser(description="视频站音频 → 有声书")
    p.add_argument("urls", nargs="+", help="视频/播放列表/多P URL；可写成 URL|文件名")
    p.add_argument("--album", default="", help="专辑名；不填时用播放列表或第一个视频的标题")
    p.add_argument("--root", required=True, help="音声分类目录，专辑目录建在它下面")
    p.add_argument("--existing-root", action="append", default=[], help="其它库的同一分类目录（已有文件就跳过）")
    p.add_argument("--mode", choices=["files", "m4b"], default="files")
    p.add_argument("--items", default="", help="只取播放列表/多P 的这些序号，如 1-6,9")
    p.add_argument("--name", default="{title}", help="files 模式的文件名模板：{title} {index} {album}")
    p.add_argument("--output", default=None, help="m4b 模式的输出文件名")
    p.add_argument("--format", default="140/ba[ext=m4a]/ba", help="yt-dlp 格式选择")
    p.add_argument("--min-mb", type=float, default=1, help="已存在文件小于这个大小(MB)视为残缺，重新下载")
    args = p.parse_args()

    auth = auth_args()
    items = parse_ranges(args.items)
    args.names, entries = {}, []
    first_title = ""
    for spec in args.urls:
        url, _, name = spec.partition("|")
        url, name = url.strip(), name.strip()
        if name and is_single_video(url):
            entries.append((url, name, 1))
            args.names[url] = name
            continue
        try:
            expanded = expand(url, auth, items)
        except RuntimeError as e:
            log(f"解析失败 {url}：{e}")
            continue
        if name and len(expanded) == 1:
            args.names[expanded[0][0]] = name
        first_title = first_title or getattr(expand, "last_title", "")
        entries += expanded
    if not entries:
        raise SystemExit("没有可下载的条目（链接解析失败，见上面的日志）")
    args.album = safe_name(args.album or first_title or entries[0][1] or "未命名专辑", 80)
    args.dest = os.path.join(args.root, args.album)
    os.makedirs(args.dest, exist_ok=True)
    log(f"《{args.album}》共 {len(entries)} 个条目 → {args.dest}")
    progress(0, len(entries), "开始下载")
    ok = run_m4b(entries, args, auth) if args.mode == "m4b" else run_files(entries, args, auth)
    progress(len(entries), len(entries), "完成" if ok else "部分失败")
    result(album=args.album, path=args.dest, ok=ok)
    raise SystemExit(0 if ok else 1)


if __name__ == "__main__":
    main()
