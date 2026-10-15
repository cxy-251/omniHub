# omniHub 现状与计划

更新：2026-10-15。规格见 `ARCHITECTURE.md`，编码规范见 `DEV_RULES.md`。

## 现状（接手时核实）

| 部分 | 状态 |
|---|---|
| Go 服务 | 能编译、零库启动，空载约 20MB。账号/权限、多库、漫画/音频/文本/视频/相册接口有基础实现 |
| Qt 客户端 | 短视频（浏览/多联放映）、相册（瀑布流，只读）、小说（EPUB 书架与阅读器）、资源库管理、游戏大厅（未完成）。播放器只作为「打开方式」调用的独立外部窗口 |
| Web 前端 | React/TS 迁移了一部分；`web/js/tools.js`、`media.js` 仍是旧代码，工具模块有权限绕过与脚本注入问题 |
| 原生游戏 | ✅ 扫描各分类目录（无需索引），按 omni-deck 规则识别主程序；原生 / Proton / Lime3DS / Ren'Py 启动；每个游戏独立 systemd scope + ionice，停止与退出时清理整个 scope；游玩时长按账号记录。封面（图片文件 / Steam 缓存 / exe 图标，缓存在 games.db）、按账号收藏。待做：硬件画像、按账号存档隔离 |
| 资源库管理 | ✅ 管理员页可视化增删（目录浏览）、分类开关，实时生效；Qt 端用系统文件夹对话框 |
| 真实资源库 | agy 阶段（10-13）曾在 `~/Games/omni_library` 写入 `users.db`、`games.db`、`manga.db`、短视频 `albums.db`/`thumbnails.db`/`meta.db`，原图目录仍在；之后保持只读 |

## 计划（按用户常用度排序）

1. **进程与内存分离**：✅ 游戏在独立 systemd 用户 scope（实测 Wine/Proton、Ren'Py、Lime3DS）。
2. **短视频**：✅ 库内数据库（索引、缩略图、图集入库、校验清理），Web 与 Qt 浏览/播放（真实库已挂载）。待做：跨设备数据库合并（2.3）。
3. **多联放映矩阵**：✅ VA-API 显卡直出（EGL）且渲染不等待目标时刻，三路 4K/60 帧不再丢帧；点赞与「我的点赞」频道、管理员移到回收站、记住每屏视频与位置、可弹出独立窗口。 Qt 与 Web 同一套：2/3 屏、焦点屏顶栏、焦点出声、每屏独立频道（平台/博主）与顺序/随机、静音、快捷键；序列由服务端按种子确定性生成。待做：Qt 端音声伴听、移到回收站、Qt 矩阵实机交互验收。
4. **相册**：✅ 只读浏览（Qt 与 Web）：色系筛选、虚拟滚动网格、原图查看。待做：入库流程（原图分卷写入后再开放）、人脸相册。
5. **SC2**：✅ 游戏大厅 Wine 分类里的「星际争霸 II」卡片 → 对战面板（133 张地图缩略图与人数上限、种族、电脑难度/打法、sc2Mod 的 mod）。对局由 features/sc2 的 Python 助手（uv 锁定依赖，移植自 omni-deck）经 burnysc2 + proton-wine 拉起，作为游戏会话运行在独立 scope；实测进入对局、停止后清理干净。助手与虚拟环境在 ~/.cache/omnihub。
6. **挂载主资源库**：上述数据库功能完成后，经用户确认以可写方式挂载真实库（处理 agy 阶段遗留的数据库）。
6.6 **音声**：✅ Qt 音声页（专辑、继续收听、分集搜索与断点）与全局播放引擎（切页不断、底部播放条、倍速/循环模式/定时关闭/±10/30 秒、启动恢复上次分集、MPRIS）。断点按账号存服务端（含时长）。待做：多联放映伴听（Qt）。
6.7 **MEGA**：✅ Qt「下载」页：账号与容量、粘贴链接下载（收件箱 / 游戏暂存区 / 自选目录）、云盘浏览、网盘回收站（移入/恢复）、传输队列（暂停/继续/取消）、配额超限倒计时。MEGAcmd 按需启动，空闲 5 分钟或服务退出时关闭。实测：状态、列表、下载到指定目录、服务退出时关闭 MEGAcmd。未实测：登录表单、配额超限、5 分钟空闲关闭。
6.5 **小说**：✅ Qt 书架（分类、最近阅读、封面或生成式书封）与 EPUB 阅读器（目录、字号、章节+章内位置续读）。docs 下的 md/rst 富文本归 Web 端。TXT 暂未做（库里没有）。
7. **其他**：其余游戏驱动、Web 端剩余模块（网页游戏、漫画、小说等）、工具模块安全修复、第三方运行器改为构建时拉取、GitHub Actions 交叉编译。

## 内存与资源回收

- Qt 与 Web 的列表都按页懒加载，只保留最近 8 页数据；Qt 的 GridView、Web 的 VirtualGrid 只创建可视区附近的格子，滚远即销毁，图片随之释放。页面切走时整页销毁并中止进行中的请求。
- Qt 客户端 2026-10-15 offscreen 实测：RSS 260–310MB，但大部分是共享库与字体的文件映射（Mesa LLVM 55MB 等）；私有脏页 63MB（无图片页）/ 91MB（短视频网格）。
- 服务端：600 张缩略图 16 路并发 1.3 秒，RSS 稳定约 33MB。
- 多联放映：短视频中约 18% 是 4K HEVC。mpv 默认软解时三屏私有内存 1.2–1.6GB 且持续上涨；改为硬解拷回（auto-copy-safe）+ 软解回退 2 线程 + 硬解额外帧 2 后约 300MB、稳定，单路 4K CPU 减半。默认暂停时 CPU 为 0。
- `scripts/run.sh` 把服务和客户端放进 `app-omnihub-<pid>.scope` 并安装 omnihub.desktop，KDE 系统监视器「应用程序」页显示为 omniHub（内存合计）。

## 调试

offscreen 检查 QML 错误时必须设置 `QT_FORCE_STDERR_LOGGING=1`，否则 Qt 把日志写进 journald，看起来“没有报错”。

## 构建（本机 Linux）

```sh
source scripts/steamdeck-env.sh
pnpm --dir web install --frozen-lockfile && pnpm --dir web build
nice -n 19 go build -p 2 -o build/omnihub-server ./cmd/server
cmake -S client -B build/client -G Ninja -DCMAKE_BUILD_TYPE=Release -DCMAKE_SYSROOT="$OH_SYSROOT"
nice -n 19 cmake --build build/client -j2
```

构建产物都在 `build/`（已忽略），Go 服务和 Qt 客户端的这几条命令 2026-10-15 实测通过。
