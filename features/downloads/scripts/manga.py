#!/usr/bin/env python3
"""JM 漫画下载：按本子 ID 整本下载并封成 CBZ（封面、metadata.json、ComicInfo.xml、按章节分目录的原图）。
由 omniHub 服务按需调用；完成后服务端更新漫画索引。

已有同 ID 的 CBZ 复用已保存的章节，只下载缺少的章节；新包完成后旧包移入系统回收站（gio trash）。
下载中间文件放在 --work 下（本次任务自己的临时目录），成功封箱后删除；失败时保留，下次接着下。
移植自 omni-deck omni/features/manga/service.py 的下载与封箱部分。
"""
import argparse
import contextlib
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


def existing_chapters(path, photos):
    """按章节 ID 复用新包；旧包只复用目录名完全匹配的章节，不猜测编号。"""
    if not path:
        return {}
    meta = cbz_metadata(path)
    manifest = meta.get("chapters")
    known = {}
    if manifest is not None:
        if not isinstance(manifest, list):
            raise RuntimeError("旧包章节记录损坏，保留原包")
        for chapter in manifest:
            if not isinstance(chapter, dict) or not chapter.get("id") or not chapter.get("directory"):
                raise RuntimeError("旧包章节记录不完整，保留原包")
            key = str(chapter["id"])
            if key in known:
                raise RuntimeError("旧包章节编号重复，保留原包")
            known[key] = chapter
    groups = {}
    with zipfile.ZipFile(path) as archive:
        for info in archive.infolist():
            if info.is_dir() or not info.filename.lower().endswith(IMAGE_EXTS) or info.filename.lower() in ("cover.jpg", "cover.webp", "cover.png"):
                continue
            parts = info.filename.split("/")
            if len(parts) != 2 or any(p in ("", ".", "..") or "\\" in p for p in parts):
                raise RuntimeError("旧包图片目录不符合章节格式，保留原包")
            groups.setdefault(parts[0], []).append(info.filename)
    reusable, claimed = {}, set()
    for photo in photos:
        key = str(photo.photo_id)
        chapter = known.get(key) if manifest is not None else {"directory": chapter_dirname(photo)}
        if not chapter:
            continue
        directory = chapter["directory"]
        files = groups.get(directory, [])
        if directory in claimed:
            raise RuntimeError("旧包章节目录重复，保留原包")
        claimed.add(directory)
        if files and len(files) == len(set(files)) and chapter.get("pages", len(files)) == len(files):
            reusable[key] = sorted(files, key=natural_key)
    if groups.keys() - claimed:
        raise RuntimeError("线上章节与旧包不一致，无法安全增量更新，已保留原包")
    return reusable


def pack(work, target, meta, existing=None, reusable=None):
    """原图不重新压缩（ZIP_STORED），先写 .part 再原子替换。"""
    part = target + ".part"
    reusable = reusable or {}
    with contextlib.ExitStack() as stack:
        old = stack.enter_context(zipfile.ZipFile(existing)) if existing else None
        zf = stack.enter_context(zipfile.ZipFile(part, "w", compression=zipfile.ZIP_STORED))
        cover = os.path.join(work, "cover.jpg")
        if os.path.isfile(cover) and os.path.getsize(cover):
            zf.write(cover, "cover.jpg")
        elif old:
            for name in ("cover.jpg", "cover.webp", "cover.png"):
                if name in old.namelist():
                    zf.writestr(name, old.read(name))
                    break
        pages = 0
        for chapter in meta["chapters"]:
            ch = chapter["directory"]
            count = 0
            if chapter["id"] in reusable:
                for name in reusable[chapter["id"]]:
                    with old.open(name) as source, zf.open(f"{ch}/{name.rsplit('/', 1)[-1]}", "w") as dest:
                        shutil.copyfileobj(source, dest)
                    count += 1
            else:
                for f in sorted(os.listdir(os.path.join(work, ch)), key=natural_key):
                    source = os.path.join(work, ch, f)
                    if f.lower().endswith(IMAGE_EXTS) and os.path.isfile(source) and not os.path.islink(source):
                        zf.write(source, f"{ch}/{f}")
                        count += 1
            if not count:
                raise RuntimeError(f"章节没有图片，保留原包：{ch}")
            chapter["pages"] = count
            pages += count
        if pages == 0:
            raise RuntimeError("没有下载到任何图片")
        zf.writestr("metadata.json", json.dumps(meta, ensure_ascii=False, indent=2))
        zf.writestr("ComicInfo.xml", comic_info(meta))
    return part, pages


