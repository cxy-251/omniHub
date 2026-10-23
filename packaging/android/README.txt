omniHub 安卓版（arm64，只含服务端与网页端）

1. 安装 Termux（F-Droid 版），把压缩包解压到 Termux 里，例如：
   tar -xzf omniHub-*-android-arm64.tar.gz -C ~
2. 启动服务：~/omniHub/start.sh
3. 第一次使用：另开一个 Termux 会话，创建管理员（会提示输入密码）：
   ~/omniHub/omnihub-server --setup-admin admin
4. 手机浏览器打开 http://127.0.0.1:8998/ 登录；在「设置 → 资源库」添加资源库目录（目录结构与 Deck 上的相同）。
   同一 Wi-Fi 的其他设备打开 http://<手机 IP>:8998/ 。

建议把 Termux 的省电策略设为「无限制」，否则切到后台后可能被系统结束。
