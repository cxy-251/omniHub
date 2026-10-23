# omniHub

omni-deck 的继任者：Go 服务 + Qt6/libmpv 本地客户端 + React Web 前端，面向 Steam Deck 的游戏与媒体中心。

- [架构规格](ARCHITECTURE.md)
- [编码规范](DEV_RULES.md)
- [现状、计划与构建](STATUS.md)
- [任务注册](docs/TASKS.md)

## 下载

在 [Releases](https://github.com/cxy-251/omniHub/releases) 页面下载：

| 平台 | 文件 |
| --- | --- |
| Linux / Steam Deck | `.AppImage`（直接运行）或 `-linux-x86_64.tar.gz`（解压后运行 `omniHub/AppRun`） |
| Windows | `-setup.exe`（装到当前用户，不需要管理员权限）或 `-portable.zip`（解压后运行 `omniHub.cmd`） |
| macOS（Apple 芯片） | `.dmg` 或 `.zip`；未经 Apple 公证，首次打开需在「系统设置 → 隐私与安全性」中允许 |
| 安卓（Termux） | `-android-arm64.tar.gz`：只有服务端与网页端，用法见包内 README.txt（第一次用 `--setup-admin` 创建管理员） |

## 发版

1. 修改仓库根目录的 `VERSION`。**第二位变化（x.Y.0）才发布**；第三位的修订（x.y.Z）打标签只构建、不发布。
2. 提交后打标签并推送：`git tag v<版本号> && git push origin main v<版本号>`。

标签推送后由 GitHub Actions 构建全部平台（`.github/workflows/release.yml`），x.Y.0 版本创建 Release。平时推送代码不触发构建。
改了工作流先在 Actions 页面手动运行一次（不填标签只构建），全部平台通过再打标签。
标签构建失败时不必重打标签：修好推送后手动运行工作流并填写该标签，用最新代码构建并发布到它。
