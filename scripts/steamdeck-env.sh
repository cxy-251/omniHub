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
# 使用 --sysroot 后，链接器在 sysroot 下找系统库（libproxy、libpulse 的私有依赖）的 RUNPATH，找不到；显式指定。
export LDFLAGS="-Wl,-rpath-link,/usr/lib/libproxy -Wl,-rpath-link,/usr/lib/pulseaudio${LDFLAGS:+ $LDFLAGS}"

unset _t _libs
