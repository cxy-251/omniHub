#!/usr/bin/env python3
"""JM 漫画下载：按本子 ID 整本下载并封成 CBZ（封面、metadata.json、ComicInfo.xml、按章节分目录的原图）。
由 omniHub 服务按需调用；完成后服务端更新漫画索引。

已有同 ID 的 CBZ 且章节数不少于线上时跳过；线上有新章节时整本重下，新包完成后旧包移入系统回收站（gio trash）。
下载中间文件放在 --work 下（本次任务自己的临时目录），成功封箱后删除；失败时保留，下次接着下。
移植自 omni-deck omni/features/manga/service.py 的下载与封箱部分。
"""
import argparse
import html
import json
import os
import re
import shutil
import subprocess
import sys
import time
import zipfile

import jmcomic

IMAGE_EXTS = (".jpg", ".jpeg", ".png", ".webp", ".gif")
API_DOMAINS = ["www.cdngwc.club", "www.cdngwc.net", "www.cdngwc.cc", "www.cdnhjk.net"]


def log(msg):
    print(msg, flush=True)


def progress(done, total, text):
    print(f"OMNIHUB_PROGRESS {done} {total} {text}", flush=True)


def result(**fields):
    print("OMNIHUB_RESULT " + json.dumps(fields, ensure_ascii=False), flush=True)


def sanitize(name, max_bytes=180):
    """去掉路径非法字符，按 UTF-8 字节截断（ext4 文件名上限 255 字节）。"""
    clean = re.sub(r'[\/\\:\*\?"<>\|\r\n\t]', "_", str(name or "")).strip()
    data = clean.encode("utf-8")
    if len(data) > max_bytes:
        clean = data[:max_bytes].decode("utf-8", errors="ignore").rstrip("_ .")
    return clean or "unnamed"


def chapter_dirname(photo):
    """章节目录：4 位序号 + 章节名，避免重名与过长。"""
    idx = getattr(photo, "index", 1) or 1
    name = sanitize(getattr(photo, "name", "") or "", max_bytes=50)
    return f"{idx:04d}_{name}" if name and name != "unnamed" else f"{idx:04d}"


def natural_key(s):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r"(\d+)", s)]


def setup():
    jmcomic.disable_jm_log()
    jmcomic.JmModuleConfig.FLAG_API_CLIENT_AUTO_UPDATE_DOMAIN = False
    jmcomic.JmModuleConfig.DOMAIN_API_LIST = API_DOMAINS
    jmcomic.JmModuleConfig.PFIELD_ADVICE["name"] = chapter_dirname
    jmcomic.JmModuleConfig.VAR_FILE_NAME_LENGTH_LIMIT = 50


def option_for(base_dir):
    opt = jmcomic.JmOption.default()
    opt.dir_rule = jmcomic.DirRule("Bd_Pname", base_dir=base_dir)
    opt.download_image_decode = True
    return opt


def cbz_metadata(path):
    try:
        with zipfile.ZipFile(path) as zf:
            if "metadata.json" in zf.namelist():
                return json.loads(zf.read("metadata.json").decode("utf-8"))
    except (OSError, zipfile.BadZipFile, ValueError):
        pass
    return {}


def find_existing(dest, aid):
    """按 metadata.json 里的 id 找已有的包（文件名可能和标题不同）。"""
    for name in os.listdir(dest):
        if name.lower().endswith(".cbz"):
            path = os.path.join(dest, name)
            if str(cbz_metadata(path).get("id", "")) == str(aid):
                return path
    return None


def comic_info(meta):
    e = lambda v: html.escape(str(v or ""))
    tags = ",".join(meta.get("tags") or [])
    return (
        '<?xml version="1.0" encoding="utf-8"?>\n<ComicInfo>\n'
        f"  <Title>{e(meta['title'])}</Title>\n  <Writer>{e(meta['author'])}</Writer>\n"
        f"  <Summary>{e(meta['description'])}</Summary>\n  <Genre>{e(tags)}</Genre>\n  <Tags>{e(tags)}</Tags>\n"
        f"  <Web>https://18comic.vip/album/{e(meta['id'])}</Web>\n</ComicInfo>\n"
    )


def pack(work, target, meta):
    """原图不重新压缩（ZIP_STORED），先写 .part 再原子替换。"""
    part = target + ".part"
    chapters = sorted((d for d in os.listdir(work) if os.path.isdir(os.path.join(work, d))), key=natural_key)
    with zipfile.ZipFile(part, "w", compression=zipfile.ZIP_STORED) as zf:
        cover = os.path.join(work, "cover.jpg")
        if os.path.isfile(cover):
            zf.write(cover, "cover.jpg")
        zf.writestr("metadata.json", json.dumps(meta, ensure_ascii=False, indent=2))
        zf.writestr("ComicInfo.xml", comic_info(meta))
        pages = 0
        for ch in chapters:
            for f in sorted(os.listdir(os.path.join(work, ch)), key=natural_key):
                if f.lower().endswith(IMAGE_EXTS):
                    zf.write(os.path.join(work, ch, f), f"{ch}/{f}")
                    pages += 1
    if pages == 0:
        os.remove(part)
        raise RuntimeError("没有下载到任何图片")
    return part, pages


