# 项目概述

## 1. 项目定位

墨笺（Mdocman）是一个本地优先的 Markdown 知识库应用。它把笔记正文保持为 Markdown，把笔记本、目录、文档元数据、全文索引和扩展功能状态存入 SQLite，并通过 React 工作区提供类似关联式笔记工具的编辑体验。

项目不是单一前端或单一后端：主产品由 Go 管理端与 TypeScript/React 前端组成，另外还包含只读静态站点服务、Markdown 路径预览、命令行查询、Chrome 捕获扩展、Cloudflare Worker 前端入口以及 AI、Git、音频等外部集成。

## 2. 主要功能

- 管理多个独立 SQLite 知识库，并在 `~/.mdoc/` 中创建和切换 `.db` 文件。
- 维护“笔记本 → 递归目录 → 文档”的层级，支持固定、废纸篓、私密标记、别名和修订号。
- 使用 Meowdown 编辑 Markdown，提供 Wiki 链接、反向链接、标签、模板、附件、图片尺寸和快捷命令。
- 自动创建每日笔记，并从 Markdown `+ [ ]` 项投影出跨文档任务视图。
- 使用 SQLite FTS5 同时服务 Web 命令面板、CLI 搜索和 AI 上下文检索。
- 对接 OpenAI、Anthropic、Google 和 OpenRouter，支持选区改写、知识库问答、资源描述与音频转录。
- 通过浏览器扩展捕获网页标题、URL、选区、备注和截图，并在本地服务不可用时排队重试。
- 将知识库投影为 Git 仓库进行备份，也支持 Markdown 目录导入和 ZIP 导出。
- 使用 Goldmark 生成实时预览、分享页和增量静态站点，再由独立只读 Go 服务托管。

## 3. 目标用户或调用者

主要用户是希望在本机管理 Markdown 知识库、每日记录、任务和资料摘录的个人用户。直接调用者包括：

- 浏览器中的 React/PWA 管理界面；
- `mdoc`/`mdocman` 命令行用户；
- Chrome Manifest V3 捕获扩展；
- 静态站点访问者；
- AI 模型供应商 API 和 Git 远端；
- 开发时的 Vinext/Vite、Wrangler 与测试工具链。

## 4. 输入与输出

| 类别 | 输入 | 输出 |
|---|---|---|
| 编辑 | Markdown、YAML frontmatter、附件、Wiki 链接 | SQLite 文档记录、附件文件、编辑器视图 |
| 查询 | 搜索词、笔记 ID/标题、视图命令 | FTS5 搜索结果、正文、任务/标签/反向链接投影 |
| 捕获 | 网页 URL、标题、选区、截图、令牌 | 每日笔记块、专用捕获文档、上传图片 |
| AI | 选区、问题、公开笔记、资源或音频 | SSE 文本流、聊天记录、资源描述、转录文档 |
| 同步 | SQLite 中的知识库与远端 Git 配置 | Markdown/资源投影、提交、合并结果、回写后的笔记 |
| 发布 | 笔记树、附件、侧栏选项 | `public-site/` 静态 HTML、分享 URL、只读站点 |
| 预览 | 数据库文档或本地 Markdown 路径 | 本机临时 HTTP 预览页 |

## 5. 技术栈

- 后端与本地运行时：Go 1.24、`net/http`、`database/sql`、`modernc.org/sqlite`。
- Markdown 渲染：Goldmark、GFM/脚注/Typographer 扩展、Chroma 代码高亮、浏览器端 Mermaid。
- 前端：TypeScript 5.9、React 19、Next.js 16 API 兼容层、Vinext、Vite 8、Meowdown、Lucide。
- 本地数据：SQLite WAL、FTS5、普通文件目录、JSON 工作区状态。
- 凭据与外部进程：系统 Keychain、`git` 可执行文件、AI 供应商 HTTP API。
- 替代前端运行时：Cloudflare Worker、可选 D1/Drizzle 脚手架；当前核心数据仍由 Go API 提供。
- 构建与包管理：pnpm 11、Bash、Go build、Vinext 预渲染。

## 6. 项目核心组成

| 逻辑模块 | 核心职责 | 主要实现 |
|---|---|---|
| 核心知识模型 | 笔记层级、Markdown 元数据、链接、任务和每日笔记 | `cmd/mdocman/main.go`、`app/reflect/types.ts`、`app/reflect/markdown.ts` |
| 应用接口层 | Web 工作区、REST、CLI、路径预览、浏览器扩展 | `app/reflect/workspace.tsx`、`cmd/mdocman/cli.go`、`cmd/mdocman/path_preview.go` |
| 自动化与集成 | AI、资源描述、音频转录、Git 备份、内容迁移 | `cmd/mdocman/ai.go`、`assets.go`、`audio.go`、`sync.go` |
| 数据与发布基础设施 | 多数据库管理、附件目录、渲染、静态发布、嵌入式交付 | `workspace_databases.go`、`frontend.go`、`build.sh`、`cmd/mdocman-site/main.go` |