def trash(path):
    if subprocess.run(["gio", "trash", "--", path]).returncode != 0:
        raise RuntimeError(f"旧包移入回收站失败，保留新包为 .part：{path}")


def download_album(client, aid, dest, work_root):
    detail = client.get_album_detail(aid)
    title = detail.name
    photos = list(detail)
    online = len(photos)
    book = download_album.book  # (第几本, 共几本)，任务进度按本算，步骤里再写到第几章
    log(f"《{title}》共 {online} 章")
    if not online:
        raise RuntimeError("线上没有可用章节，保留原包")
    existing = find_existing(dest, aid)
    reusable = existing_chapters(existing, photos)
    if existing:
        if len(reusable) == online:
            log(f"已是最新（{online}/{online} 章），跳过：{os.path.basename(existing)}")
            return True
        log(f"复用本地 {len(reusable)} 章，仅下载缺少的 {online - len(reusable)} 章")
    target = existing or os.path.join(dest, sanitize(title) + ".cbz")
    if not existing and os.path.exists(target):
        target = os.path.join(dest, f"{sanitize(title, 160)} [{aid}].cbz")
        if os.path.exists(target):
            raise RuntimeError(f"目标文件已存在且编号不同，保留原文件：{target}")

    work = os.path.join(work_root, str(aid))
    os.makedirs(work, exist_ok=True)
    opt = option_for(work)
    failed = []
    for i, photo in enumerate(photos, 1):
        if str(photo.photo_id) in reusable:
            continue
        log(f"[{i}/{online}] {chapter_dirname(photo)}")
        progress(book[0] - 1, book[1], f"第 {book[0]}/{book[1]} 本《{title}》第 {i}/{online} 章")
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
        **(cbz_metadata(existing) if existing else {}),
        "id": str(aid), "title": title,
        "author": ", ".join(detail.authors or []), "description": detail.description or "",
        "tags": list(detail.tags or []), "online_chapters": online, "local_chapters": online,
        "download_complete": True, "completion_status": "unknown", "verified": True, "updated_at": time.time(),
        "chapters": [{"id": str(photo.photo_id), "directory": chapter_dirname(photo)} for photo in photos],
    }
    meta.pop("is_complete", None)
    part, pages = pack(work, target, meta, existing, reusable)
    if os.path.exists(target):
        trash(target)
    os.replace(part, target)
    shutil.rmtree(work, ignore_errors=True)
    log(f"完成：{os.path.basename(target)}（{online} 章，{pages} 页）")
    download_album.saved.append(target)
    return True


download_album.saved = []
download_album.book = (1, 1)


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


# ---------- 联网查询（服务端按需调用，结果以一行 JSON 输出到 stdout） ----------

def album_brief(aid, info):
    info = info or {}
    tags = info.get("tags") or []
    author = info.get("author") or ""
    if isinstance(author, list):
        author = ", ".join(author)
    return {"id": str(aid), "title": str(info.get("name") or info.get("title") or f"JM{aid}"),
            "author": str(author), "tags": [str(t) for t in tags][:12]}


def page_json(page, number):
    results = [album_brief(aid, info) for aid, info in page.content]
    try:
        page_count = int(page.page_count or 1)
    except Exception:
        page_count = 1
    return {"results": results, "total": int(getattr(page, "total", len(results)) or len(results)),
            "page": number, "page_count": page_count}


def detail_json(client, aid):
    d = client.get_album_detail(aid)
    return {"id": str(aid), "title": d.name, "author": ", ".join(d.authors or []),
            "tags": list(d.tags or []), "description": d.description or "",
            "chapters": [chapter_dirname(p) for p in d]}