def trash(path):
    if subprocess.run(["gio", "trash", "--", path]).returncode != 0:
        raise RuntimeError(f"旧包移入回收站失败，保留新包为 .part：{path}")


def download_album(client, aid, dest, work_root):
    detail = client.get_album_detail(aid)
    title = detail.name
    photos = list(detail)
    online = len(photos)
    log(f"《{title}》共 {online} 章")
    existing = find_existing(dest, aid)
    if existing:
        meta = cbz_metadata(existing)
        if int(meta.get("local_chapters") or 0) >= online:
            log(f"已是最新（{meta.get('local_chapters')}/{online} 章），跳过：{os.path.basename(existing)}")
            return True
        log(f"本地 {meta.get('local_chapters')} 章，线上 {online} 章，重新下载整本")
    target = existing or os.path.join(dest, sanitize(title) + ".cbz")
    if not existing and os.path.exists(target):
        target = os.path.join(dest, f"{sanitize(title, 160)} [{aid}].cbz")

    work = os.path.join(work_root, str(aid))
    os.makedirs(work, exist_ok=True)
    opt = option_for(work)
    failed = []
    for i, photo in enumerate(photos, 1):
        log(f"[{i}/{online}] {chapter_dirname(photo)}")
        progress(i - 1, online, f"《{title}》第 {i}/{online} 章")
        for attempt in range(1, 4):
            try:
                jmcomic.download_photo(photo.photo_id, option=opt)
                break
            except Exception as e:  # jmcomic 的网络错误类型很多，统一重试
                log(f"    重试 {attempt}/3：{e}")
                time.sleep(3 * attempt)
        else:
            failed.append(chapter_dirname(photo))
    if failed:
        log(f"有 {len(failed)} 章下载失败（{', '.join(failed[:5])}），保留已下载部分，下次重试")
        return False
    try:
        client.download_album_cover(aid, os.path.join(work, "cover.jpg"))
    except Exception as e:
        log(f"封面下载失败：{e}")

    meta = {
        "id": str(aid), "title": title,
        "author": ", ".join(detail.authors or []), "description": detail.description or "",
        "tags": list(detail.tags or []), "online_chapters": online, "local_chapters": online,
        "is_complete": True, "verified": True, "updated_at": time.time(),
    }
    part, pages = pack(work, target, meta)
    if os.path.exists(target):
        trash(target)
    os.replace(part, target)
    shutil.rmtree(work, ignore_errors=True)
    log(f"完成：{os.path.basename(target)}（{online} 章，{pages} 页）")
    download_album.saved.append(target)
    return True


download_album.saved = []


def parse_ids(specs):
    """接受纯数字 ID 或网址（取 /album/<数字> 或 /photo/<数字> 里的数字），空格、逗号、换行分隔。"""
    out = []
    for token in (t for spec in specs for t in re.split(r"[,\s]+", spec) if t):
        m = re.search(r"/(?:album|photo)/(\d+)", token) or re.fullmatch(r"(?:jm)?(\d+)", token, re.I)
        if not m:
            raise SystemExit(f"认不出本子 ID：{token}")
        if m.group(1) not in out:
            out.append(m.group(1))
    return out


def main():
    p = argparse.ArgumentParser(description="JM 漫画下载并封成 CBZ")
    p.add_argument("ids", nargs="+", help="本子 ID 或网址，可用空格、逗号或换行分隔")
    p.add_argument("--dest", required=True, help="漫画分类目录")
    p.add_argument("--work", required=True, help="下载中间文件目录")
    args = p.parse_args()
    ids = parse_ids(args.ids)
    setup()
    client = option_for(args.work).new_jm_client()
    ok = 0
    for n, aid in enumerate(ids, 1):
        log(f"\n=== {n}/{len(ids)} 本子 {aid} ===")
        try:
            ok += bool(download_album(client, aid, args.dest, args.work))
        except Exception as e:
            log(f"失败：{e}")
    log(f"\n{ok}/{len(ids)} 本完成")
    result(path=args.dest, saved=[os.path.basename(x) for x in download_album.saved], ok=ok == len(ids))
    sys.exit(0 if ok == len(ids) else 1)


if __name__ == "__main__":
    main()