项目是一个仓库内的多入口混合应用，但不是多个独立业务微服务。Go 管理端拥有数据和业务规则，React 前端负责交互与一部分 Markdown 派生逻辑；只读发布端和浏览器扩展是外围适配器。

## 7. 核心运行流程

### 7.1 管理端启动

```text
cmd/mdocman/main.go:main
→ newMarkdown
→ 判断是否进入路径预览模式
→ defaultWorkspaceDirectory
→ migrateLegacyWorkspace
→ newDatabaseManager / openDBAt
→ migrateJSON
→ 判断是否执行 CLI 命令
→ 注册 REST、文件和嵌入式前端路由
→ http.ListenAndServe
```

### 7.2 笔记编辑与保存

```text
MeowdownEditor
→ ReflectWorkspace.changeDocument
→ 浏览器内 NotebookRecord 树
→ 800ms 防抖 persist
→ 单文档 PUT /api/documents/{id}
→ revision 条件更新
→ SQLite + FTS5 触发器
```

目录或笔记本结构变化则走 `PUT /api/notebooks`，后端在事务中重建层级记录并恢复关联数据；这条路径与单文档的乐观并发路径不同。

### 7.3 发布

```text
POST /api/build
→ server.load
→ Goldmark/Chroma 渲染
→ 内容与导航哈希比较
→ 增量写入 public-site/
→ 管理端 /site/ 预览或 mdocman-site 只读托管
```

## 8. 文档阅读建议

- 首次了解项目：先读[整体架构](./01-architecture-overview.md)，再读[核心知识模型总览](./02-core-knowledge-model/02-00-core-knowledge-model-overview.md)。
- 只关注产品功能：阅读[Web 工作区](./03-application-interfaces/03-01-web-workspace.md)、[Markdown 编辑器与链接图谱](./02-core-knowledge-model/02-02-markdown-editor-and-link-graph.md)和[每日笔记与任务投影](./02-core-knowledge-model/02-03-daily-notes-and-task-projection.md)。
- 只关注架构与运行：阅读[应用接口层总览](./03-application-interfaces/03-00-application-interfaces-overview.md)及[数据、发布与运行基础设施总览](./05-data-publication-and-runtime/05-00-data-publication-and-runtime-overview.md)。
- 修改前端交互：从 `app/reflect/workspace.tsx` 和 `app/reflect/reflect-editor.tsx` 开始。
- 修改数据模型或 API：从 `cmd/mdocman/main.go:openDBAt`、`server.load/save` 和 `main` 中的路由注册开始。
- 修改外部集成：分别进入 `ai.go`、`audio.go`、`assets.go`、`capture.go` 或 `sync.go`，并同步检查对应前端设置组件与测试。

## 9. 分析边界与验证

本次忽略依赖缓存、构建产物、用户数据库、录音、上传文件和已生成站点内容。代码静态分析之外，执行了 `go test ./...` 和 `pnpm exec tsx --test tests/reflect-core.test.ts`，两组测试均通过。AI 供应商、操作系统 Keychain、真实 Git 远端和 Cloudflare 部署未进行在线端到端验证。

## 10. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `main`、`openDBAt`、`server.load/save` | 主入口、数据库结构、HTTP 路由与核心持久化 |
| `app/reflect/workspace.tsx` | `ReflectWorkspace`、`persist` | 前端状态编排、导航、保存和功能聚合 |
| `app/reflect/reflect-editor.tsx` | `ReflectEditor` | Markdown 编辑器与 AI/附件/链接扩展 |
| `cmd/mdocman/workspace_databases.go` | `databaseManager` | `~/.mdoc` 多数据库生命周期 |
| `cmd/mdocman/ai.go` | `aiChat`、`aiTransform` | AI 供应商和流式处理 |
| `cmd/mdocman/sync.go` | `performSync` | Git 备份投影、合并与回写 |
| `cmd/mdocman/path_preview.go` | `servePathPreview` | 本地 Markdown 文件/目录预览入口 |
| `cmd/mdocman-site/main.go` | `main` | 只读静态站点服务入口 |
| `build.sh` | 构建脚本 | 前端预渲染、资源嵌入与两个 Go 二进制构建 |

