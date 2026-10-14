# Qt 客户端构建

需要 C++17 编译器、CMake 3.22+、Qt 6.6+（Core/Gui/Quick/Qml/Network/OpenGL，Linux 另需 DBus）和 libmpv 开发头文件/链接库。

通过 `CMAKE_PREFIX_PATH` 指定 SDK 安装前缀。已有 pkg-config 时使用其导入目标；没有 pkg-config 时，直接查找 `mpv/client.h` 和链接库，仍保留真实的库路径。Windows 可以使用兼容编译器的 libmpv SDK，无需额外安装 pkg-config。

在项目根目录运行，`QT_SDK_PREFIX`、`MPV_SDK_PREFIX` 由本机工具位置提供：

```sh
cmake -S client -B .build/client -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PREFIX_PATH="$QT_SDK_PREFIX;$MPV_SDK_PREFIX"
cmake --build .build/client --parallel 2
```

可通过 `CMAKE_CXX_COMPILER`、`CMAKE_MAKE_PROGRAM` 指定现有工具。在没有系统开发头文件的环境中，使用与本机 libc 匹配的独立工具链；手工补充 C 头文件时使用 `-idirafter`，避免把它们排到 C++ 标准库头文件之前而破坏 `#include_next`。修改工具链后用新的构建目录，避免旧的编译器检测缓存污染结果。

依赖只解压至项目构建目录即可进行隔离验证，不需要写入系统目录。依赖包和二进制不提交源码仓库；正式发布还需打包运行库、Qt 平台插件和 QML 模块，并在各目标平台验证。

2026-10-14 已使用现有 GCC 15.1.1、Qt 6.9.1、CMake/Ninja 及项目内 libmpv 0.40.0 SDK 完成 Linux Release 编译/链接，产物位于 `.build/qt-native/omnihub_client`。动态依赖检查无缺失项。播放器已改为渲染线程持有 GL 上下文、GUI 线程消费有界事件，核心延迟至最后一个渲染器释放后销毁；实际 OpenGL 测试验证了视频像素、暂停/精确跳转/恢复、隐藏后的场景图重建和正常退出。完整客户端仅做隔离启动检查；原生其他页面、Windows/macOS 构建和发布包尚未验收。

## 播放核心集成验证

必要的原生集成验证可单独启用，不引入 QtTest 或 GUI 自动化依赖：

```sh
cmake -S client -B .build/client -DOMNIHUB_BUILD_PLAYER_CHECK=ON
cmake --build .build/client --target omnihub_player_check --parallel 2
.build/client/omnihub_player_check "$TEST_VIDEO" "$TEST_SCREENSHOT"
```

测试视频需至少八秒，含有不同颜色的真实画面；测试目录与截图路径均应在项目内。测试创建一个 320×240 窗口，检查有效像素、进度及渲染器重建，并正常释放资源。无显示窗口验证可使用 Qt 的 `offscreen` 平台，但它默认的软件后端不支持 FBO：需要同时指定 `QT_QUICK_BACKEND=rhi`、`QSG_RHI_BACKEND=opengl` 和 `QT_QPA_PLATFORM=offscreen`，以及本机 Qt 插件/运行库路径。软件后端应显示明确错误并退出测试，而非空指针崩溃。

## 原生游戏服务联调

可选检查使用真实 Qt 网络控制器与 Go 测试在项目临时目录生成的资源库/子进程，不需要浏览器、插件或 QtTest：

```sh
cmake -S client -B .build/client -DOMNIHUB_BUILD_GAME_CHECK=ON
cmake --build .build/client --target omnihub_game_check --parallel 2
mkdir -p .test-tmp
OMNIHUB_QT_GAME_CHECK="$PWD/.build/client/omnihub_game_check" \
  TMPDIR="$PWD/.test-tmp" \
  go test ./internal/server -run '^TestIndexedLaunchUsesOwningLibraryAndHostMetadata$' -count=1 -v
```

动态 SDK 的运行库路径由本机环境提供。检查验证两页清单、库/游戏标识、真实子进程启动及准备文件、会话停止和实际退出；只有输出 `GAME_CHECK_OK` 且 Go 端确认准备文件/无活动游戏才通过。普通 Go 全套测试不依赖 Qt。该检查不代表商业游戏、Wine/Proton、完整游戏期间内存让出或跨系统 Qt 运行已完成。

原生游戏检查现在先通过真实 GameLauncher 发起所选库索引扫描，再分页；新增的测试目录不会预写索引。大厅中的“扫描所选库”是明确的管理员写操作，仅对在线可写库启用，运行中的游戏/任务或正在扫描会阻止新的扫描。只读库使用其已有索引，不由此按钮建库或迁移。

## GUI 进程让出检查

```sh
cmake -S client -B .build/client -DOMNIHUB_BUILD_YIELD_CHECK=ON
cmake --build .build/client --target omnihub_yield_check --parallel 2
OMNIHUB_QT_YIELD_CHECK="$PWD/.build/client/omnihub_yield_check" \
  TMPDIR="$PWD/.test-tmp" \
  go test ./internal/server -run '^TestIndexedLaunchUsesOwningLibraryAndHostMetadata$' -count=1 -v
```

运行库/平台插件及 offscreen 参数按上文提供。该检查使用同一检查程序的 GUI、Core 观察器、恢复 GUI 三种分支；确认原 GUI 已退出但实际游戏存活，服务端停止游戏后恢复四项快照。输出 `GUI_YIELD_OK` 才表示链路通过；Linux `/proc` 内存数据仅作诊断，不用于信号控制。普通 Go 测试跳过此可选检查。

产品客户端启动游戏后退出完整 GUI 进程，Core 观察器正常期间缓存连接配置，仅查询当前会话；完成后重新打开 GUI。配置目录中的 `native-ui.json` 是限量界面恢复快照，`native-yield.lock` 防止清理与恢复重叠。连续服务查询失败会带明确状态未知提示恢复界面。此次 Linux offscreen Core 观察器约 26MiB RSS，尚不代表商业游戏、跨平台 GUI 或完整客户端空闲内存预算已验收。

## 音轨与字幕检查

原生播放器检查可增加第三个参数，输入媒体必须有两条音轨和两条内嵌字幕，时长至少八秒；建议使用无声 fixture，不播放测试音。第三个参数是项目内外部字幕文件：

```sh
.build/client/omnihub_player_check "$MULTITRACK_VIDEO" "$SCREENSHOT" "$EXTERNAL_SUBTITLE"
```

仍按上文提供 offscreen/RHI/OpenGL 和 SDK 环境。检查读取真实轨道、切到第二音轨/字幕、关闭字幕、加入第三条外部字幕、字幕延迟请求、真实彩色视频像素和原有暂停/精确跳转/渲染器重建。中文/空格路径转换为 file URL 后加载；只有 `TRACK_SWITCH_DISABLE_EXTERNAL_AND_DELAY_OK` 和 `CLEAN_EXIT 0` 均出现才通过。默认两个参数的原播放器检查仍可使用。

轨道通过异步 mpv 属性事件更新，每媒体首次加载补一次异步读取；最多 256 条、单字段 512 字节，源替换清空旧清单并用加载版本拒绝迟到回复。外部字幕限本地 SRT/ASS/SSA/VTT 和 8MiB，界面按纯文本显示元数据。真实文件对话框选择和触控硬件操作尚需用户设备验收；不把 API/像素检查当作这两项验收。
