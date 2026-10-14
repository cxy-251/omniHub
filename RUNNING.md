# 启动 omniHub 开发预览

当前提供 Linux 本地预览，不是完整架构验收版。功能及缺项见 DELIVERY_STATUS.md。

解压后在包目录运行：

```sh
./omniHub
```

启动器先启动 Go 服务，写入本机连接信息，再打开 Qt 窗口。首次使用会出现管理员创建框；尚未配置资源库也可启动。服务端通常监听 8998，端口被占用时最多向后寻找十个端口，以终端输出及配置目录的 local-client.json 为准。远程浏览器使用服务器 IP 和实际端口登录；运行时不需要 Node、pnpm 或外网 CDN。

启动器保持服务运行，关闭 Qt 窗口不关闭服务，游戏期间让出 GUI 也不停止游戏。在终端按 Ctrl+C 停止服务及其托管游戏。仅启动服务或连接已启动服务：

```sh
./server
./client
./server --version
```

`client` 不另行启动服务。当前没有全局 GUI 单实例路由，不应反复打开多个客户端；已有服务运行时通过 `client` 连接。

默认配置在操作系统用户配置目录的 omnihub 子目录。第一次试用可使用包所在目录内的隔离配置，避免接触已有配置：

```sh
OMNIHUB_CONFIG_DIR="$PWD/preview-config" ./omniHub
```

不要添加三个参考项目的资产目录作为可写库。添加已有媒体库时勾选“只读”；明确选择可写库会按配置创建目录和模块索引。全局账号状态初始存配置目录，切换主库通过界面确认迁移。

Qt 当前包含播放器、游戏大厅、资源库管理三页。播放器暂时没有完整文件选择界面，连接服务后可用 `./client /absolute/path/video.mp4` 打开本机文件。原生游戏在所选可写库下的 standalone_games 分类目录中提供 omni.json，然后使用大厅“扫描所选库”。Windows exe/Wine/Proton 尚不能当原生程序启动。

Linux 包携带 Qt/QML、libmpv 与动态依赖；仍依赖目标系统的 glibc、图形驱动和显示服务。使用当前构建主机验证，尚不保证其他 Linux 发行版可运行。Windows/macOS 当前只有服务编译证据，Qt 发布包未完成。环境有问题时直接在终端启动查看错误；包中 MANIFEST.json 记录文件哈希、构建身份及依赖来源。

## 从源码构建

Web 必须先构建，再由 Go 嵌入：

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build -o .build/omnihub-server ./cmd/server
```

Qt 构建说明见源码 client/BUILDING.md。使用已配置好的项目内 Qt 构建目录和现有 SDK 打包：

```sh
QT_RUNTIME_DIR="$QT_SDK_PREFIX/lib/qt6" \
LD_LIBRARY_PATH="$QT_SDK_PREFIX/lib:$MPV_SDK_PREFIX/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
CMAKE="$CMAKE_EXECUTABLE" \
node scripts/package-linux.mjs
```

脚本只在项目 .build/releases 中写入，不安装工具，不写外部参考项目。输出一个展开目录和 tar.gz；环境变量可指定 CMAKE、PNPM、OMNIHUB_CLIENT_BUILD_DIR。构建产物没有账号、连接凭证或用户资源；使用现有配置目录运行时才创建运行状态。
