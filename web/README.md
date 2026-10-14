# Web 构建与游戏验证

使用 pnpm：`pnpm --dir web build` 同时构建 React/TypeScript/Tailwind 主界面与 TypeScript 游戏运行器入口。生成的 dist 随 Go 服务嵌入，无 Node 的运行环境也可使用已构建服务。

游戏界面仅保留 24 条当前页记录及一个 sandbox iframe。按库/游戏标识请求临时资源授权，不拼接显示名称；离开或退出时取消待处理打开请求、卸载 iframe 并以 keepalive 请求释放授权。原始游戏脚本不能访问父界面或管理 API。部分引擎依赖浏览器持久存储、特定图形加载或 NW.js，须由实际引擎适配验证，当前授权与静态资源主流程不代表这些兼容项已完成。

可选真实浏览器检查使用已有 Chrome，通过 CDP 操作隔离 Go 测试资源库，不安装插件、Playwright 或浏览器，不读取用户浏览器资料。普通 Go 测试不会启动浏览器。先完成构建，再从项目根目录运行，`CHROME_EXECUTABLE` 由本机已有工具位置提供：

```sh
mkdir -p .test-tmp
OMNIHUB_CHROME="$CHROME_EXECUTABLE" \
  OMNIHUB_WEB_GAME_BROWSER_CHECK="$PWD/web/tests/game.check.js" \
  TMPDIR="$PWD/.test-tmp" \
  go test ./internal/server -run '^TestWebGameGrantsResolveOwningLibraryAndRevokeAccess$' -count=1 -v
```

需要支持内置 WebSocket 的 Node（当前验证使用 Node 24）。检查实际登录账号、游戏卡片、iframe 脚本、所属库相对资源、父界面/API 隔离以及退出撤销，成功输出 WEB_GAME_CHECK_OK。截图和 JSON 位于 `.test-tmp/web-game`，测试只清理其自建 Chrome 配置目录。

正式本机索引入口和无索引只读发现已接通，离线 EmulatorJS/Ruffle 及 NES/SWF 验证见下文。其余核心、RPG/SLG、引擎存档及性能仍须完成；旧猜测编号目录的 JS 入口和吞异常的未用 polyfill 已移除。

## 离线引擎

已嵌入 Ruffle 0.5.0、EmulatorJS 4.2.3 和运行器使用的十个非线程 legacy 核心，约 49MB，按用户选择加载；服务端没有启动时下载。版本/原始哈希、许可证及本地差异在 vendor/players。不含 BIOS 或游戏镜像。EmulatorJS 使用源码模块，禁用在线更新/CDN 兜底；沙箱中禁用浏览器持久存储，账号存档尚需接入服务端，不能据此称为存档已保存。

可选真实运行时检查通过自编写的 NES 指令/PPU 色块与 SWF 色块，使用已有 Chrome 和项目隔离配置：

```sh
OMNIHUB_CHROME="$CHROME_EXECUTABLE" \
  OMNIHUB_WEB_RUNTIME_BROWSER_CHECK="$PWD/web/tests/runtime.check.js" \
  TMPDIR="$PWD/.test-tmp" \
  go test ./internal/server -run '^TestOfflineWebRuntimesInChrome$' -count=1 -v
```

检查分别断言正确的运行器身份、实际像素/元数据、无外部网络、退出和资源授权撤销；结果及截图在 .test-tmp/web-runtime。此处证明 NES 与基础 SWF 主流程，其他核心、实体输入/音频、复杂游戏及峰值资源仍需实测。
