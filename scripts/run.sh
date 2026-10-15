#!/bin/bash
# 本机运行：服务未运行时在后台启动（日志 build/logs/server.log），然后打开 Qt 客户端。
# 带文件参数（文件管理器「打开方式 → omniHub」）时打开独立外部播放窗口；已有外部窗口时交给它切播。
# 关闭客户端不会停止服务；停止服务：pkill -x omnihub-server
#
# 进程放在 systemd 用户 scope「app-omnihub-<pid>.scope」里，并安装 omnihub.desktop，
# 这样 KDE 系统监视器的「应用程序」页能把服务和客户端归为 omniHub 一个应用显示（内存合计）。
ROOT=$(cd "$(dirname "$0")/.." && pwd)

DESKTOP_FILE="${XDG_DATA_HOME:-$HOME/.local/share}/applications/omnihub.desktop"
DESKTOP_CONTENT="[Desktop Entry]
Type=Application
Name=omniHub
Comment=游戏与媒体中心
Exec=\"$ROOT/scripts/run.sh\" %F
Icon=applications-multimedia
Categories=AudioVideo;
Terminal=false
MimeType=video/mp4;video/x-matroska;video/webm;video/quicktime;video/x-msvideo;video/x-flv;video/mpeg;audio/mpeg;audio/flac;audio/ogg;audio/x-wav;audio/mp4;audio/aac;audio/opus;"
if [ "$(cat "$DESKTOP_FILE" 2>/dev/null)" != "$DESKTOP_CONTENT" ]; then
    mkdir -p "$(dirname "$DESKTOP_FILE")"
    printf '%s\n' "$DESKTOP_CONTENT" > "$DESKTOP_FILE" 2>/dev/null || true
    # 刷新 MIME 关联缓存，文件管理器「打开方式」里才会出现 omniHub。
    command -v update-desktop-database >/dev/null && update-desktop-database "$(dirname "$DESKTOP_FILE")" 2>/dev/null
fi
if [ -z "$OMNIHUB_SCOPED" ] && command -v systemd-run >/dev/null 2>&1 \
        && systemd-run --user --scope --quiet true >/dev/null 2>&1; then
    export OMNIHUB_SCOPED=1
    exec systemd-run --user --scope --quiet --collect --unit="app-omnihub-$$" -- "$ROOT/scripts/run.sh" "$@"
fi

source "$ROOT/scripts/steamdeck-env.sh"
cd "$ROOT"
if ! pgrep -x omnihub-server > /dev/null; then
    mkdir -p build/logs
    setsid nohup build/omnihub-server >> build/logs/server.log 2>&1 &
    sleep 1.5
fi
# 本机 Qt 没带 wayland 平台插件，直接用 xcb（XWayland），避免启动时的回退警告。
export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-xcb}"
# 标准错误不是终端时 Qt 默认把日志写进 journald，这里强制输出到终端。
export QT_FORCE_STDERR_LOGGING=1
exec build/client/omnihub_client "$@"
