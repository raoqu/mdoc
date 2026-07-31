# 路径预览与浏览器捕获

## 1. 功能说明

路径预览让用户不启动完整前端即可浏览单个 Markdown 文件或目录；浏览器捕获则把网页资料从 Chrome 扩展提交到本地知识库。二者都是围绕 Markdown 的外围入口，但一个只读、一个写入。

## 2. 职责边界

路径预览不打开 SQLite，也不修改源文件。Chrome 扩展不直接访问数据库，只维护本地发送队列并调用受令牌保护的捕获 API。

## 3. 所属上级模块

[应用接口层](./03-00-application-interfaces-overview.md)。

## 4. 对外接口

- `mdoc <Markdown 文件或目录>`；
- `MDOC_PORT`、`MDOC_NO_BROWSER`、`MDOC_PREVIEW_THEME_DIR`；
- Chrome 扩展按钮与 `Command/Ctrl+Shift+K`；
- `POST /api/capture` 的 Bearer token；
- `/api/capture/tokens` 的令牌管理。

## 5. 主要实现组成

### 路径预览

- `pathPreviewArgument` 在数据库初始化之前识别单路径参数。
- `newPathPreviewServer` 解析绝对路径和符号链接，确定单文件/目录模式。
- `resolve` 使用 `filepath.Rel` 防止逃逸根目录。
- 目录优先渲染 README/index，否则列出 Markdown 和子目录；目录模式还生成 Markdown 树。
- 相对图片等非 Markdown 资源由同一受限根目录服务。

### 浏览器捕获

- 扩展 service worker 获取活动页、选区和可选截图。
- 捕获项先存入 `chrome.storage.local.queue`，再逐条发送。
- 安装时建立每分钟 alarm，启动和手动操作也会重试。
- 后端把令牌以 SHA-256 摘要保存，并将内容幂等写入每日笔记和可选专用捕获文档。

## 6. 输入与输出

路径预览输入本地路径，输出仅监听 `127.0.0.1` 的临时 HTTP 服务和自动打开的浏览器。捕获输入网页元数据、截图 data URL 和备注，输出每日笔记中的链接块、上传截图和专用 Markdown 文档 ID。

## 7. 处理流程

```text
Chrome 活动页
→ executeScript 获取选区
→ captureVisibleTab
→ chrome.storage.local 队列
→ POST /api/capture + Bearer token
→ 验证 URL/令牌/大小
→ 保存截图
→ 事务更新每日笔记
→ 可选创建 capture-{hash} 文档
```

捕获块用 URL、日期、目标文档和选区生成稳定键，通过 HTML 注释定位并替换，重复提交不会无限追加同一块。

## 8. 依赖关系

路径预览依赖 Goldmark、预览主题和操作系统打开浏览器命令。捕获依赖 Chrome MV3 API、Go 捕获 handler、SQLite 和上传目录。

## 9. 配置项

- 路径预览默认选择随机可用端口；显式 `MDOC_PORT` 可固定。
- 扩展 API 地址固定为 `127.0.0.1:8080`。
- 截图最大 12 MiB，整体捕获请求最大 16 MiB。
- 扩展拥有 `activeTab`、`scripting`、`storage`、`alarms` 和本地端口 host permission。

## 10. 错误处理

路径不存在、不是 Markdown、符号链接越界或端口失败会终止预览。扩展无法截图时仍可提交文本；本地服务不可用时保留队列和错误信息。后端拒绝无效 URL、令牌、截图和跨笔记本目标。

## 11. 扩展与修改建议

- 让扩展 API 地址可配置或从管理端生成，支持非默认端口。
- 为扩展队列增加上限、重试退避和逐项可见状态。
- 将每日笔记创建逻辑抽为共享后端服务，供捕获和音频共同使用。
- 路径预览若加入写回能力，必须保持当前根目录逃逸检查。

## 12. 代码入口与调用链

```text
main
→ pathPreviewArgument
→ servePathPreview
→ pathPreviewServer.ServeHTTP
→ renderMarkdownFile / renderDirectory

chrome.runtime / action
→ capturePage
→ flushQueue
→ server.capture
→ upsertCaptureBlock
→ SQLite commit
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/path_preview.go` | `pathPreviewServer`、`servePathPreview` | 文件/目录预览 |
| `cmd/mdocman/preview_theme.go` | `servePreviewAsset` | 预览主题和脚本 |
| `cmd/mdocman/markdown.go` | `newMarkdown` | 共享 Markdown renderer |
| `extensions/mdocman-capture/service-worker.js` | `capturePage`、`flushQueue` | 捕获与离线队列 |
| `extensions/mdocman-capture/manifest.json` | 权限与命令 | 扩展运行契约 |
| `cmd/mdocman/capture.go` | `captureTokens`、`capture` | 鉴权和持久化 |
| `cmd/mdocman/capture_test.go` | 捕获块和令牌测试 | 幂等与摘要验证 |

