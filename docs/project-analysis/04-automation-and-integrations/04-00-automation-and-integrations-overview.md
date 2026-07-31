# 自动化与外部集成总览

## 1. 模块职责

本模块把本地 Markdown 知识库连接到 AI 供应商、麦克风、浏览器捕获、操作系统 Keychain、Git 远端和文件迁移流程。其目标是在不改变 SQLite/Markdown 权威地位的前提下增加自动处理与跨系统流转。

## 2. 在整体架构中的位置

自动化模块位于 Go 应用服务层，通常由 Web API 触发，再读取活动 SQLite 数据库和本地文件。它们不是独立服务，也没有消息队列；长操作在当前 HTTP 请求内同步执行，AI 文本输出通过 SSE 流式返回。

## 3. 对外提供的能力

- 多 AI 供应商配置与密钥托管；
- 私密感知的选区改写和知识库问答；
- 图片、SVG、PDF 的 AI 描述；
- 浏览器录音上传与 OpenAI/Google 转录；
- Chrome 页面捕获；
- Git 备份、合并、冲突保留和回写；
- Markdown 目录导入、ZIP 导出与笔记模板。

## 4. 内部子模块

1. [AI 与资源增强](./04-01-ai-and-asset-enrichment.md)
2. [音频备忘与转录](./04-02-audio-memos-and-transcription.md)
3. [Git 备份与内容迁移](./04-03-git-backup-and-content-transfer.md)

浏览器捕获作为外部入口单独记录在[路径预览与浏览器捕获](../03-application-interfaces/03-03-path-preview-and-browser-capture.md)。

## 5. 上游调用者

- Web 编辑器选区菜单与 Chat 页面；
- 设置页中的 AI、资源描述、模板、捕获和 Git 面板；
- 音频录制面板；
- Chrome 扩展；
- 用户主动导入、导出或发布操作。

## 6. 下游依赖

- 活动 SQLite 数据库与 FTS5；
- `~/.mdoc/uploads`、`audio-memos` 和项目相对 `data/sync`；
- OS Keychain；
- AI 供应商 HTTPS API；
- 本机 `git` 可执行文件与远端；
- 浏览器 MediaRecorder、Chrome extension API。

## 7. 核心数据结构

| 结构 | 作用 |
|---|---|
| `aiProviderConfig` | 供应商、模型、base URL、默认状态和密钥提示 |
| `chatConversation` / `chatMessage` | AI 对话持久化 |
| `audioMemoRecord` | 录音文件、转录状态和生成文档 |
| `captureTokenInfo` | 扩展令牌元数据 |
| `syncConfig` | 远端、分支、状态和自动同步配置 |
| `syncManifest` | Git 投影中文档身份、标题、目录和类型 |
| `noteTemplate` | 知识库作用域的模板 |

## 8. 主要处理流程

```text
触发动作
→ 校验知识库、私密状态、凭据和输入大小
→ 读取 SQLite/本地文件
→ 调用 AI、Git 或浏览器能力
→ 事务写回文档/状态
→ 前端刷新或消费 SSE
```

AI 和转录会创建或更新文档；捕获直接改写每日笔记；Git 同步先导出投影，再合并并把远端内容回写 SQLite。

## 9. 配置与扩展方式

- AI 供应商元数据在 SQLite，密钥在 Keychain。
- Git 远端、分支和状态在 `sync_configs`，令牌在 Keychain。
- 自动同步与资源描述由工作区设置/同步配置驱动。
- 新集成通常需要后端 handler、前端设置组件、持久化结构和隐私规则四部分同步修改。

## 10. 代码入口

- AI：`aiProviders`、`aiTransform`、`aiChat`、`describeAssets`。
- 音频：`audioMemos`、`audioMemo`。
- 捕获：`captureTokens`、`capture`。
- Git：`syncSettings`、`syncRun`、`performSync`。
- 内容迁移：`importMD`、`exportMD`、`templates`。

## 11. 设计特点

- AI 密钥和 Git 令牌不写入 SQLite，只保存 Keychain 引用/提示。
- AI 上下文和资源描述执行明确的私密过滤。
- 资源描述使用内容哈希避免重复付费，并保护用户自写的描述文件。
- 捕获和音频以 Markdown 回链连接原始媒体与知识库。
- Git 备份采用可读 Markdown 投影而不是复制 SQLite 文件。

## 12. 潜在维护风险

- 外部网络请求大多在用户请求线程内运行，取消、超时和部分成功语义不完全一致。
- 私密策略只在特定集成入口实现，新增入口若未复用检查可能泄露内容。
- 供应商 API 结构由手写 map/JSON 解析维护，模型或协议变化会直接影响功能。
- Git 合并会保留正文冲突标记并继续提交，需要用户在编辑器中解决。
- Keychain 中的凭据生命周期与切换数据库的元数据生命周期不完全绑定。

## 13. 相关文档

- [Web 工作区](../03-application-interfaces/03-01-web-workspace.md)
- [HTTP API 与命令行接口](../03-application-interfaces/03-02-http-api-and-cli.md)
- [SQLite 与本地文件存储](../05-data-publication-and-runtime/05-01-sqlite-and-local-files.md)

## 14. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/ai.go` | AI handlers 与供应商流 | AI 配置、问答和改写 |
| `cmd/mdocman/assets.go` | `describeAssets` | 资源分类和描述 |
| `cmd/mdocman/audio.go` | `audioMemos`、`audioMemo` | 录音和转录 |
| `cmd/mdocman/capture.go` | `capture` | 网页捕获写回 |
| `cmd/mdocman/sync.go` | `performSync` | Git 同步 |
| `cmd/mdocman/templates.go` | 模板 handlers | 自定义模板 |
| `app/reflect/ai-settings.tsx` | `AiSettings` | AI 配置 UI |
| `app/reflect/sync-settings.tsx` | `SyncSettings` | Git 配置 UI |
