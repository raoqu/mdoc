# 应用接口层总览

## 1. 模块职责

应用接口层把用户意图转换为核心模型操作。它包含 React/PWA 工作区、Go HTTP API、一次性 CLI、Markdown 路径预览和 Chrome 捕获扩展，并把不同入口统一到 SQLite 文档与 Markdown 内容上。

## 2. 在整体架构中的位置

该模块位于用户/外部客户端与核心内容能力之间。Web 与扩展通过 HTTP 调用 Go；CLI 与路径预览直接在 Go 进程中调用数据库或渲染能力；静态站点访问则绕过管理 API，只读取生成文件。

## 3. 对外提供的能力

- 浏览器中的完整知识库管理和编辑界面；
- JSON、multipart、ZIP、HTML、SSE 和静态文件 HTTP 接口；
- 今日笔记、搜索、正文和逻辑路径 CLI；
- 本地文件/目录 Markdown 临时预览；
- 浏览器页面捕获与离线队列。

## 4. 内部子模块

1. [Web 工作区](./03-01-web-workspace.md)
2. [HTTP API 与命令行接口](./03-02-http-api-and-cli.md)
3. [路径预览与浏览器捕获](./03-03-path-preview-and-browser-capture.md)

## 5. 上游调用者

- 本地桌面浏览器和 PWA；
- 终端用户；
- Chrome Manifest V3 扩展；
- 开发脚本、测试和静态站点访问者。

## 6. 下游依赖

- 核心知识模型和 `server` 方法；
- SQLite 与文件目录；
- Meowdown、React 与浏览器 API；
- Goldmark 预览；
- AI、同步、音频等自动化 handler。

## 7. 核心数据结构

- `WorkspaceView`：Web 导航状态。
- `DatabaseCatalog`：活动数据库与可切换数据库列表。
- `DocumentRecord`/`NotebookRecord`：API 主传输结构。
- `SearchHit`：FTS5 搜索结果。
- 捕获 envelope：URL、标题、选区、备注、截图与目标文档。
- SSE `AiStreamEvent`：AI 流式事件。

## 8. 主要处理流程

```text
用户动作
→ React 组件或 CLI/扩展入口
→ 参数验证与本地状态更新
→ HTTP handler 或直接 Go 调用
→ 核心模型/自动化模块
→ JSON、HTML、ZIP、SSE 或终端文本
```

开发时 Vite 代理让前端保持同源路径；生产时 Go 根路由直接提供内嵌前端，因此前端代码始终使用相对 `/api` URL。

## 9. 配置与扩展方式

- `API_PORT` 控制管理服务端口。
- `MDOC_PORT` 和 `MDOC_NO_BROWSER` 控制路径预览。
- Vite `server.proxy` 定义开发时后端路径。
- Chrome 扩展当前把 API 地址固定为 `http://127.0.0.1:8080/api/capture`。
- 新接口需同时更新 `main` 路由注册、handler、前端调用和必要的 CORS/大小限制。

## 10. 代码入口

- Web：`app/page.tsx:Home → ReflectWorkspace`。
- HTTP：`cmd/mdocman/main.go:main → http.Handle* → ListenAndServe`。
- CLI：`main → cliRequested → runCLI`。
- 路径预览：`main → pathPreviewArgument → servePathPreview`。
- 扩展：`extensions/mdocman-capture/service-worker.js` 的事件监听。

## 11. 设计特点

- 所有管理端浏览器请求都使用相对 URL，开发代理和生产内嵌无需改变前端代码。
- 主二进制根据参数早期分流为预览、CLI 或服务模式。
- 扩展先写本地队列再尝试发送，适合本地服务间歇运行。
- API 以资源 handler 为主，没有额外路由框架或控制器层。

## 12. 潜在维护风险

- HTTP 路由和方法判断分散在多个 handler，缺少集中式接口契约。
- Web 编排组件承担过多功能，测试目前主要覆盖纯函数和构建结果，缺少完整交互测试。
- API 无认证且允许任意 Origin，默认监听所有网卡；本地运行假设需要明确。
- Chrome 扩展端口硬编码，若 `API_PORT` 改变则无法捕获。
- CLI 错误主要打印后返回 `true`，进程退出码不会系统性反映命令失败。

## 13. 相关文档

- [核心知识模型](../02-core-knowledge-model/02-00-core-knowledge-model-overview.md)
- [自动化与外部集成](../04-automation-and-integrations/04-00-automation-and-integrations-overview.md)
- [构建、部署与测试](../05-data-publication-and-runtime/05-03-build-deployment-and-testing.md)

## 14. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `app/page.tsx` | `Home` | Web 入口 |
| `app/reflect/workspace.tsx` | `ReflectWorkspace` | 管理界面组合根 |
| `cmd/mdocman/main.go` | `main` | HTTP/CLI/预览分流与路由 |
| `cmd/mdocman/cli.go` | `runCLI` | 命令行接口 |
| `cmd/mdocman/path_preview.go` | `pathPreviewServer` | 本地路径预览 |
| `extensions/mdocman-capture/service-worker.js` | `capturePage`、`flushQueue` | 浏览器捕获入口 |
| `vite.config.ts` | `server.proxy` | 开发时接口代理 |
