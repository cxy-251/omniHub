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

`client` 不另行启动服务。已有服务运行时通过 `client` 连接主界面。带文件参数的调用使用独立外部窗口，同一用户/配置下后续调用路由到该窗口；普通主界面目前仍不做全局单实例。

默认配置在操作系统用户配置目录的 omnihub 子目录。第一次试用可使用包所在目录内的隔离配置，避免接触已有配置：

```sh
OMNIHUB_CONFIG_DIR="$PWD/preview-config" ./omniHub
```

不要添加三个参考项目的资产目录作为可写库。添加已有媒体库时勾选“只读”；明确选择可写库会按配置创建目录和模块索引。全局账号状态初始存配置目录，切换主库通过界面确认迁移。

Qt 当前包含播放器、游戏大厅、资源库管理和文本阅读四页。播放器中“打开文件”可多选本机音视频，播放列表最多 256 项，支持上一项/下一项、列表选项和结束自动接续，末项停止。列表在切页和原生游戏让出/恢复界面时保留。提供音量、0.5–3× 倍速、音轨/字幕切换、关闭字幕、加载外部 SRT/ASS/SSA/VTT 和字幕延迟；也可用 `./client /absolute/path/video.mp4` 打开文件。视频区域左侧纵向手势调画面亮度、右侧调音量，横向手势跳转，长按临时至少 2× 播放并在松开后恢复。画面亮度不改变系统背光，触控尚待实体屏验收。本地文件账号级断点已接入，网络流断点仍需实现；带参数外部窗口与主播放器独立，游戏交接快照单独保留主界面队列。原生游戏在所选可写库下的 standalone_games 分类目录中提供 omni.json，然后使用大厅“扫描所选库”。Windows exe/Wine/Proton 尚不能当原生程序启动。

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

Linux 桌面会话中，MPRIS 可控制当前已打开的播放器（播放/暂停/停止、音量、倍速、跳转和列表导航），提供媒体 title/artist/album 标签，标签缺失时标题回退文件名。本地同目录 cover.jpg/cover.png/folder.jpg 可作为封面；暂不提取媒体内嵌封面。离开播放器页会清除媒体控制对象和能力，独立文件在服务不可用时仍继续播放。实体锁屏/息屏音频行为还需验收，不能把 D-Bus 接口可调用当作后台播放全部完成。

本地文件断点现在由 Qt 接入服务端账号状态：重新打开同一文件版本可恢复位置、暂停、音量和倍速，游戏交接/切页快照已有位置时优先使用快照。播放中每 15 秒检查一次，暂停、切换、关闭时保存，位置按 0.1 秒量化，相同状态跳过；完成/停止保存为从零开始。只对本机普通文件启用，网络流暂不保存。文件身份/版本使用路径摘要及文件大小、修改/创建时间摘要，不是内容哈希；保持这些元数据的原地改写仍可能沿用断点。

需要服务在线且管理员已初始化。断点跟随全局账号状态迁移，不在媒体旁写入；界面显示失败，重连/重新打开可重新发现账号。退出最多等待 1.5 秒，服务离线、存储故障或强杀可能丢失最后一段进度；不能宣称离线保存已成功。保存队列最多 16 份并按媒体合并，避免网络故障无限占内存；服务不可用仍允许独立本地播放。

## 外部文件与 Linux 关联

`./client file.mp4` 打开独立外部播放窗口；再次 `./client other.mkv` 在原外部窗口切播、激活，不暂停主界面正在播放的内容。一次最多 32 个现存本地文件，多个参数构成外部列表。IPC 和锁位于该配置的私有空间；测试配置与正式配置各自独立。没有服务时也可播放，账号断点会显示不可用。

包内提供 omnihub-client 与 omnihub.desktop，Linux 用户可选择注册自己的媒体打开入口。在包目录中手动执行（构建/检查不会替你写入这些系统用户目录）：

```sh
mkdir -p "$HOME/.local/bin" "${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ln -s "$PWD/omnihub-client" "$HOME/.local/bin/omnihub-client"
cp omnihub.desktop "${XDG_DATA_HOME:-$HOME/.local/share}/applications/omnihub.desktop"
```

安装后确保 ~/.local/bin 在桌面会话 PATH 中，通过文件管理器“打开方式”选择 omniHub Player；不会自动更改现有默认播放器。移动/删除预览包需重新更新入口。CMake Linux 安装也提供 bin/omnihub-client 与 share/applications/omnihub.desktop。模板已用 desktop-file-validate 检查，实际桌面关联/激活和 Windows/macOS 文件事件仍待目标设备验收。

## Qt 文本阅读

“文本阅读”按页浏览各库 docs/novels 下的书籍，每页 24 项；TXT、Markdown、RST 当前按源文本阅读。服务每次最多读取 80 行/2MiB，Qt 每屏最多排版 16Ki UTF-16 字符，避免长行一次进入布局；保留连续空行和完整 emoji。上一页/下一页支持当前阅读历史，“从头阅读”可从恢复位置返回书首。历史最多 256 个块，不常驻整本书。

成功读取块后合并 500ms 内的进度更新，保存全局账号下的文件版本/物理字节游标，重开恢复到该块开头；不是块内精确滚动位置。闲置没有定时扫描或写入。切页销毁请求与内容；关闭窗口最多等待一秒保存，离线/强杀可能丢失最后位置，保存失败会显示。EPUB 原生章节阅读、Markdown/RST 排版尚未实现，当前不将这些算作完成。

## 任务调试台

Web 任务页已使用注册任务选择与参数表单，拒绝任意命令路径。注册文件放在服务配置目录 tasks.json，读取规则、可直接试运行的 uv 版本检查、Python/凭证及目录锁说明见源码 docs/TASKS.md（预览包内 TASKS.md）。默认没有注册任务，业务爬虫/封箱仍需分别交付；不把调试台框架当作这些业务功能完成。离开页面不会停止服务端任务，重开可从任务记录继续查看和停止。
