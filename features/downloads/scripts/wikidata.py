"""按 Wikidata 判断艺人是不是女团或女团成员（纯规则，不用 AI）。由 general.py 调用。

不逐个艺人去搜（频道里几百上千个艺人，一个个查要几十分钟）：用两条 SPARQL 一次拿到「名字表」，在本地比对：
  1. 女团（P31 = 女团 Q641066）和它们的成员（成员的 P463「所属团体」或团的 P527「成员」，现在和以前的都算，
     如太妍 → 少女时代），连同英文、韩文、日文、中文的名字和别名
  2. 没标「女团」、但成员全是女性的 K-pop 团体（如 aespa）和它们的成员
名字比对前去掉大小写、空格和标点（Jeong Eun Ji = jeongeunji），名字后面的括号说明另收一份（Karina (aespa) → karina）。
另取一份 K-pop 男团、男团成员和男歌手的名字表：两边都有的名字（BTS 的 Jin、歌手 Crush 和某个女团成员的同名别名）
有歧义，不算女团，留给用户自己勾。
只收有名的：团的维基百科词条语言数（sitelinks）≥ MIN_LINKS；成员看自己和所在团里大的那个。
名字表缓存在脚本目录旁边的 girl-names.json，7 天刷新一次。
比不上的再看是不是已确认女团的小分队（WJSN CHOCOME → WJSN），其余算「其他艺人」（男团、其他歌手或查不到）。
"""
import json
import os
import re
import time
import unicodedata
import urllib.parse
import urllib.request

SPARQL = "https://query.wikidata.org/sparql"
UA = {"User-Agent": "omniHub/0.1 (https://github.com/cxy251/omniHub; personal media library)",
      "Accept": "application/sparql-results+json"}
CACHE = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "girl-names.json")
MAX_AGE = 7 * 24 * 3600
LANGS = 'FILTER(LANG(?name) IN ("mul", "en", "ko", "ja", "zh"))'   # mul：Wikidata 的多语言通用名（很多艺人的英文名在这里）
MIN_LINKS = 12   # 知名度门槛：有多少种语言的维基百科写了词条（TWICE 94、IVE 34、QWER 12；没什么名气的 2～11）
# 每行给出名字、条目和知名度（团：团的词条数；成员：自己和所在团里大的那个，大团的成员个人词条少也算）
QUERIES = [
    # 女团和成员
    """SELECT DISTINCT ?item ?name ?links WHERE {
      { ?item wdt:P31 wd:Q641066 ; wikibase:sitelinks ?links . }
      UNION { ?g wdt:P31 wd:Q641066 ; wikibase:sitelinks ?gl . ?item wdt:P463 ?g ; wikibase:sitelinks ?il .
              BIND(IF(?il > ?gl, ?il, ?gl) AS ?links) }
      UNION { ?g wdt:P31 wd:Q641066 ; wikibase:sitelinks ?gl ; wdt:P527 ?item . ?item wikibase:sitelinks ?il .
              BIND(IF(?il > ?gl, ?il, ?gl) AS ?links) }
      ?item rdfs:label|skos:altLabel ?name . """ + LANGS + "}",
    # 成员全是女性的 K-pop 团体（没标女团的，如 aespa）
    """SELECT DISTINCT ?item ?name ?links WHERE {
      ?item wdt:P31 wd:Q215380 ; wdt:P136 wd:Q213665 ; wdt:P527 ?m ; wikibase:sitelinks ?links .
      ?m wdt:P21 wd:Q6581072 .
      FILTER NOT EXISTS { ?item wdt:P527 ?x . ?x wdt:P21 wd:Q6581097 . }
      ?item rdfs:label|skos:altLabel ?name . """ + LANGS + "}",
    # 上面这些团体的成员（如 aespa 的 Karina）
    """SELECT DISTINCT ?item ?name ?links WHERE {
      ?g wdt:P31 wd:Q215380 ; wdt:P136 wd:Q213665 ; wdt:P527 ?item ; wikibase:sitelinks ?gl .
      ?item wdt:P21 wd:Q6581072 ; wikibase:sitelinks ?il .
      FILTER NOT EXISTS { ?g wdt:P527 ?x . ?x wdt:P21 wd:Q6581097 . }
      BIND(IF(?il > ?gl, ?il, ?gl) AS ?links)
      ?item rdfs:label|skos:altLabel ?name . """ + LANGS + "}",
]
BOY_QUERIES = [   # K-pop 男团、成员和男歌手：用来找出撞名的（全世界的男团太多，查询会超时）
    """SELECT DISTINCT ?item ?name WHERE {
      { ?item wdt:P31 wd:Q216337 ; wdt:P136 wd:Q213665 . }
      UNION { ?g wdt:P31 wd:Q216337 ; wdt:P136 wd:Q213665 . ?item wdt:P463 ?g . }
      UNION { ?g wdt:P31 wd:Q216337 ; wdt:P136 wd:Q213665 . ?g wdt:P527 ?item . }
      ?item rdfs:label|skos:altLabel ?name . """ + LANGS + "}",
    # 男性 K-pop 歌手、成员里有男性的 K-pop 乐队（Crush、DAY6 等，没标「男团」的）
    """SELECT DISTINCT ?item ?name WHERE {
      { ?item wdt:P136 wd:Q213665 ; wdt:P21 wd:Q6581097 . }
      UNION { ?item wdt:P136 wd:Q213665 ; wdt:P527 ?m . ?m wdt:P21 wd:Q6581097 . }
      ?item rdfs:label|skos:altLabel ?name . """ + LANGS + "}",
]


