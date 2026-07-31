# AI 与资源增强

## 1. 功能说明

AI 子模块支持供应商管理、编辑器选区改写、基于本地笔记检索的聊天、图片/SVG/PDF 描述，并为音频模块提供可复用的供应商凭据。

## 2. 职责边界

它负责供应商协议和隐私过滤，不负责编辑器的可视替换交互或音频文件生命周期。Chat 只使用当前笔记本中非私密、未删除的文档，不访问互联网检索。

## 3. 所属上级模块

[自动化与外部集成](./04-00-automation-and-integrations-overview.md)。

## 4. 对外接口

- `/api/ai/providers` 与 `/api/ai/providers/{id}`；
- `POST /api/ai/transform`；
- `POST /api/ai/chat`；
- `/api/ai/conversations` 与子资源；
- `/api/semantic`（状态、启停和强制重建）；
- `POST /api/assets/describe`；
- 前端 `consumeEventStream` 和内置选区提示词。

## 5. 主要实现组成

- 支持 `openai`、`anthropic`、`google`、`openrouter` 四类 provider。
- SQLite 保存供应商/模型/base URL/默认标记，Keychain 以随机 provider ID 保存密钥。
- `streamProvider` 把统一 `aiMessage` 转为各供应商的请求，并从 SSE 行提取增量。
- Chat 通过模型原生工具循环按需调用 `search_notes`、`read_notes`、
  `list_recent_notes`、`list_daily_notes` 和 `read_assets`；启用本地语义检索且
  索引就绪时，`search_notes` 用 FTS5 与本机句向量做 reciprocal-rank fusion，
  否则退回词法召回。工具最多运行 12 轮，最后一轮强制模型综合回答。
- macOS 通过 Natural Language 框架生成英文/简体中文等系统句向量，仍保持单个
  Go 二进制和零出站推理；非 macOS 或无 CGO 构建通过能力检测安全降级。
- 笔记按标题分节和句子边界积累到约 1,000 字符，短尾合并；内容哈希让未改变的
  文档跳过重算，资源 `.reflect.md` 描述也作为引用笔记的语义分块。
- 模型上下文按供应商目录中的保守窗口裁剪：先省略旧工具结果，再按完整对话轮次
  丢弃最早历史，避免拆散工具调用和结果。
- `classifyAsset` 要求资源被至少一篇公开文档引用，并在任何私密文档引用时整体跳过。

## 6. 输入与输出

选区改写输入文档 ID、当前选区和提示词，输出 `text-delta/complete/error` SSE。
Chat 输入问题或最多四张图片、会话 ID、供应商配置、具体模型和可选系统提示词，
输出工具调用、工具结果、来源列表和增量回答，并持久化消息、图片及模型可重放的
工具上下文。资源描述输入文件名集合和供应商，输出处理计数与旁车
`.reflect.md` 文件。

## 7. 处理流程

### 知识库问答

```text
问题
→ configuredProvider + Keychain
→ 构造图谱统计、检索规则、精确 [[标题]] 引用要求和用户附加指令
→ streamProviderRound
→ 模型调用 search/read/list/read_assets 工具
→ 每个工具在服务端排除 private/trashed/frontmatter-private
→ search_notes 在索引就绪时融合 FTS 与本地句向量排名
→ 把工具结果回传模型，最多多轮收集
→ 最后一轮关闭工具并综合回答
→ SSE 给前端
→ chat_messages 持久化文本、来源、工具活动和模型上下文
```

### 资源描述

```text
扫描 uploads
→ 类型/引用/隐私/大小检查
→ SHA-256 与现有 managed 描述比较
→ 供应商视觉或文件 API
→ 写入 {asset}.reflect.md
```

## 8. 依赖关系

上游是编辑器、Chat 页面、设置页和自动资源任务；下游是 SQLite/FTS5/语义分块、
macOS Natural Language、Keychain、本地上传目录和四类供应商 API。

## 9. 配置项

- 每个供应商配置包含 model、label、可选 base URL 和默认标记。
- 选区/Chat 流使用供应商默认端点或用户 base URL。
- 资源最大读取 20 MiB，供应商响应限制 4 MiB，普通请求超时 60 秒。
- `read_notes` 单篇最多返回 24,000 字符、单次最多 10 篇；Chat 历史按模型上下文
  窗口和 200k token 实用上限动态裁剪。
- Chat 模型选择独立于供应商配置的默认模型，可在同一个 BYOK 密钥下切换该
  供应商目录中的全部模型，并保存在本地工作区设置中。
- 自定义 Chat 系统提示词最多 20,000 字符，始终附加在内置检索和隐私规则之后。
- Chat 图片长边在浏览器侧缩到 1,568px；服务端只接受 JPEG、PNG、WebP、GIF，
  单张最多 5 MiB、每轮最多 4 张且总计不超过 12 MiB。
- Chat 打开时会恢复六小时内更新的最近会话；桌面端显示固定历史栏，移动端通过
  顶栏入口打开历史和新对话。
- 本地语义检索默认关闭；启用后后台增量建立索引并在设置页报告进度、模型、失败和
  重建入口。私密、已删除及 frontmatter-private 笔记不会写入向量表。

## 10. 错误处理

无 Keychain 密钥、无供应商、私密笔记、选区已变化或供应商非 2xx 时返回明确错误。SSE 开始后用 error 事件报告失败。资源批处理把单文件失败计入 `refused` 并继续处理其他文件。

## 11. 扩展与修改建议

- 把供应商协议适配拆成明确接口和类型化请求/响应，减少单函数分支。
- 为外部请求统一客户端超时、取消和重试策略；当前流式请求使用默认客户端。
- 将前后端私密判定纳入共享契约测试。
- 为非 macOS 发行版增加可内嵌的跨平台句向量运行时，同时维持单文件发布。
- 新增供应商时必须同时覆盖文本流、资源类型支持和前端 catalog。

## 12. 代码入口与调用链

```text
ReflectEditor.streamAiRun / ChatScreen.send
→ /api/ai/transform 或 /api/ai/chat
→ cloudSafeDocument / groundingNotes
→ configuredProvider
→ streamProvider
→ providerDelta
→ SSE
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/ai.go` | `configuredProvider` | 供应商与 Keychain 解析 |
| `cmd/mdocman/ai.go` | `aiTransform`、`aiChat` | 两条 AI 业务流程 |
| `cmd/mdocman/chat_tools.go` | `chatToolDefinitions`、`executeChatTool` | 只读工具、混合 RAG、隐私门禁和上下文预算 |
| `cmd/mdocman/semantic.go` | `semanticService`、`semanticChunks` | 增量语义索引、RRF 召回和状态 API |
| `cmd/mdocman/semantic_embed_darwin.go` | `appleNaturalLanguageEmbedder` | macOS 系统句向量桥 |
| `cmd/mdocman/provider_tools.go` | `streamProviderRound` | 四供应商多轮工具协议适配 |
| `cmd/mdocman/ai.go` | `streamProvider`、`providerDelta` | 多供应商 SSE 适配 |
| `cmd/mdocman/assets.go` | `classifyAsset`、`describeAssets` | 资源隐私和描述批处理 |
| `app/reflect/ai.ts` | prompts、`consumeEventStream` | 前端提示词和 SSE 消费 |
| `app/reflect/reflect-editor.tsx` | `streamAiRun` | 选区待确认替换 |
| `app/reflect/chat-screen.tsx` | `send` | Chat 流式 UI |
