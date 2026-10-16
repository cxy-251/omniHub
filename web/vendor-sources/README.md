# 网页端离线运行器的来源

`web/vendor/` 不进仓库，由 `scripts/fetch-web-vendor.sh` 生成（`pnpm --dir web build` 会先运行它）：

- EmulatorJS 4.2.3 前端（`@emulatorjs/emulatorjs`）与 10 个 legacy 核心（`@emulatorjs/core-*`：fceumm、snes9x、gambatte、mgba、mupen64plus_next、melonds、genesis_plus_gx、smsplus、mednafen_pce、fbneo），按 `emulatorjs.lock` 里的版本与 sha512 从 npm 下载。
- 本地改动只有 `emulatorjs-4.2.3-offline.patch`（关闭在线更新检查与 CDN 回退、沙箱里不碰浏览器存储）。

许可证：EmulatorJS 前端为 GPL-3.0，生成的目录里保留其 LICENSE；各核心的许可证在核心压缩包内（license.txt）。不包含任何 BIOS、ROM 或商业游戏文件。

Markdown（marked）、公式（KaTeX）、图表（Mermaid）是普通 npm 依赖，版本由 `pnpm-lock.yaml` 固定，随网页一起打包。
Flash（Ruffle）已移除。
