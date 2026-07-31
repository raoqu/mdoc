# Git 备份与内容迁移

## 1. 功能说明

该子模块把 SQLite 知识库转换为可读的 Markdown/资源目录并同步到 Git 远端，同时提供普通 Markdown 导入、ZIP 导出和自定义模板管理。

## 2. 职责边界

Git 同步是备份与双向内容回写，不是实时协作协议。普通导入导出不保留全部内部元数据；模板独立于普通文档，但在 Git 投影中一起备份。

## 3. 所属上级模块

[自动化与外部集成](./04-00-automation-and-integrations-overview.md)。

## 4. 对外接口

- `GET/POST/DELETE /api/sync`；
- `POST /api/sync/run?notebookId=...`；
- `POST /api/import`、`GET /api/export`；
- `/api/templates` 与 `/api/templates/{id}`；
- 设置页中的同步、模板和导出操作。

## 5. 主要实现组成

- `syncConfig` 保存远端、分支、状态和自动同步。
- `exportSyncProjection` 重建 `notes/`、`daily/`、`templates/`、`assets/` 和 `.mdocman/manifest.json`。
- `performSync` 初始化仓库、配置远端、提交、fetch、merge、导入并 push。
- manifest 冲突按 `kind + id` 合并；正文冲突标记保留给用户。
- `importMD` 根据上传相对路径创建目录树；`exportMD` 按知识库目录结构写 ZIP。
- `templates` handlers 提供知识库作用域的 CRUD。

## 6. 输入与输出

Git 输入知识库 ID、远端、分支、可选令牌和私密备份确认；输出仓库投影、提交和同步状态。普通导入输入 multipart Markdown 文件及相对路径，输出导入数量；导出输出 ZIP。

## 7. 处理流程

```text
保存完成
→ 前端可选 30 秒防抖 triggerSync
→ performSync 全局互斥
→ exportSyncProjection
→ git add/commit
→ fetch/merge
→ manifest 冲突合并、正文冲突保留
→ importSyncProjection
→ git push
→ sync_configs 状态更新
```

导入同步投影时，缺失目录会回退到知识库第一个目录；Markdown 第一个 H1 可覆盖 manifest 标题，frontmatter 决定 private 列。

## 8. 依赖关系

上游是工作区自动保存和设置页；下游是 SQLite、文件系统、OS Keychain、`git` 命令、远端网络和 ZIP/multipart 标准库。

## 9. 配置项

- 默认分支 `main`，支持 HTTPS、SSH、scp 风格和本地相对/绝对 remote。
- Git 令牌以 `mdocman-git` 服务名保存在 Keychain。
- 单个同步资源达到 95 MiB 时跳过复制。
- 同步工作副本路径为项目相对的 `data/sync/{safeNotebookId}`。
- 前端可启用 `autoSync`；保存后延迟 30 秒触发。

## 10. 错误处理

同步请求最长 3 分钟。网络相关错误映射为 `offline`，其他错误为 `failed`。合并失败时尝试合并 manifest，并提交包含冲突标记的结果；检测到标记后状态为 `needs_review`。Git 输出会进入错误文本。

## 11. 扩展与修改建议

- 将同步仓库移入工作区目录或显式可配置路径，避免依赖启动工作目录。
- 对 manifest 引入 schema 版本迁移和校验，避免静默忽略不兼容内容。
- 明确远端删除如何回写；当前 manifest 驱动导入不会系统性删除 SQLite 中已移除条目。
- 普通导入应处理 ID 冲突、重复文件和 frontmatter 元数据映射。
- 模板/附件导入中的忽略错误应改为可观察的逐项结果。

## 12. 代码入口与调用链

```text
ReflectWorkspace.persist
→ triggerSync
→ POST /api/sync/run
→ syncRun
→ performSync
→ exportSyncProjection
→ runGit(fetch/merge/push)
→ importSyncProjection
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/sync.go` | `syncSettings`、`syncRun` | 配置与执行 API |
| `cmd/mdocman/sync.go` | `exportSyncProjection`、`importSyncProjection` | SQLite/Markdown 双向映射 |
| `cmd/mdocman/sync.go` | `performSync` | Git 控制流 |
| `cmd/mdocman/sync.go` | `mergeManifestSides`、`hasGitConflicts` | 冲突处理 |
| `app/reflect/sync-settings.tsx` | `SyncSettings` | 配置和手动同步 UI |
| `cmd/mdocman/main.go` | `importMD`、`exportMD` | 普通 Markdown 导入导出 |
| `cmd/mdocman/templates.go` | `templates`、`template` | 模板 CRUD |
| `cmd/mdocman/sync_test.go` | 同步测试 | manifest 合并和 Git 初始化验证 |

