#!/data/data/com.termux/files/usr/bin/sh
# 在 Termux 里启动 omniHub 服务（只有服务端与网页端）。手机浏览器打开 http://127.0.0.1:8998/ ，
# 同一 Wi-Fi 的其他设备打开 http://<手机 IP>:8998/ 。第一次使用先运行 ./omnihub-server --setup-admin <用户名> 创建管理员。
cd "$(dirname "$0")" && exec ./omnihub-server "$@"
