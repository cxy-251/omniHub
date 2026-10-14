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

2026-10-14 已使用现有 GCC 15.1.1、Qt 6.9.1、CMake/Ninja 及项目内 libmpv 0.40.0 SDK 完成 Linux Release 编译/链接，产物位于 `.build/qt-native/omnihub_client`。动态依赖检查无缺失项。此次不代表播放或界面验收：旧播放器的 OpenGL 初始化线程、mpv 回调与销毁顺序仍待修复；Windows/macOS 原生客户端构建和发布包也尚未验证。
