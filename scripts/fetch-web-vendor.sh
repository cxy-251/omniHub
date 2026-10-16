#!/bin/bash
# 生成 web/vendor（不进仓库）：按 web/vendor-sources/emulatorjs.lock 固定的版本从 npm 下载 EmulatorJS 前端与核心，
# 校验 sha512 后解出需要的文件，再打上本地补丁（关闭在线更新检查等，见补丁文件开头说明）。
# 下载的压缩包缓存在 ~/.cache/omnihub/vendor-src，重复运行不再下载。用法：scripts/fetch-web-vendor.sh [输出目录]
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${1:-$ROOT/web/vendor}
CACHE=${OMNIHUB_VENDOR_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/omnihub/vendor-src}
LOCK=$ROOT/web/vendor-sources/emulatorjs.lock
PATCH=$ROOT/web/vendor-sources/emulatorjs-4.2.3-offline.patch
mkdir -p "$CACHE"
DEST=$OUT/players/emulatorjs
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
rm -rf "$DEST"
mkdir -p "$DEST/data/cores/reports"
while read -r name version integrity; do
    case "$name" in ''|'#'*) continue ;; esac
    base=${name#@emulatorjs/}
    tgz=$CACHE/$base-$version.tgz
    if [ ! -f "$tgz" ]; then
        curl -sSfL --retry 3 -o "$tgz.part" "https://registry.npmjs.org/$name/-/$base-$version.tgz"
        mv "$tgz.part" "$tgz"
    fi
    actual="sha512-$(openssl dgst -sha512 -binary "$tgz" | base64 -w0)"
    if [ "$actual" != "$integrity" ]; then
        echo "完整性校验失败：$name@$version" >&2
        rm -f "$tgz"
        exit 1
    fi
    rm -rf "$TMP/package"
    tar xzf "$tgz" -C "$TMP"
    if [ "$base" = emulatorjs ]; then
        cp -r "$TMP/package/data/." "$DEST/data/"
        cp "$TMP/package/LICENSE" "$TMP/package/README.md" "$TMP/package/package.json" "$DEST/"
    else
        core=${base#core-}
        cp "$TMP/package/$core-legacy-wasm.data" "$DEST/data/cores/"
        cp "$TMP/package/reports/$core.json" "$DEST/data/cores/reports/"
    fi
done < "$LOCK"
# 打补丁用 git apply（SteamOS 没有 patch 命令）；放在临时目录里做，避免被当成本仓库内的路径。
mkdir -p "$TMP/apply/data/src"
cp "$DEST/data/src/emulator.js" "$TMP/apply/data/src/"
(cd "$TMP/apply" && git init -q && git apply -p1 "$PATCH")
cp "$TMP/apply/data/src/emulator.js" "$DEST/data/src/emulator.js"
# EmulatorJS 找不到 zh-CN 时退回 zh：两份相同的中文界面翻译。
cp "$DEST/data/localization/zh-CN.json" "$DEST/data/localization/zh.json"
echo "web vendor ready: $DEST"
