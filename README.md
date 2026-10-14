# omniHub

Go 服务、React/TypeScript/Tailwind Web 和 Qt6/libmpv 本地客户端的跨平台媒体与游戏中心。

当前提供可启动的 Linux 开发预览。完整目标仍在实现中，具体已实现主流程和缺项见 [DELIVERY_STATUS.md](DELIVERY_STATUS.md)，启动和构建见 [RUNNING.md](RUNNING.md)。

- [架构规格](ARCHITECTURE.md)
- [编码规范](DEV_RULES.md)
- [分阶段实现与验证记录](IMPLEMENTATION_PLAN.md)
- [Codex 接手记录与代码行数](TAKEOVER.md)
- [Qt 构建与必要集成检查](client/BUILDING.md)
- [Web 构建与离线游戏检查](web/README.md)

预览包目录中的 `omniHub` 启动服务和本地界面，`server` 单独启动服务，`client` 连接已运行服务。服务内嵌 Web 页面和离线引擎，使用时无需 Node 或 pnpm；开发构建使用 pnpm。

开发数据、临时验证和构建包分别在项目 `.test-tmp` 与 `.build` 中。三个来源项目保持只读，预览包不包含用户账号、凭证或媒体资产。
