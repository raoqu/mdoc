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
- `POST /api/assets/describe`；
- 前端 `consumeEventStream` 和内置选区提示词。

## 5. 主要实现组成

- 支持 `openai`、`anthropic`、`google`、`openrouter` 四类 provider。
- SQLite 保存供应商/模型/base URL/默认标记，Keychain 以随机 provider ID 保存密钥。
- `streamProvider` 把统一 `aiMessage` 转为各供应商的请求，并从 SSE 行提取增量。
- `groundingNotes` 使用 FTS5 选取最多 8 篇笔记，限制单篇和总体字符预算。
- `classifyAsset` 要求资源被至少一篇公开文档引用，并在任何私密文档引用时整体跳过。

## 6. 输入与输出

选区改写输入文档 ID、当前选区和提示词，输出 `text-delta/complete/error` SSE。Chat 输入问题与会话 ID，输出来源列表和增量回答，并持久化消息。资源描述输入文件名集合和供应商，输出处理计数与旁车 `.reflect.md` 文件。

## 7. 处理流程

### 知识库问答

```text
问题
→ configuredProvider + Keychain
→ groundingNotes(FTS5)
→ 排除 private/trashed/frontmatter-private
→ 附加允许发送的资源描述
→ 构造带精确 [[标题]] 引用要求的 system prompt
→ streamProvider
→ SSE 给前端
→ chat_messages 持久化
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

上游是编辑器、Chat 页面、设置页和自动资源任务；下游是 SQLite/FTS5、Keychain、本地上传目录和四类供应商 API。

## 9. 配置项

- 每个供应商配置包含 model、label、可选 base URL 和默认标记。
- 选区/Chat 流使用供应商默认端点或用户 base URL。
- 资源最大读取 20 MiB，供应商响应限制 4 MiB，普通请求超时 60 秒。
- Chat 上下文总体字符预算约 32,000，单篇先截到 12,000。

## 10. 错误处理

无 Keychain 密钥、无供应商、私密笔记、选区已变化或供应商非 2xx 时返回明确错误。SSE 开始后用 error 事件报告失败。资源批处理把单文件失败计入 `refused` 并继续处理其他文件。

## 11. 扩展与修改建议

- 把供应商协议适配拆成明确接口和类型化请求/响应，减少单函数分支。
- 为外部请求统一客户端超时、取消和重试策略；当前流式请求使用默认客户端。
- 将前后端私密判定纳入共享契约测试。
- 对 FTS 上下文增加 token 级预算和更明确的排序/引用证据。
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
| `cmd/mdocman/ai.go` | `groundingNotes` | 私密过滤和 FTS 上下文 |
| `cmd/mdocman/ai.go` | `streamProvider`、`providerDelta` | 多供应商 SSE 适配 |
| `cmd/mdocman/assets.go` | `classifyAsset`、`describeAssets` | 资源隐私和描述批处理 |
| `app/reflect/ai.ts` | prompts、`consumeEventStream` | 前端提示词和 SSE 消费 |
| `app/reflect/reflect-editor.tsx` | `streamAiRun` | 选区待确认替换 |
| `app/reflect/chat-screen.tsx` | `send` | Chat 流式 UI |

