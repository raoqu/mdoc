# 构建、部署与测试

## 1. 功能说明

该子模块定义前后端开发、生产预渲染、单二进制嵌入、只读站点构建、Cloudflare 前端产物、PWA shell、远端静态同步和自动化测试。

## 2. 职责边界

它负责把源代码变成可运行产物，不管理用户数据迁移之外的业务流程。`deploy.sh` 只部署已生成静态站点，不部署管理端二进制。

## 3. 所属上级模块

[数据、发布与运行基础设施](./05-00-data-publication-and-runtime-overview.md)。

## 4. 对外接口

- `./dev.sh`、`./dev.sh <路径>`；
- `./build.sh`、`./clean.sh`；
- `pnpm run dev|build|test|lint`；
- `go test ./...`；
- `DEPLOY_TARGET=... ./deploy.sh`；
- `SITE_PORT`/`SITE_DIR` 启动 `mdocman-site`。

## 5. 主要实现组成

- `dev.sh` 启动 Go 后端，轮询 `/api/notebooks` 健康状态，再启动 Vinext HMR。
- `build.sh` 执行 Vinext 全量预渲染，复制 client 和 index 到 `frontend_dist`，构建两个 Go 二进制，并通过 trap 清理临时文件。
- `frontend.go` 使用 `go:embed all:frontend_dist` 并提供 SPA fallback。
- `vite.config.ts` 组合 Vinext、Cloudflare 和 Sites 元数据插件。
- `worker/index.ts` 提供 Vinext App Router 与 Cloudflare 图片优化。
- PWA Service Worker 缓存 shell 资源，导航离线时回退到 `/`。

## 6. 输入与输出

输入是 Go/TypeScript 源码、依赖锁文件、主题和构建配置；输出是 `mdoc`、`dist/bin/mdocman-site`、`dist/client`、`dist/server`、`dist/.openai` 和可部署的 `public-site/`。

## 7. 处理流程

```text
pnpm exec vinext build --prerender-all
→ dist/server/prerendered-routes/index.html
→ dist/client/**
→ cp 到 cmd/mdocman/frontend_dist
→ go build ./cmd/mdocman → ./mdoc
→ go build ./cmd/mdocman-site → dist/bin/mdocman-site
→ trap 清理 frontend_dist 临时内容
```

开发时前端 `/api`、`/_mdoc`、`/site`、`/uploads`、`/s` 由 Vite 代理到 Go；生产时同一路径由 Go 直接处理。

## 8. 依赖关系

上游是开发者和发布流程；下游是 Go 1.24、Node 22、pnpm 11、Vinext/Vite、Wrangler、Cloudflare 插件、rsync 和操作系统 shell 工具。

## 9. 配置项

- `package.json` 锁定主要前端依赖版本和脚本。
- `go.mod` 定义 Go 1.24 与渲染/SQLite/Keychain 依赖。
- `MDOC_DEV_HMR=1` 或 Seatbelt 环境使用 Vite polling。
- `.openai/hosting.json` 当前 `d1`、`r2` 为 `null`。
- `tsconfig.json` 只包含应用目录的 TypeScript，排除 worker、db、build 等 Cloudflare/构建源码。

## 10. 错误处理

Shell 脚本使用 `set -Eeuo pipefail`，在缺少 Go/pnpm/rsync、端口占用、产物缺失或构建失败时退出。`dev.sh` 清理后端子进程；`build.sh` 无论成功失败都清理临时嵌入目录。

## 11. 扩展与修改建议

- 在 CI 中分别执行 Go 测试、TypeScript 纯逻辑测试、lint 和完整生产构建；仓库当前未包含 `.github` 工作流。
- 将 Go 测试包扫描限制在项目包，避免 `go test ./...` 意外发现 `node_modules` 中的 Go 源码。
- 为 Worker/Vite 构建源码建立独立 TypeScript 配置，当前主 `tsconfig` 显式排除它们。
- 明确 Cloudflare 部署只承载前端时 `/api` 的目标拓扑；当前生产 Worker 路径没有本地 Go API 等价实现。
- 对 `deploy.sh --delete` 保持目标路径校验和发布前预览，因为它会删除远端多余文件。

## 12. 代码入口与调用链

```text
开发：dev.sh
→ go run ./cmd/mdocman
→ 健康轮询
→ pnpm run dev

生产：build.sh
→ vinext build
→ 复制前端
→ go build 两个入口

静态部署：POST /api/build
→ public-site
→ deploy.sh / mdocman-site
```

## 13. 测试结构与当前验证

- Go 测试覆盖 AI 辅助逻辑、资源分类、音频回链、捕获幂等、前端嵌入、路径预览、数据库管理和 Git 同步。
- `tests/reflect-core.test.ts` 覆盖 frontmatter、冲突标记、任务、Wiki 重命名、图片元数据和月份网格。
- `tests/rendered-html.test.mjs` 在 Vinext 构建后检查服务端渲染 shell 和关键迁移能力。
- 本次分析执行 `go test ./...` 与 `pnpm exec tsx --test tests/reflect-core.test.ts`，均通过；未执行会重建 `dist/` 的完整 `pnpm test`，以避免覆盖工作区现有构建产物。

## 14. 关键源代码位置

| 路径 | 作用 |
|---|---|
| `dev.sh` | 双进程开发和路径预览分流 |
| `build.sh` | 预渲染、资源嵌入和二进制构建 |
| `deploy.sh` | rsync 增量静态部署 |
| `clean.sh` | 构建产物与缓存清理 |
| `vite.config.ts` | 开发代理和 Cloudflare/Vinext 插件 |
| `build/sites-vite-plugin.ts` | 打包 `.openai` 与 Drizzle 元数据 |
| `worker/index.ts` | Cloudflare Worker 入口 |
| `cmd/mdocman/frontend.go` | 嵌入前端文件服务 |
| `public/sw.js` | PWA shell 缓存 |
| `package.json`、`go.mod` | 运行时和依赖契约 |
| `cmd/mdocman/preview_test.go`、`tests/reflect-core.test.ts`、`tests/rendered-html.test.mjs` | 自动化测试 |