def cmd_search(client, args):
    """关键词搜索；只填编号或网址时直接取那本（多个编号逐个取详情）。"""
    q = args.query.strip()
    tokens = [t for t in re.split(r"[\s,，;；]+", q) if t]
    ids = []
    for t in tokens:
        m = re.search(r"/(?:album|photo)/(\d+)", t) or re.fullmatch(r"(?:jm)?(\d+)", t, re.I)
        if m:
            ids.append(m.group(1))
    if tokens and len(ids) == len(tokens):
        results = []
        for aid in dict.fromkeys(ids):
            try:
                d = detail_json(client, aid)
                results.append({"id": d["id"], "title": d["title"], "author": d["author"], "tags": d["tags"][:12], "chapters": len(d["chapters"])})
            except Exception as e:
                results.append({"id": aid, "title": f"JM{aid}", "author": "", "tags": [], "error": str(e)})
        return {"results": results, "total": len(results), "page": 1, "page_count": 1}
    page = client.search_site(search_query=q, page=args.page, order_by=args.order or "mv")
    return page_json(page, args.page)


def cmd_ranking(client, args):
    fn = {"day": client.day_ranking, "month": client.month_ranking}.get(args.type, client.week_ranking)
    return page_json(fn(page=args.page), args.page)


def cmd_covers(client, args):
    """把还没缓存的封面下载到 --dir/<id>.jpg（4 个一起下；单个失败不影响别的）。"""
    from concurrent.futures import ThreadPoolExecutor
    os.makedirs(args.dir, exist_ok=True)

    def one(aid):
        target = os.path.join(args.dir, f"{aid}.jpg")
        part = os.path.join(args.dir, f"{aid}.part.jpg")  # 按扩展名决定图片格式，临时文件也要 .jpg 结尾
        if os.path.exists(target):
            return True
        try:
            client.download_album_cover(aid, part)
            if os.path.getsize(part) < 500:
                raise RuntimeError("封面太小")
            os.replace(part, target)
            return True
        except Exception as e:
            log(f"封面 {aid}：{e}")
            try:
                os.remove(part)
            except OSError:
                pass
            return False

    with ThreadPoolExecutor(4) as pool:
        ok = sum(pool.map(one, [i for i in args.ids if i.isdigit()]))
    return {"ok": ok}


def query_main(argv):
    p = argparse.ArgumentParser(description="JM 联网查询")
    sub = p.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("search"); s.add_argument("--query", required=True); s.add_argument("--page", type=int, default=1); s.add_argument("--order", default="mv")
    r = sub.add_parser("ranking"); r.add_argument("--type", default="week"); r.add_argument("--page", type=int, default=1)
    d = sub.add_parser("detail"); d.add_argument("id")
    c = sub.add_parser("covers"); c.add_argument("--dir", required=True); c.add_argument("ids", nargs="+")
    args = p.parse_args(argv)
    setup()
    client = option_for(os.path.join(os.getcwd(), ".jm-query")).new_jm_client()
    try:
        client.postman.meta_data["timeout"] = 15
    except Exception:
        pass
    try:
        out = {"search": cmd_search, "ranking": cmd_ranking, "covers": cmd_covers,
               "detail": lambda c, a: detail_json(c, a.id)}[args.cmd](client, args)
    except Exception as e:
        out = {"error": f"{type(e).__name__}: {e}"}
    print("OMNIHUB_JSON " + json.dumps(out, ensure_ascii=False), flush=True)


def main():
    if len(sys.argv) > 1 and sys.argv[1] in ("search", "ranking", "detail", "covers"):
        query_main(sys.argv[1:])
        return
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
        download_album.book = (n, len(ids))
        progress(n - 1, len(ids), f"第 {n}/{len(ids)} 本：读取本子信息")
        try:
            ok += bool(download_album(client, aid, args.dest, args.work))
        except Exception as e:
            log(f"失败：{e}")
    progress(len(ids), len(ids), f"{ok}/{len(ids)} 本完成")
    log(f"\n{ok}/{len(ids)} 本完成")
    result(path=args.dest, saved=[os.path.basename(x) for x in download_album.saved], ok=ok == len(ids))
    sys.exit(0 if ok == len(ids) else 1)


if __name__ == "__main__":
    main()
