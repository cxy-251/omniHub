#!/bin/bash
# Steam Deck (SteamOS 只读根分区) 上的本机构建环境。
# 工具单独解压在 ~/Applications/<工具名>/usr（Arch Linux 软件包）。
# 用法: source scripts/steamdeck-env.sh

OH_TOOLS="${OH_TOOLS:-$HOME/Applications}"

for _t in gcc cmake ninja qt6; do
    PATH="$OH_TOOLS/$_t/usr/bin:$PATH"
done
PATH="$OH_TOOLS/qt6/usr/lib/qt6/bin:$PATH"
export PATH

# gcc/usr/lib 里有一份 glibc，不能整体加入；编译器只缺 isl 和 mpc，单独放在 gcc/runtime-libs。
_libs="$OH_TOOLS/gcc/runtime-libs"
for _t in cmake qt6 libmpv-sdk; do
    _libs="$_libs:$OH_TOOLS/$_t/usr/lib"
done
export LD_LIBRARY_PATH="$_libs${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

export CMAKE_PREFIX_PATH="$OH_TOOLS/qt6/usr:$OH_TOOLS/libmpv-sdk/usr"
export OH_SYSROOT="$OH_TOOLS/gcc"
export QT_PLUGIN_PATH="$OH_TOOLS/qt6/usr/lib/qt6/plugins"
export QML_IMPORT_PATH="$OH_TOOLS/qt6/usr/lib/qt6/qml"
# 中文输入：版本一致时借用系统 fcitx 插件；系统升级后改用自带 Qt 的
# IBus 插件连接 fcitx5 的兼容前端，避免加载依赖新版 Qt 私有接口的插件。
_im="/usr/lib/qt6/plugins/platforminputcontexts/libfcitx5platforminputcontextplugin.so"
if [ -f "$_im" ] && [ -e /usr/lib/libQt6Core.so.6.9.1 ] && [ -e "$OH_TOOLS/qt6/usr/lib/libQt6Core.so.6.9.1" ]; then
    _imdir="${XDG_CACHE_HOME:-$HOME/.cache}/omnihub/qt-im"
    mkdir -p "$_imdir/platforminputcontexts" && ln -sf "$_im" "$_imdir/platforminputcontexts/"
    QT_PLUGIN_PATH="$QT_PLUGIN_PATH:$_imdir"
    case "${XMODIFIERS:-}" in *fcitx*) export QT_IM_MODULE="${QT_IM_MODULE:-fcitx}" ;; esac
elif [ -f "$OH_TOOLS/qt6/usr/lib/qt6/plugins/platforminputcontexts/libibusplatforminputcontextplugin.so" ]; then
    case "${XMODIFIERS:-}:${QT_IM_MODULE:-}" in
        *fcitx:|*fcitx:fcitx) export QT_IM_MODULE=ibus; export IBUS_USE_PORTAL=1 ;;
    esac
fi
unset _im _imdir
# 使用 --sysroot 后，链接器在 sysroot 下找系统库（libproxy、libpulse 的私有依赖）的 RUNPATH，找不到；显式指定。
export LDFLAGS="-Wl,-rpath-link,/usr/lib/libproxy -Wl,-rpath-link,/usr/lib/pulseaudio${LDFLAGS:+ $LDFLAGS}"

# SteamOS 只有运行库 libgcc_s.so.1，工具链 sysroot 又没有开发链接名。
# 在本项目的忽略目录补链接，不修改系统或外部 SDK。
if [ ! -e "$OH_SYSROOT/usr/lib/libgcc_s.so" ] && [ -e /usr/lib/libgcc_s.so.1 ]; then
    _oh_linkdir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.build/compiler-libs"
    mkdir -p "$_oh_linkdir"
    ln -sf /usr/lib/libgcc_s.so.1 "$_oh_linkdir/libgcc_s.so"
    export LIBRARY_PATH="$_oh_linkdir${LIBRARY_PATH:+:$LIBRARY_PATH}"
    export LDFLAGS="-L$_oh_linkdir $LDFLAGS"
    unset _oh_linkdir
fi



# Go 服务是纯 Go（modernc SQLite），不用 cgo；PATH 里有 gcc 时 Go 会默认启用 cgo 并找不到系统头文件。
export CGO_ENABLED=0
unset _t _libs
