#!/data/data/com.termux/files/usr/bin/sh
# 在 Termux 里管理 omniHub 服务（只有服务端与网页端）。
# 用法：omnihub.sh start | stop | status | log | boot-on | boot-off
#   boot-on 装上 Termux:Boot 开机脚本（需要安装 Termux:Boot 应用并打开过一次），boot-off 去掉。
cd "$(dirname "$0")" || exit 1
dir=$(pwd)
boot="$HOME/.termux/boot/omnihub-start.sh"
pid() { p=$(cat omnihub.pid 2>/dev/null) && [ -r "/proc/$p/cmdline" ] && grep -q omnihub-server "/proc/$p/cmdline" && echo "$p"; }

case "${1:-status}" in
  start)
    p=$(pid) && kill "$p" && sleep 1
    # 不申请唤醒锁：持有唤醒锁约一小时后 HyperOS 会把 Termux 判为异常耗电并强制结束（z6x hub 2026-10-02 遇到过）
    nohup ./omnihub-server >> omnihub.log 2>&1 < /dev/null &
    echo $! > omnihub.pid
    sleep 2
    if p=$(pid); then echo "omniHub 已启动（PID $p）"; else echo "启动失败："; tail -5 omnihub.log; exit 1; fi ;;
  stop)
    if p=$(pid); then kill "$p"; echo "omniHub 已停止"; else echo "omniHub 本来就没有运行"; fi
    rm -f omnihub.pid ;;
  status)
    if p=$(pid); then echo "omniHub 运行中（PID $p）"; else echo "omniHub 未运行"; exit 1; fi ;;
  log)
    tail -20 omnihub.log ;;
  boot-on)
    mkdir -p "$(dirname "$boot")"
    cat > "$boot" <<EOF
#!/data/data/com.termux/files/usr/bin/sh
# omniHub 开机启动（由 $dir/omnihub.sh boot-on 生成）。
"$dir/omnihub.sh" start
# 不退出：开机脚本是 Termux 的一个后台任务，脚本结束后 Termux 可被系统回收，连同 omniHub 一起被结束。
# 一直挂着让服务保持运行（通知栏显示 Termux 有 1 个任务）。
exec sleep 2147483647
EOF
    chmod 755 "$boot"
    echo "已装上开机脚本：$boot" ;;
  boot-off)
    rm -f "$boot" && echo "已去掉开机脚本" ;;
  *)
    echo "用法：omnihub.sh start | stop | status | log | boot-on | boot-off"; exit 1 ;;
esac
