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
