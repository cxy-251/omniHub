omniHub 安卓版（arm64，只含服务端与网页端）

1. 安装 Termux 与 Termux:Boot（都用 F-Droid 版），Termux:Boot 装好后打开一次。
   把压缩包解压到 Termux 里，例如：
   tar -xzf omniHub-*-android-arm64.tar.gz -C ~
2. 第一次使用先创建管理员（会提示输入密码）：
   ~/omniHub/omnihub-server --setup-admin admin
3. 启动服务并设为开机自启：
   ~/omniHub/omnihub.sh start
   ~/omniHub/omnihub.sh boot-on
   其他命令：omnihub.sh stop | status | log | boot-off
   开机脚本也会启动 SSH（装了 openssh 时，端口 8022），方便从电脑更新。SSH 不归「只在家里的网络上对外服务」管，
   在外面的 Wi-Fi 上也开着，所以只允许密钥登录、不设密码：把电脑的公钥加进 ~/.ssh/authorized_keys，再运行
   printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\n' > $PREFIX/etc/ssh/sshd_config.d/key-only.conf
   （没有 sshd_config.d 目录先 mkdir -p 建一个），然后 pkill -HUP sshd。设置 → 网络 页会显示是否只允许密钥登录。
4. 手机浏览器打开 http://127.0.0.1:8998/ 登录；在「设置 → 资源库」添加资源库目录（目录结构与 Deck 上的相同）。
   同一 Wi-Fi 的其他设备打开 http://<手机 IP>:8998/ 。
   短视频缩略图要用 ffmpeg：pkg install ffmpeg

后台保活：在系统设置里把 Termux 的省电策略设为「无限制」。开机脚本会一直挂着（通知栏显示 Termux 有 1 个任务），
这样系统不会回收 Termux。不要给 Termux 开唤醒锁：HyperOS 等系统会把长时间持有唤醒锁判为异常耗电并强制结束。
