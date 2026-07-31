# Web 工作区

## 1. 功能说明

Web 工作区是墨笺的主要用户界面，提供知识库/数据库切换、每日笔记、文档编辑、命令面板、标签、任务、废纸篓、AI Chat、音频面板和各类设置。

## 2. 职责边界

它负责浏览器状态和交互编排，不是最终数据源。刷新后会从 Go API 重新加载笔记树；仅外观与编辑偏好保存在 `localStorage`。

## 3. 所属上级模块

[应用接口层](./03-00-application-interfaces-overview.md)。

## 4. 对外接口

- React `ReflectWorkspace` 根组件；
- `WorkspaceSidebar`、`ReflectEditor`、`CommandPalette`、`ChatScreen`、`AudioMemoPanel` 等子组件；
- URL 深链：`view=note|daily|tasks|search` 与 `action=new|append|task`；
- 键盘命令：搜索、新建、今日、任务、AI、独立窗口和侧栏。

## 5. 主要实现组成

- `history/historyIndex` 实现应用内前进后退。
- `notebooks/notebookId/view` 表达当前工作集。
- `structuralDirtyRef` 和 `dirtyDocumentIdsRef` 区分整树保存与单文档保存。
- `conflicts` 保存后端 409 返回的版本。
- 多个 effect 负责初始加载、外部写入刷新、自动保存、自动同步、资源描述和深链。
- 设置页组合 AI、模板、捕获、Git 同步和数据操作面板。

## 6. 输入与输出

输入是 API 数据、编辑器变化、用户事件、浏览器 URL/网络状态和本地设置；输出是 React 视图、API 请求、导航历史、通知以及持久化后的 revision。

## 7. 处理流程

### 保存调度

```text
UI 修改
→ mutateNotebooks
→ 标记 structural 或 documentIds
→ dirty=true
→ 800ms 后 persist
→ 整树 PUT /api/notebooks
   或逐文档 PUT /api/documents/{id}
→ 更新 revision / 记录冲突
→ 30 秒后可选自动 Git 同步
→ 2 秒后可选资源描述
```

### 外部写入协调

捕获扩展、音频转录或 Git 导入可绕过当前浏览器状态写数据库。工作区在无本地 dirty 时每 10 秒及窗口获得焦点时重新拉取笔记树。

## 8. 依赖关系

上游是浏览器用户；下游是几乎全部 `/api` 资源、React、Meowdown、浏览器媒体/剪贴板/网络 API。它是前端功能的主要耦合点。

## 9. 配置项

`WorkspaceSettings` 控制主题、语法显示、拼写检查、列表行为、内容宽度、字号和资源描述。数据库、AI、模板、捕获和 Git 配置由后端持久化。

## 10. 错误处理

- 后端初始不可用时保留示例知识库并显示通知。
- 保存失败不丢弃页面内修改，dirty 状态继续保留。
- revision 冲突显示横幅，让用户选择服务端版本或以新 revision 重试本地版本。
- 切换/创建数据库时若正在保存会被阻止。
- 同步失败根据 `navigator.onLine` 标记为 `failed` 或 `offline`。

## 11. 扩展与修改建议

- 将保存协调、导航、集成设置和知识库变换拆成独立 hooks/reducer，降低 `ReflectWorkspace` 的修改半径。
- 为结构性更新增加冲突协议后，再允许多窗口安全编辑目录和笔记本。
- 为外部刷新增加基于 revision/事件的增量拉取，替代周期性整树请求。
- 为深链和关键保存场景增加浏览器级集成测试。

## 12. 代码入口与调用链

```text
app/page.tsx:Home
→ ReflectWorkspace
→ GET /api/databases + GET /api/notebooks
→ WorkspaceSidebar / ReflectEditor / 各集合视图
→ mutateNotebooks
→ persist
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `app/page.tsx` | `Home` | 页面入口 |
| `app/reflect/workspace.tsx` | `ReflectWorkspace` | 状态、导航和集成编排 |
| `app/reflect/workspace.tsx` | `persist`、`mutateNotebooks` | 保存协调 |
| `app/reflect/workspace-sidebar.tsx` | `WorkspaceSidebar` | 数据库、知识库和主导航 |
| `app/reflect/command-palette.tsx` | `CommandPalette` | 命令与 FTS 搜索 |
| `app/reflect/chat-screen.tsx` | `ChatScreen` | 知识库 AI 对话 |
| `app/reflect/audio-memo.tsx` | `AudioMemoPanel` | 浏览器录音入口 |
| `app/reflect/types.ts` | `WorkspaceView`、`WorkspaceSettings` | 工作区状态协议 |

