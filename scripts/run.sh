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
exec build/client/omnihub_client "$@"
