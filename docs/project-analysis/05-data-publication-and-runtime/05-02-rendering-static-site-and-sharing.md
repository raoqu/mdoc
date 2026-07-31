# 渲染、静态站点与分享

## 1. 功能说明

该子模块将 Markdown 转为 HTML，用于编辑器外部预览、本地文件预览、完整静态站点和单文档分享页，并提供独立的只读 Go 文件服务器。

## 2. 职责边界

它负责内容呈现和输出文件，不负责编辑器状态或远端托管平台。静态 HTML 生成后不再访问 SQLite。

## 3. 所属上级模块

[数据、发布与运行基础设施](./05-00-data-publication-and-runtime-overview.md)。

## 4. 对外接口

- `GET/POST /api/preview/{id}`；
- `POST /api/build`；
- `POST /api/share`；
- 管理端 `/site/` 和 `/s/`；
- `mdocman-site` 的 `/` 与 `/healthz`；
- 路径预览中的 `pathPreviewServer`。

## 5. 主要实现组成

- `newMarkdown` 启用 GFM、脚注、Typographer、自动 heading ID 和 Chroma。
- `markdownBody` 在预览时去除完整 frontmatter。
- `prepareImageMetadataForRender` 把图片 JSON 注释转换为 Goldmark 可渲染的 title，再后处理成宽高属性。
- 预览页面使用内嵌主题 CSS/JS 与 Mermaid 脚本。
- 静态站点页面使用统一 template、目录链接、KaTeX 和 Mermaid CDN。
- `buildManifest` 记录每个 HTML 的内容哈希和侧栏状态。

## 6. 输入与输出

输入是 Markdown、标题、笔记本名、目录选项和已有 manifest；输出是预览 HTML、`public-site/{book}/{doc}/index.html`、分享 token 页面、站点首页、样式、附件副本和新 manifest。

## 7. 处理流程

### 增量站点生成

```text
server.load
→ 收集全部文档导航
→ 计算 navHash
→ 每篇计算 title + content + sidebar + navHash
→ 与旧 manifest 比较
→ 只重写变化页面
→ 删除 manifest 中已消失页面
→ 复制 uploads
→ 写首页与新 manifest
```

### 单文档分享

```text
documentId
→ 查找或创建 shares.token
→ 写 public-site/s/{token}/index.html
→ 返回 /s/{token}/ 与 /site/s/{token}/
```

## 8. 依赖关系

上游是管理端预览、发布和分享动作；下游是 Goldmark、Chroma、主题嵌入、文件系统、浏览器 CDN 与 `mdocman-site`。

## 9. 配置项

- 发布侧栏由 `includeSidebar` 控制。
- `SITE_PORT` 默认 8090，`SITE_DIR` 默认 `public-site`。
- 预览主题目录可由 `MDOC_PREVIEW_THEME_DIR` 覆盖。
- 静态页面的 KaTeX 与 Mermaid 从 jsDelivr 加载。
- 预览页面设置限制性 CSP；静态发布模板未设置同等 HTTP CSP。

## 10. 错误处理

预览对缺失文档、无效表单和不支持方法返回相应状态码。构建和分享处理部分文件写入错误不完全检查，例如某些 `os.WriteFile`、`os.Create` 和模板执行结果被忽略，可能形成部分输出。只读服务依赖 `http.FileServer` 的标准行为。

## 11. 扩展与修改建议

- 在发布/分享前明确并执行私密、废纸篓和未发布状态策略；当前遍历没有显式过滤这些字段。
- 将输出写入临时目录后原子切换，避免部分构建对访问者可见。
- 对每个文件操作和模板执行统一收集错误。
- 若要求离线静态站点，将 KaTeX/Mermaid 资源本地化并提供内容安全策略。
- 分离“预览主题”和“发布主题”配置，避免默认 CSS 兼任两种用途。

## 12. 代码入口与调用链

```text
POST /api/build
→ server.build
→ server.load
→ walkFolders
→ server.render
→ Goldmark
→ public-site
→ cmd/mdocman-site:main
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `cmd/mdocman/markdown.go` | `newMarkdown` | Markdown renderer 配置 |
| `cmd/mdocman/main.go` | `render`、`markdownBody` | 内容预处理与渲染 |
| `cmd/mdocman/main.go` | `previewDocument` | 数据库文档预览 |
| `cmd/mdocman/main.go` | `build`、`buildManifest` | 增量静态站点 |
| `cmd/mdocman/main.go` | `share` | token 分享页 |
| `cmd/mdocman/preview_theme.go` | 主题资源 | 预览样式和脚本 |
| `themes/embed.go` | `Files` | CSS 嵌入 |
| `cmd/mdocman-site/main.go` | `main` | 只读站点入口 |

