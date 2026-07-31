# 笔记层级与元数据

## 1. 功能说明

该子模块把 SQLite 的扁平行重建为笔记本、递归目录和文档树，并把浏览器中的结构修改重新保存。文档还携带固定、废纸篓、私密、别名、创建/更新时间和 revision。

## 2. 职责边界

它负责内容对象的身份、归属、排序和元数据，不负责 Markdown 可视编辑、AI 调用或 HTML 渲染。附件和音频文件只通过正文 URL 与文档关联，并不成为树节点。

## 3. 所属上级模块

[核心知识模型](./02-00-core-knowledge-model-overview.md)。

## 4. 对外接口

- `GET /api/notebooks`：返回完整笔记树。
- `PUT /api/notebooks`：保存结构性变更后的完整树。
- `GET /api/documents/{id}`：读取单篇文档。
- `PUT /api/documents/{id}`：带 revision 的单篇更新。
- TypeScript `documentsInNotebook`、`updateDocument`、`removeDocument`。

## 5. 主要实现组成

- `openDBAt` 创建表、增量添加文档列并建立 FTS5。
- `loadFolders` 先收集目录，再按 `parent_id` 构造递归树，并把文档挂到目录。
- `save` 在事务内重建笔记层级，并尽力恢复依赖于文档/笔记本的辅助记录。
- `databaseManager` 让同一进程在 `~/.mdoc/*.db` 之间切换。
- `normalizeNotebookMetadata` 用 frontmatter 补充别名和私密标记。

## 6. 输入与输出

输入是 SQL 行、前端完整树或单文档 JSON；输出是有序树、持久化后的文档和 409 冲突响应。数组下标被转换为数据库的 `position`。

## 7. 处理流程

### 整树读取

```text
SELECT notebooks ORDER BY position
→ 每个 notebook 查询 folders 和 documents
→ 以 folder id 建索引
→ 按 parent_id 建 children
→ 生成根目录列表
→ JSON 返回浏览器
```

### 单文档更新

```text
incoming revision
→ UPDATE ... SET revision=revision+1
  WHERE id=? AND revision=?
→ 成功返回新记录
→ 无匹配时读取当前记录并返回 409
```

## 8. 依赖关系

上游是 Web 保存调度、CLI、捕获、音频、同步与 AI。下游是 `database/sql` 和 SQLite。`shares`、`templates`、聊天、捕获令牌和音频记录通过外键或业务引用依赖笔记树。

## 9. 配置项

- 工作区目录固定为当前用户的 `~/.mdoc/`。
- 活动数据库记录在 `~/.mdoc/workspace.json`。
- 默认数据库名是 `mdocman.db`。
- 数据库文件与目录权限分别设置为 `0600` 和 `0700`。
- SQLite 连接启用外键和 WAL。

## 10. 错误处理

数据库打开、schema 初始化和迁移失败会阻止应用启动。API 将无效 JSON、缺失文档、版本冲突和 SQL 错误分别映射为 400、404、409 和 500。整树保存使用事务回滚主流程，但恢复辅助记录时使用 `INSERT OR IGNORE` 且忽略单条错误。

## 11. 扩展与修改建议

- 新增文档字段时建立显式、可追踪的 schema 版本，而不是继续追加“忽略 duplicate column”的迁移列表。
- 为结构更新增加知识库级版本或事务化变更 API，减少整树重建。
- 若保留同一数据库内多笔记本，应把每日笔记等约定 ID 加入 notebook 命名空间，或改为复合唯一键。
- 明确数据库列与 frontmatter 的权威顺序，并在所有导入路径统一归一化。

## 12. 代码入口与调用链

```text
main
→ newDatabaseManager
→ activate / openDBAt
→ server.load
→ notebooks(GET)

ReflectWorkspace.persist
→ document(PUT) 或 notebooks(PUT)
→ documentByID 或 save
→ SQLite
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `openDBAt` | 表、索引、迁移和 FTS5 |
| `cmd/mdocman/main.go` | `load`、`loadFolders`、`save` | 树的序列化与反序列化 |
| `cmd/mdocman/main.go` | `document` | revision 乐观并发 |
| `cmd/mdocman/workspace_databases.go` | `databaseManager` | 数据库创建、切换和连接缓存 |
| `app/reflect/types.ts` | `updateFolders`、`updateDocument` | 递归不可变更新 |
| `app/reflect/workspace.tsx` | `mutateNotebooks`、`persist` | dirty 分类和保存分派 |

