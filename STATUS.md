# omniHub 现状与计划

更新：2026-10-15。规格见 `ARCHITECTURE.md`，编码规范见 `DEV_RULES.md`。

## 现状（接手时核实）

| 部分 | 状态 |
|---|---|
| Go 服务 | 能编译、零库启动，空载约 20MB。账号/权限、多库、漫画/音频/文本/视频/相册接口有基础实现 |
| Qt 客户端 | 4 页：播放器（libmpv 内嵌 + 外部窗口）、游戏大厅、资源库管理、文本阅读。无短视频、矩阵、相册、SC2 页 |
| Web 前端 | React/TS 迁移了一部分；`web/js/tools.js`、`media.js` 仍是旧代码，工具模块有权限绕过与脚本注入问题 |
| 原生游戏 | 只能启动 Linux 原生程序；Wine/Proton/Steam 未实现；游戏未放入独立 cgroup |
| 资源库管理 | ✅ 管理员页可视化增删（目录浏览）、分类开关，实时生效；Qt 端用系统文件夹对话框 |
| 真实资源库 | agy 阶段（10-13）曾在 `~/Games/omni_library` 写入 `users.db`、`games.db`、`manga.db`、短视频 `albums.db`/`thumbnails.db`/`meta.db`，原图目录仍在；之后保持只读 |

## 计划（按用户常用度排序）

1. **进程与内存分离**：游戏和外部窗口放入独立 systemd 用户 scope，游戏被杀不波及 omniHub。
2. **短视频**：✅ 库内数据库（索引、缩略图、图集入库、校验清理）与 Web 浏览/管理页（2026-10-15，测试库实测）。待做：Qt 短视频页、跨设备数据库合并（2.3）。
3. **多联放映矩阵**：Qt 原生 2/3 屏、焦点出声、槽位独立频道。
4. **相册**：`gallery.db` + 原图分卷入库流程；Qt 相册页。
5. **SC2**：Wine/Proton 启动驱动 + 地图/种族/难度面板。
6. **挂载主资源库**：上述数据库功能完成后，经用户确认以可写方式挂载真实库（处理 agy 阶段遗留的数据库）。
7. **其他**：其余游戏驱动、Web 端剩余模块（网页游戏、漫画、小说等）、工具模块安全修复、第三方运行器改为构建时拉取、GitHub Actions 交叉编译。

## 构建（本机 Linux）

```sh
source scripts/steamdeck-env.sh
pnpm --dir web install --frozen-lockfile && pnpm --dir web build
nice -n 19 go build -p 2 -o build/omnihub-server ./cmd/server
cmake -S client -B build/client -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_SYSROOT="$OH_SYSROOT"
nice -n 19 cmake --build build/client -j2
```

构建产物都在 `build/`（已忽略），Go 服务和 Qt 客户端的这几条命令 2026-10-15 实测通过。
