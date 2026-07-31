# HTTP API 与命令行接口

## 1. 功能说明

Go 管理端通过标准库 `net/http` 提供数据、搜索、自动化、预览、导入导出、发布和文件服务；同一主程序还提供面向终端的一次性查询命令。

## 2. 职责边界

本子模块负责协议、方法分派、参数校验和响应格式。具体 AI、同步、音频、捕获、渲染与数据规则由对应 `server` 方法实现。

## 3. 所属上级模块

[应用接口层](./03-00-application-interfaces-overview.md)。

## 4. 对外接口

| 路径 | 方法 | 主要职责 |
|---|---|---|
| `/api/databases` | GET/POST/PUT | 列举、新建、切换 SQLite 数据库 |
| `/api/notebooks` | GET/PUT | 读取或保存完整笔记树 |
| `/api/documents/{id}` | GET/PUT | 单文档读取与乐观并发更新 |
| `/api/search` | GET | FTS5 搜索 |
| `/api/ai/*` | 多种 | 供应商、选区改写、聊天和历史 |
| `/api/templates*` | GET/POST/PUT/DELETE | 模板管理 |
| `/api/capture*` | 多种 | 捕获令牌和网页捕获 |
| `/api/audio-memos*` | GET/POST | 录音上传、列表和转录 |
| `/api/sync*` | GET/POST/DELETE | Git 配置与执行 |
| `/api/assets/describe` | POST | 资源 AI 描述 |
| `/api/preview/{id}` | GET/POST | 已保存或未保存正文预览 |
| `/api/upload`、`/api/import`、`/api/export` | 多种 | 文件与 Markdown 迁移 |
| `/api/build`、`/api/share` | POST | 静态站点和分享页 |
| `/uploads/`、`/audio/`、`/site/`、`/s/` | GET/HEAD | 本地文件服务 |
| `/` | GET/HEAD | 嵌入式前端和 SPA fallback |

CLI 命令包括 `serve`、`today`、`search <query>`、`show <id-or-title>`、`path <id-or-title>` 和帮助命令。

## 5. 主要实现组成

- `main` 使用全局默认 `http.ServeMux` 注册 handler。
- `cors` 统一添加跨域响应头并处理 OPTIONS。
- `jsonOut` 统一 JSON 输出。
- 各 handler 自行进行 HTTP method switch 与路径后缀解析。
- CLI 直接调用活动数据库，不经过 HTTP。

## 6. 输入与输出

HTTP 输入包括 JSON、query 参数、路径参数、表单和 multipart；输出包括 JSON、SSE、ZIP、HTML、二进制附件和状态码。CLI 输入来自 `os.Args`，输出写入 stdout/stderr。

## 7. 处理流程

```text
main
→ 初始化工作区与活动数据库
→ cliRequested
→ 若命令已处理则退出
→ 注册 handler
→ ListenAndServe
```

Web 开发服务器将后端相关路径代理到 `127.0.0.1:${API_PORT}`；生产二进制由 Go 同源提供前端和 API。

## 8. 依赖关系

上游是 Web、扩展、终端和测试；下游是所有核心/集成模块。CLI 搜索复用 AI 模块中的 `ftsExpression`，体现包内函数级共享。

## 9. 配置项

- `API_PORT` 默认 `8080`。
- HTTP 服务地址由 `http.ListenAndServe(":"+port)` 决定。
- 上传和音频请求限制为 32 MiB，捕获限制为 16 MiB，预览表单限制为 8 MiB。
- CORS 允许 `*` Origin、`Content-Type/Authorization` 和常用写方法。

## 10. 错误处理

handler 主要以纯文本 `http.Error` 返回 4xx/5xx；成功 JSON 没有统一 envelope。SSE 在流已开始后用 `type=error` 事件传递供应商错误。CLI 将错误打印到 stderr，但大多数命令处理后正常返回到 `main`，未显式设置非零退出码。

## 11. 扩展与修改建议

- 建立路由与请求/响应契约表或 OpenAPI，避免前后端依赖隐式字符串。
- 统一 JSON 错误结构并记录请求上下文。
- 对管理 API 增加本地访问约束、认证或更严格 CORS，尤其是服务监听非 loopback 时。
- 为 CLI 返回明确 error/exit code，便于脚本化使用。
- 路由增长后可引入局部 router，但无需改变本地单体部署。

## 12. 代码入口与调用链

```text
cmd/mdocman/main.go:main
→ pathPreviewArgument（路径模式优先）
→ databaseManager
→ cliRequested / runCLI
→ http.HandleFunc / http.Handle
→ http.ListenAndServe
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `main` | 全部管理端路由注册 |
| `cmd/mdocman/main.go` | `cors`、`jsonOut` | 公共 HTTP 辅助 |
| `cmd/mdocman/cli.go` | `runCLI`、`cliRequested` | CLI 命令分派 |
| `cmd/mdocman/frontend.go` | `isBackendPath` | 防止 SPA fallback 遮蔽后端路径 |
| `vite.config.ts` | `server.proxy` | 开发代理 |
| `dev.sh` | Go 与 Vinext 启动流程 | 本地双进程开发入口 |

