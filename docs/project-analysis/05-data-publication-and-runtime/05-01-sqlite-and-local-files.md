# SQLite 与本地文件存储

## 1. 功能说明

该子模块以 SQLite 保存结构化状态，以普通文件保存附件、录音、活动数据库选择、Git 投影和静态输出。SQLite 是笔记和业务元数据的权威来源。

## 2. 职责边界

它不解释 Markdown 链接或任务语义，只保存正文并维护全文索引。AI 密钥和 Git 令牌不进入数据库，而交给系统 Keychain。

## 3. 所属上级模块

[数据、发布与运行基础设施](./05-00-data-publication-and-runtime-overview.md)。

## 4. 对外接口

- `openDBAt(path)`；
- `databaseManager.current/activate/createAndActivate/catalog`；
- `server.database/workspacePath/uploadsDir/audioMemosDir`；
- `migrateLegacyWorkspace`；
- SQL 查询、事务和 FTS5。

## 5. 主要实现组成

| 存储 | 内容 |
|---|---|
| `notebooks` | 笔记本显示与排序 |
| `folders` | 递归目录和排序 |
| `documents` | Markdown、元数据、revision |
| `documents_fts` + triggers | 标题/正文全文索引 |
| `shares` | 文档到分享 token |
| `ai_providers` | 非秘密的 AI 配置 |
| `chat_conversations/messages` | 对话历史 |
| `templates` | 自定义模板 |
| `capture_tokens` | 捕获令牌摘要 |
| `audio_memos` | 录音文件与转录状态 |
| `sync_configs` | Git 远端和状态 |

## 6. 输入与输出

输入是数据库文件路径、SQL 参数和运行文件；输出是连接、事务结果、活动数据库 catalog 和本地文件 URL。SQLite 查询值被扫描到 Go 结构，再编码为 JSON。

## 7. 处理流程

```text
应用启动
→ ~/.mdoc 目录权限 0700
→ 检查旧 data/
→ 必要时 VACUUM INTO 复制旧数据库
→ 复制缺失 uploads/audio/notebooks.json
→ 读取 workspace.json
→ 选择或创建活动 .db
→ openDBAt
→ schema + 列迁移 + FTS5
```

`databaseManager` 缓存已打开数据库连接，切换时更新 `workspace.json`，进程退出时统一关闭。

## 8. 依赖关系

上游是所有 Go 业务模块；下游是 `modernc.org/sqlite`、用户目录、文件权限和 SQLite FTS5。Keychain 与数据库只通过 provider ID 或 credential account 字符串关联。

## 9. 配置项

- 主目录：`~/.mdoc/`，代码中没有覆盖环境变量。
- 默认库：`mdocman.db`；活动库：`workspace.json`。
- SQLite DSN 启用 `foreign_keys(1)` 与 `journal_mode(WAL)`。
- 上传：`~/.mdoc/uploads/`；音频：`~/.mdoc/audio-memos/`。
- Git 工作副本：`data/sync/`；静态站点：`public-site/`，后二者相对当前工作目录。

## 10. 错误处理

schema 或非重复列迁移错误会关闭连接并失败。数据库名拒绝路径、隐藏名、控制字符和过长输入。`workspace.json` 使用同目录临时文件加 rename 原子替换。旧数据库使用 `VACUUM INTO` 形成一致快照。

## 11. 扩展与修改建议

- 引入 schema 版本表和顺序迁移，记录已应用版本。
- 统一所有运行数据到工作区根下，或把路径作为显式配置。
- 将附件按数据库或笔记本命名空间隔离，避免切库后文件混用和清理困难。
- 为数据库切换与并发请求定义一致性语义；当前旧连接保留到进程退出。
- 对整树保存恢复辅助表时的忽略错误增加校验和日志。

## 12. 代码入口与调用链

```text
main
→ defaultWorkspaceDirectory
→ migrateLegacyWorkspace
→ newDatabaseManager
→ databaseFiles / savedActiveDatabase
→ activate
→ openDBAt
→ server.database
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/main.go` | `openDBAt` | schema、迁移、FTS5 |
| `cmd/mdocman/main.go` | `workspacePath`、`uploadsDir`、`audioMemosDir` | 运行文件路径 |
| `cmd/mdocman/workspace_databases.go` | `databaseManager` | 多库连接与活动状态 |
| `cmd/mdocman/workspace_databases.go` | `migrateLegacyWorkspace` | 旧 `data/` 迁移 |
| `cmd/mdocman/workspace_databases.go` | `copySQLiteSnapshot` | 一致数据库快照 |
| `cmd/mdocman/workspace_databases_test.go` | 管理器与迁移测试 | 文件、切换和迁移验证 |

