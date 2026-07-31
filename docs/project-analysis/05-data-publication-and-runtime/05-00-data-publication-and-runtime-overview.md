# 数据、发布与运行基础设施总览

## 1. 模块职责

本模块承载墨笺的权威持久化、附件目录、Markdown 渲染、静态站点、前端资源嵌入、开发/生产启动和质量验证。它决定应用如何从源码变为本地单二进制及只读发布站点。

## 2. 在整体架构中的位置

它位于架构底部，为核心模型和外部集成提供 SQLite、文件、渲染器和进程环境；同时向外提供静态站点服务，是唯一兼具基础设施与交付职责的一级模块。

## 3. 对外提供的能力

- `~/.mdoc` 多数据库工作区与旧数据迁移；
- SQLite schema、WAL、FTS5 和事务；
- 上传、音频、同步投影和静态输出目录；
- 数据库文档预览、本地路径预览和静态 HTML 渲染；
- 分享页、增量站点构建和只读站点进程；
- Vinext 开发、预渲染、Go 资源嵌入、远端 rsync 部署；
- Go 与 TypeScript 测试入口。

## 4. 内部子模块

1. [SQLite 与本地文件存储](./05-01-sqlite-and-local-files.md)
2. [渲染、静态站点与分享](./05-02-rendering-static-site-and-sharing.md)
3. [构建、部署与测试](./05-03-build-deployment-and-testing.md)

## 5. 上游调用者

核心知识模型、全部 Go handler、Web 开发/生产运行时、CLI、AI/音频/捕获/同步模块和静态站点访问者。

## 6. 下游依赖

操作系统文件权限与用户目录、SQLite、Go embed、Vinext/Vite/Wrangler、pnpm、Go toolchain、rsync、浏览器缓存和可选 Cloudflare 环境。

## 7. 核心数据结构

- SQLite 业务表与 `documents_fts`；
- `databaseManager.connections/active`；
- `workspace.json`；
- `buildManifest{sidebar, files}`；
- `frontend_dist` 嵌入文件系统；
- `public-site/.mdocman-manifest.json`；
- PWA manifest 与 Service Worker cache。

## 8. 主要处理流程

```text
源码
→ pnpm/Vinext 构建与全量预渲染
→ dist/client + prerendered index
→ 临时复制到 frontend_dist
→ go build mdoc
→ go build mdocman-site
→ 清理临时嵌入文件
```

运行时：

```text
mdoc
→ ~/.mdoc 工作区和 SQLite
→ 同源 API + 嵌入式前端
→ 可选生成 public-site
→ mdocman-site 或 rsync 发布
```

## 9. 配置与扩展方式

- 环境变量：`API_PORT`、`SITE_PORT`、`SITE_DIR`、`DEPLOY_TARGET`、预览相关变量。
- `.openai/hosting.json` 提供可选 D1/R2 binding 名，当前均为 `null`。
- `vite.config.ts` 组合 Vinext、Sites 打包插件和 Cloudflare Vite 插件。
- 预览主题由 `themes/default.css` 等主题文件与 `previewThemes` 登记。

## 10. 代码入口

- 数据：`openDBAt`、`newDatabaseManager`。
- 渲染：`newMarkdown`、`server.render`。
- 静态构建：`server.build`、`server.share`。
- 管理端前端交付：`embeddedFrontendHandler`。
- 只读站点：`cmd/mdocman-site/main.go:main`。
- 构建：`build.sh`；开发：`dev.sh`；部署：`deploy.sh`。

## 11. 设计特点

- 主数据库和附件完全本地，生产管理端可只分发一个二进制。
- 前端开发和生产交付路径不同，但共享同一相对 API 协议。
- 静态站点通过内容哈希与导航哈希增量重建。
- 预览主题和脚本嵌入 Go 二进制，开发时可从磁盘热读主题。
- 只读发布端不依赖 SQLite。

## 12. 潜在维护风险

- 数据分布在 `~/.mdoc`、项目相对 `data/sync` 和 `public-site`，备份/迁移边界不统一。
- 上传和音频目录由同一工作区中的多个 `.db` 共享，切换数据库不隔离文件。
- 构建脚本临时改写 `frontend_dist`，依赖 trap 清理；中断和并行构建需要谨慎。
- PWA 只缓存 shell，离线时 API 数据不可用，不能视为完整离线编辑。
- Cloudflare 前端部署与本地 Go 后端的拓扑没有在代码中闭合。

## 13. 相关文档

- [整体架构](../01-architecture-overview.md)
- [笔记层级与元数据](../02-core-knowledge-model/02-01-note-hierarchy-and-metadata.md)
- [Git 备份与内容迁移](../04-automation-and-integrations/04-03-git-backup-and-content-transfer.md)

## 14. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `openDBAt`、`build`、`share` | 数据 schema 与静态输出 |
| `cmd/mdocman/workspace_databases.go` | `databaseManager` | 多数据库工作区 |
| `cmd/mdocman/markdown.go` | `newMarkdown` | 共享渲染器 |
| `cmd/mdocman/frontend.go` | `embeddedFrontend` | 前端嵌入与服务 |
| `cmd/mdocman-site/main.go` | `main` | 只读站点 |
| `build.sh` | 构建流程 | 单二进制打包 |
| `vite.config.ts` | 插件与代理 | Web 构建运行时 |
| `tests/reflect-core.test.ts` | 核心前端测试 | Markdown 纯逻辑验证 |