def norm(name):
    """比对用：NFKC（花体字）、去大小写、只留字母数字（含韩文、日文、中文）。"""
    return "".join(ch for ch in unicodedata.normalize("NFKC", name or "").casefold() if ch.isalnum())


def _query(q, log):
    """跑一条 SPARQL；查询服务有时很慢：每次最多等 4 分钟，失败重试两次。"""
    req = urllib.request.Request(SPARQL + "?" + urllib.parse.urlencode({"query": q}), headers=UA)
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=240) as r:
                return json.load(r)["results"]["bindings"]
        except Exception as e:
            if attempt == 2:
                raise
            log(f"   Wikidata 查询没成功（{e}），{10 * (attempt + 1)} 秒后重试")
            time.sleep(10 * (attempt + 1))


def _names(queries, log):
    """{norm 过的名字: [Wikidata 条目 ID, 知名度]}：同一个条目的不同写法（SUZY / 수지）对应同一个 ID；
    同名的几个条目取知名度最高的那个。"""
    out = {}
    for q in queries:
        for b in _query(q, log):
            name, item = b["name"]["value"], b["item"]["value"].rsplit("/", 1)[-1]
            links = int(b["links"]["value"]) if "links" in b else 0
            for key in (norm(name), norm(re.sub(r"\s*[(（][^)）]*[)）]", "", name))):   # Karina (aespa) → karina
                if key and (key not in out or links > out[key][1]):
                    out[key] = [item, links]
        time.sleep(1)
    return out


def name_tables(log=print):
    """(女团与女团成员的名字表 {名字: 条目 ID}, 男团、成员与男歌手的名字集合)，名字都 norm 过。
    优先用缓存；过期或没有时查 Wikidata，查不到时用旧缓存。"""
    cached = None
    try:
        with open(CACHE, encoding="utf-8") as f:
            cached = json.load(f)
        if time.time() - cached.get("time", 0) < MAX_AGE and cached.get("v") == 4:
            return cached["girls"], set(cached["boys"])
    except (OSError, ValueError):
        pass
    try:
        girls, boys = _names(QUERIES, log), _names(BOY_QUERIES, log)
    except Exception as e:
        if cached and cached.get("v") == 4:
            log(f"   Wikidata 查询失败，用上次的名字表（{e}）")
            return cached["girls"], set(cached["boys"])
        log(f"   Wikidata 查询失败，这次认不出女团，全部放进「其他艺人」、默认不勾（{e}）")
        return {}, set()
    try:
        with open(CACHE + ".tmp", "w", encoding="utf-8") as f:
            json.dump({"v": 4, "time": time.time(), "girls": girls, "boys": sorted(boys)}, f, ensure_ascii=False)
        os.replace(CACHE + ".tmp", CACHE)
    except OSError as e:
        log(f"   名字表缓存写不了：{e}")
    log(f"   从 Wikidata 取了名字表：女团和成员 {len(girls)} 个名字，男团和成员 {len(boys)} 个名字")
    return girls, boys


def classify(names, log=print):
    """names → {名字: (类别, 条目)}：类别 "girl"（有名的女团或女团成员，知名度 ≥ MIN_LINKS）| "other"（其他艺人、没什么名气、
    查不到或和男团撞名）；
    条目是 Wikidata ID（同一个艺人的不同写法相同，用来并组），以已确认女团开头的名字
    （WJSN CHOCOME、WJSN LIVE、GFRIEND 100sec Choreography）归到那个女团。"""
    girls_table, boys = name_tables(log)
    boys = set(boys)
    result = {}
    for n in names:
        key = norm(n)
        hit = girls_table.get(key)
        result[n] = ("girl", hit[0]) if hit and hit[1] >= MIN_LINKS and key not in boys else ("other", "")
    girls = sorted((n for n, (k, _) in result.items() if k == "girl"), key=len)
    for n, (k, _) in list(result.items()):
        parent = next((g for g in girls if g != n and n.casefold().startswith(g.casefold() + " ")), None)
        if parent:
            result[n] = result[parent]
    return result
