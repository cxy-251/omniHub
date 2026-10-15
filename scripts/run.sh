#!/bin/bash
# 本机开发运行：服务未运行时在后台启动（日志 build/logs/server.log），然后打开 Qt 客户端。
# 关闭客户端不会停止服务；停止服务：pkill -x omnihub-server
ROOT=$(cd "$(dirname "$0")/.." && pwd)
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
