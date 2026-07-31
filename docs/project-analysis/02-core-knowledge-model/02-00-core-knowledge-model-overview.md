# 核心知识模型总览

## 1. 模块职责

本模块定义墨笺中“知识”的基本形态：多个笔记本包含递归目录，目录包含 Markdown 文档；文档的标题、别名、隐私、修订号等元数据与正文共同驱动链接、标签、任务和每日笔记功能。

## 2. 在整体架构中的位置

核心知识模型位于 UI 与存储之间。React 侧用 TypeScript 类型和纯函数维护内存树并计算派生视图；Go 侧用结构体、SQL schema 与事务把同一模型持久化。它不依赖具体 AI 供应商或发布方式，但这些外围能力都消费文档模型。

## 3. 对外提供的能力

- 笔记本、递归目录和文档的读写模型；
- 单文档修订号与冲突检测；
- YAML frontmatter 中的别名和私密属性；
- Wiki 链接、反向链接、标签与重命名传播；
- 每日笔记约定与任务投影；
- FTS5 全文检索所需的标题和正文索引。

## 4. 内部子模块

1. [笔记层级与元数据](./02-01-note-hierarchy-and-metadata.md)
2. [Markdown 编辑器与链接图谱](./02-02-markdown-editor-and-link-graph.md)
3. [每日笔记与任务投影](./02-03-daily-notes-and-task-projection.md)

## 5. 上游调用者

- `ReflectWorkspace` 和 `ReflectEditor`；
- Go REST handler 与 CLI；
- 捕获、音频、AI、同步和发布模块；
- 前端命令面板、日历、标签、任务与聊天视图。

## 6. 下游依赖

- SQLite 表、外键、FTS5 虚表与触发器；
- TypeScript YAML 解析库；
- Meowdown 的 Markdown 编辑状态；
- Goldmark 的 Markdown 渲染；
- 浏览器时间、`crypto.randomUUID` 与本地状态。

## 7. 核心数据结构

| 结构 | 关键字段 | 语义 |
|---|---|---|
| `Notebook` / `NotebookRecord` | `id/title/description/accent/folders` | 一个逻辑知识库 |
| `Folder` / `FolderRecord` | `id/title/docs/children` | 可递归的组织节点 |
| `Doc` / `DocumentRecord` | `id/title/content/revision/...` | Markdown 内容与行为元数据 |
| `DocumentLocation` | document/folder/notebook | 前端派生的文档上下文 |
| `WorkspaceView` | daily/note/tasks/tag/chat/... | 前端导航状态 |

数据库中的 `position` 保留笔记本、目录和文档顺序；前端 JSON 模型不显式携带它，而由数组顺序表达。

## 8. 主要处理流程

```text
SQLite 行
→ server.load / loadFolders
→ 递归 Notebook 树 JSON
→ normalizeNotebookMetadata
→ 编辑和派生视图
→ updateDocument / updateFolders
→ 单文档或整树保存
→ SQLite + FTS5
```

标题变化时，前端延迟 800ms 将旧标题写入 `aliases`，更新 frontmatter，并重写其他笔记中的 Wiki 链接。

## 9. 配置与扩展方式

知识模型没有独立配置文件。扩展字段时必须同步修改 Go 结构体、SQL schema/迁移、查询扫描、保存 SQL、TypeScript 接口和前端归一化逻辑。新增 Markdown 派生能力应优先写成 `app/reflect/markdown.ts` 中的纯函数，并增加 `tests/reflect-core.test.ts` 覆盖。

## 10. 代码入口

后端入口是 `openDBAt`、`server.load`、`server.save`、`server.document`；前端入口是 `ReflectWorkspace` 初始加载与 `normalizeNotebookMetadata`。编辑器正文由 `ReflectEditor` 将 frontmatter 与可视编辑主体拆分后再合并。

## 11. 设计特点

- 权威正文始终是 Markdown，链接、任务、标签等是运行时投影。
- 树结构适合一次加载的小型个人知识库，避免了复杂的客户端分页和缓存协议。
- 单文档修订号提供明确的乐观并发冲突反馈。
- 别名保留旧标题，使重命名后的链接和 AI 引用仍可解析。

## 12. 潜在维护风险

- 同一概念在 Go 和 TypeScript 中重复定义，字段新增或语义变化容易不同步。
- `documents.id` 是数据库级主键，而每日笔记 ID 固定为 `daily-YYYY-MM-DD`；同一 SQLite 数据库中的多个笔记本无法各自拥有同一天的每日笔记。
- 结构性整树保存没有 revision 条件，可能覆盖另一个窗口的结构变更。
- 私密状态同时存在于数据库列和 frontmatter；导入或非前端写入需要保持两者一致。
- 标题重命名传播发生在浏览器内，处理大量文档时会遍历整个知识库。

## 13. 相关文档

- [整体架构](../01-architecture-overview.md)
- [Web 工作区](../03-application-interfaces/03-01-web-workspace.md)
- [SQLite 与本地文件存储](../05-data-publication-and-runtime/05-01-sqlite-and-local-files.md)

## 14. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `Doc`、`Folder`、`Notebook` | 后端领域传输模型 |
| `cmd/mdocman/main.go` | `openDBAt`、`loadFolders`、`save` | schema、树重建与持久化 |
| `app/reflect/types.ts` | `DocumentRecord`、`updateFolders` | 前端类型与不可变树操作 |
| `app/reflect/workspace.tsx` | `normalizeNotebookMetadata`、`persist` | 模型归一化和保存策略 |
| `app/reflect/frontmatter.ts` | `splitFrontmatter`、`upsertFrontmatter` | YAML 元数据处理 |
| `app/reflect/markdown.ts` | 链接、任务和标签纯函数 | Markdown 派生语义 |
