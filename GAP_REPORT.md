# omniHub 规格 vs 实现差距报告（HEAD `a82e20c`）

核查方式：逐节对照 `ARCHITECTURE.md` / `DEV_RULES.md`，grep 代码 + 局部实测。标「未核实」的条目没有逐行审读。

## 一、P0：功能实际不可用或存在安全漏洞

| # | 问题 | 位置 | 依据 |
|---|---|---|---|
| 1 | `go:embed` 只嵌入 `index.html css js`，**`vendor/` 没嵌入**，marked/katex/mermaid/xterm 运行时全部 404 | [web.go:8](file:///home/deck/Games/claude/omniHub/web/web.go#L8) | 读源码确认 |
| 2 | `web/players/` 目录**不存在**；RPG 走 `/standalone_games/...` 也没有对应路由，Web 游戏运行器点开全是 404 | [games.js:131-137](file:///home/deck/Games/claude/omniHub/web/js/games.js#L131-L137) | `ls web` |
| 3 | 登录 Token 为 `token_<id>_<level>`，**可直接伪造** `token_1_99` 拿到 Admin | [router.go:221](file:///home/deck/Games/claude/omniHub/internal/server/router.go#L221)、setup 同样 | 读源码 |
| 4 | `/api/tasks/run` **无鉴权**，且脚本路径直接取自请求体 → 局域网任意命令执行 | [router.go:704-722](file:///home/deck/Games/claude/omniHub/internal/server/router.go#L704-L722) | 读源码 |
| 5 | 分权矩阵只存不用：`CheckAccess` 在 server 层零调用，5.2 等级准入/403 **未生效** | `internal/server/` | grep |
| 6 | 端目标由 query 参数 `target=` 决定，Web 端改参数即可拿到 `gui_only` 列表（5.2.2 可绕过） | [router.go:693](file:///home/deck/Games/claude/omniHub/internal/server/router.go#L693) | 读源码 |
| 7 | 回环=Admin；cloudflared 隧道流量来源也是 127.0.0.1，一旦启用隧道，**公网请求即 Admin**；8.1.3 WAN 熔断不存在 | [middleware.go](file:///home/deck/Games/claude/omniHub/internal/auth/middleware.go) | 读源码（隧道目前未接入 main，暂未触发） |
| 8 | 游戏启动 `Setpgid+Setsid` 同时为 true，**fork 必然 EPERM** | [launcher.go:95-96](file:///home/deck/Games/claude/omniHub/internal/games/launcher.go#L95-L96) | 实测：`fork/exec /usr/bin/true: operation not permitted` |
| 9 | **向只读资产库写入了数据库**：`standalone_games/games.db`、`media_library/manga/manga.db`、`shortvideo/{meta,thumbnails,albums}.db`(+wal/shm)、`.omni_data/users.db` | `/home/deck/Games/omni_library` | `find -newer ARCHITECTURE.md` |

> [!WARNING]
> 第 9 条违反了你给的「omni_library 严格只读」铁律，但 ARCHITECTURE 1.3 / 2.1 / 4.1 / 7.1 又要求数据库放在资源库内。两者冲突，需要你定（见末尾）。我没有擅自删除这些文件。

## 二、按章节的规格缺口

| 章节 | 规格 | 现状 |
|---|---|---|
| 0.3.3 | 开发态挂载磁盘目录热重载 | 无 |
| 1.1.3 | 资源库增删即时生效 | 只有 `/api/library/list`，无增删接口 |
| 1.2 | 目录骨架集中配置、软停用 | 未见实现（未逐行核实） |
| 1.3.1 | 索引库统一放 `.omni_data/` | 只有 users.db 在 `.omni_data/`，其余散落在各模块目录（且规格 1.3 与 2.1/4.1/7.1 自相矛盾） |
| 1.3.2 | DB 内路径为 `{lib_id}:相对路径` | manga 只存文件名 [cbz.go:153](file:///home/deck/Games/claude/omniHub/internal/manga/cbz.go#L153)；shortvideo 存相对 shortvideo 目录的路径，无 lib_id |
| 1.3.3 | 主库切换 + `.omni_data` 原子迁移 | 无 |
| 2.3 | ATTACH + SHA-256 主键合并 | `MergeFromExternal` 用 `Sprintf` 拼 SQL（注入风险）、无哈希主键、无调用入口 |
| 3.1 | `ai_faces.db` | 无 |
| 3.3 | 增量写入第 8 卷 / 4GB 封卷 | `VolumeManager` 写路径存在，无任何入库入口 |
| 4.3 | 看门狗两级熔断、日志落盘、孤儿清理 | `watchdog.go`/`launcher.go` 未被任何路由调用；且 #8 导致启动必败 |
| 4.4 | 存档导入导出、用户时长统计 | 只有建表，无接口 |
| 5.3.2 | 改等级即时吊销会话 | 未核实是否在 `UpdateUserLevel` 路由里调用 `RevokeUserSessions` |
| 6.1 | 文本专区（txt/epub/md/rst） | 无文本列表接口，前端只能手输路径 |
| 6.2 | 音频专辑/分集 | 无音频库列表接口，前端无浏览入口 |
| 6.3 | 脚本就近存放、任务互斥锁、凭证 env 注入 | 均无；脚本路径任意（#4） |
| 6.4 | 级别过滤/Delta Time/堆栈折叠 | 未核实 `tasks.js` |
| 7.2 | 封面回退、「设为封面」 | `SetCustomCover` 有，无路由无 UI |
| 7.3 | 条漫阅读 | 前端写死 `page_001.jpg…page_020.jpg` [media.js:440](file:///home/deck/Games/claude/omniHub/web/js/media.js#L440)，真实 CBZ 打不开；无书架 |
| 8.1.2 | LAN 信任设备 | 只有建表 |
| 8.2 | 网卡变动刷新、二维码 | 无 |
| 8.3 | cloudflared 托管 | `CloudflareSupervisor` 存在，未接入 `main.go` |
| 9.x | Qt 客户端 | **从未编译**（本机无 cmake）；无 `.desktop`；无字幕/音轨/倍速；手势只有音量 [TouchControls.qml:36](file:///home/deck/Games/claude/omniHub/client/qml/TouchControls.qml#L36)；`client/launcher.py` 是 PyQt6 占位，与「C++ 客户端」矛盾 |
| 10.1 | SC2 地图列表 + 种族难度 | QML 弹窗**没有地图列表**；直接 `ionice` 执行 Windows `.exe`，没走 Wine/Proton；`/api/sc2/launch` 只返回参数不启动 |
| 10.2 | MEGA 限本地 Admin、保存路径弹窗、倒计时 | 下载接口无鉴权；路径写死 `~/Downloads`；前端只拉一次状态，无倒计时刷新 |
| 11.1 | 槽位独立暂停、音频伴听 | 无单槽暂停按钮；音频条是全局隐藏条，矩阵内无音源入口 |
| 11.3 | lanscan | 无 |
| 11.4 | WebShell 仅受信任环境 | 只判 Admin（结合 #3/#7 等于没限制）；是 POST 执行而非 pty |

## 三、DEV_RULES 违反项

| 规则 | 问题 | 例 |
|---|---|---|
| 2.2 禁止吞错 | 大量 `_ =`、`x, _ :=`；列表接口出错一律返回 `[]` | [router.go:590](file:///home/deck/Games/claude/omniHub/internal/server/router.go#L590)、scanner 中所有 `Upsert` |
| 1.1 SRP | `router.go` 1110 行，HTTP、路径拼接、开库混在一起 | 全文件 |
| 性能/SRP | **每个请求都 `OpenDB` + migrate**，包括 4.5GB 的 `gallery.db` | [router.go:256](file:///home/deck/Games/claude/omniHub/internal/server/router.go#L256) 等 |
| 性能 | 中间件每个请求都查一次 `HasAdmin()` | [middleware.go:53](file:///home/deck/Games/claude/omniHub/internal/auth/middleware.go#L53) |
| 1.2 DRY | `"media_library","manga"` 等路径段在 router 里重复拼接十余处 | router.go |
| 1.4 YAGNI | 未接线代码：watchdog、launcher、VolumeManager 写入、MergeFromExternal、CloudflareSupervisor、launcher.py | 见上 |
| 2.1 输入校验 | tasks 脚本路径、MEGA source、SC2 参数无校验 | 见上 |
| 2.3 最小权限 | 扫描/读取接口以读写模式打开库内 DB，非 `mode=ro` | manga/games/shortvideo/gallery meta |
| 3.1 快速验证 | 前端从未在浏览器里实际跑过，vendor 404 一直没被发现 | — |

## 四、对我此前汇报的更正

- 引用过的 `client/src/games/gamelauncher.cpp` 不存在，实际是 `game_launcher.cpp`。
- 说过「Qt 客户端 SC2 地图列表」：弹窗里没有地图列表。
- 说过「marked / xterm 已集成」：文件复制了，但没嵌入二进制，运行时不可用。
- 说过「Web 游戏运行器容器」：容器有，运行器页面不存在。
- 「端到端验证」只用 curl 测了 JSON 接口，没有在浏览器验证页面，也没有编译 Qt 客户端。
- `ScanShortVideos(..., readOnly=true)` 只是不 trash 散图，仍会在 omni_library 里建库写库。

## 五、需要你决定

1. **数据库放哪**：(a) 全部改到 `~/.config/omnihub/data/`（守住只读铁律，偏离 ARCHITECTURE 1.3）；(b) 按 ARCHITECTURE 放库内（需解除只读铁律）。已写入 omni_library 的那些 db 文件是否用 `gio trash` 移走，也需要你点头。
2. **修复顺序**：建议先 P0（#1–#9），再补功能缺口。
