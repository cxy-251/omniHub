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

当前仍需接通正式游戏扫描/索引入口，接入离线 EmulatorJS/Ruffle 包，并实际验证 ROM、SWF、RPG/SLG、引擎存档及性能；旧猜测编号目录的 JS 入口和吞异常的未用 polyfill 已移除。
