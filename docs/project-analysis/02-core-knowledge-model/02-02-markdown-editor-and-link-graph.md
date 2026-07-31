# Markdown 编辑器与链接图谱

## 1. 功能说明

该子模块提供富 Markdown 编辑体验，同时保持正文可往返为 Markdown。它识别 Wiki 链接、标签、别名、frontmatter、附件和图片元数据，并从文档集合实时计算反向链接。

## 2. 职责边界

它负责浏览器端编辑和 Markdown 语义投影，不负责最终数据库事务或静态 HTML 发布。后端 Goldmark 渲染与前端 Meowdown 编辑是两个不同实现，共享的是 Markdown 文本协议。

## 3. 所属上级模块

[核心知识模型](./02-00-core-knowledge-model-overview.md)。

## 4. 对外接口

- `ReflectEditor` 属性和 `ReflectEditorHandle` 命令接口；
- `wikiLinksIn`、`backlinksFor`、`renameWikiLinks`、`tagsIn`；
- `splitFrontmatter`、`joinFrontmatter`、`upsertFrontmatter`；
- Meowdown 的 Wiki、标签、Slash、选区、文件和图片回调；
- `/api/upload`、`/api/ai/transform` 作为编辑器扩展服务。

## 5. 主要实现组成

- Meowdown 提供 ProseMirror 风格的可视编辑和 Markdown 往返检查。
- `ReflectEditor` 在挂载时拆分 frontmatter，只把正文交给编辑器，再在变化时无损拼回 header。
- Wiki 链接搜索限定当前知识库，按标题、ID 或别名解析，并提供悬浮预览。
- 标题重命名由 `ReflectWorkspace` 追踪稳定标题，写入别名并改写其他文档链接。
- 图片以 Markdown 后的 JSON 注释保存尺寸和可选外链，旧的“链接包图片”写法会自动归一化。

## 6. 输入与输出

输入包括 Markdown 文本、文档集合、编辑器设置、模板和 AI 供应商列表；输出是更新后的完整 Markdown、导航事件、上传 URL、标签选择或 AI 待确认替换。

## 7. 处理流程

```text
DocumentRecord.content
→ splitFrontmatter
→ normalizeLinkedImages / checkRoundTrip
→ MeowdownEditor
→ onDocChange
→ joinFrontmatter
→ ReflectWorkspace.changeDocument
→ 单文档保存
```

Wiki 链接点击会在当前知识库中按标准化标题/别名查找目标；反向链接则扫描其他文档的 Wiki 链接目标集合。

## 8. 依赖关系

上游是 Web 工作区和文档模型；下游是 `@meowdown/core`、`@meowdown/react`、`yaml`、上传 API 和选区 AI API。发布端不依赖 Meowdown，而由 Goldmark 重新渲染。

## 9. 配置项

编辑器设置保存在浏览器 `localStorage`：

- Markdown 语法标记显示模式；
- 拼写检查；
- 标题后自动开始列表；
- 编辑宽度和字号；
- 外观主题；
- 是否自动生成资源 AI 描述。

## 10. 错误处理

- Markdown 往返不安全时显示原始文本和诊断信息，而不是强行进入可视编辑。
- 无效 frontmatter 会保留原文并返回解析警告。
- Wiki 目标不存在时仅提示，不自动创建。
- 上传、AI 流和文件探测失败通过通知反馈；选区 AI 使用 AbortController 和“接受/丢弃/重试”交互。

## 11. 扩展与修改建议

- 新 Markdown 语义优先实现为纯函数，并测试代码围栏、换行和 frontmatter 边界。
- 前后端共同使用的私密与正文切分规则应增加共享规范测试，防止实现漂移。
- 大型知识库的反向链接、标签和重命名可改为索引或后端查询，避免每次遍历全部正文。
- 图片元数据协议变化时同时修改编辑器归一化和 Go `prepareImageMetadataForRender`。

## 12. 代码入口与调用链

```text
app/page.tsx:Home
→ ReflectWorkspace
→ renderEditor
→ ReflectEditor
→ MeowdownEditor.onDocChange
→ changeDocument
→ persist
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `app/reflect/reflect-editor.tsx` | `ReflectEditor` | 编辑器适配与扩展能力 |
| `app/reflect/reflect-editor.tsx` | `normalizeLinkedImages` | 图片元数据迁移 |
| `app/reflect/frontmatter.ts` | `splitFrontmatter`、`upsertFrontmatter` | YAML frontmatter 保留与更新 |
| `app/reflect/markdown.ts` | `wikiLinksIn`、`backlinksFor`、`renameWikiLinks` | 链接图谱派生 |
| `app/reflect/workspace.tsx` | 标题重命名 effect | 别名与跨文档链接改写 |
| `tests/reflect-core.test.ts` | frontmatter、Wiki、图片测试 | 核心 Markdown 行为验证 |

