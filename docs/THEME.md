# Hugo 主题扩展指南

文件 / 文件夹预览、知识库单篇预览和静态发布共用内置 Hugo 引擎与主题包。主题的布局、CSS、脚本和目录树组件均放在 `themes/<主题名>/`，无需再维护一套预览模板或修改 Go 主题注册表。

## 1. 目录与内置主题

```text
themes/
  embed.go                       # 构建时嵌入所有主题目录
  _shared/                       # 共用组件；具体主题可覆盖同名文件
    layouts/
      partials/mdoc/
        tree.html                # 独立目录树入口
        tree-nodes.html          # 递归节点、链接和当前页状态
        tree-mobile.html         # 小屏折叠目录
        tree-head.html           # 目录 CSS、状态脚本和主题标识
        theme-menu.html          # 本地预览的主题选择菜单
        preview-input.html       # 切换主题时保留未保存的预览正文
      _default/_markup/
        render-codeblock-mermaid.html
    static/mdoc/
      tree.css                   # 目录样式和 compact 变体
      mermaid.css                # 图表与放大浮层样式
      preview-theme.js            # 主题选择、目录折叠和滚动位置
      preview-mermaid.js          # Mermaid 渲染与放大查看
      math.js                    # KaTeX 初始化
  default/
    theme.json                   # 主题声明
    layouts/
      index.html
      _default/baseof.html
      _default/single.html
      _default/list.html
    static/mdoc/
      base.css                   # 正文排版、颜色和基础布局
      style.css                  # 当前主题覆盖
  sepia/                         # 护眼，继承 default
  dark/                          # 暗色，继承 default
  yahei/                         # 雅黑紧凑，继承 default
  stack/                         # Stack v3.31.0 的本地适配包
    theme.json                   # 固定上游模块与版本
    layouts/partials/            # 侧栏、head、footer 扩展
    static/mdoc/stack.css
```

`default`、`sepia`、`dark`、`yahei` 可离线直接使用。Stack 保留上游 Hugo 布局、Sass、文章目录、搜索、归档和明暗切换，并通过本地适配包接入同一个目录树。首次使用模块主题前，在“设置 → 文档发布”安装，或执行 `mdoc publish theme install`。后续构建无需外部 Hugo、Go 或 Sass 程序。

## 2. 选择主题

文件预览默认为 `default`。可通过环境变量指定初始主题：

```bash
MDOC_THEME=dark ./mdoc ./docs
MDOC_THEME=stack ./dev.sh ./README.md
```

也可使用预览页右上角的主题菜单，或访问 `http://127.0.0.1:<端口>/?theme=dark`。切换会重新经过 Hugo，布局和组件一起变化；浏览器会记住选择。菜单只列出当前可用的主题，模块主题需先安装。已有显式 `theme` URL 参数优先于浏览器偏好。文档间的相对 Markdown 链接、图片路径、目录排序、目录折叠和滚动位置保留；文件预览不修改源文件。

知识库的预览和发布使用该知识库的发布配置。在“设置 → 文档发布”选择主题，也可导入以下配置：

```json
{
  "title": "我的文档",
  "baseURL": "https://example.com/docs/",
  "subtitle": "知识库",
  "theme": "dark",
  "treeTheme": "compact",
  "includeTOC": true,
  "deployTarget": "deploy@example.com:/var/www/docs/"
}
```

`treeTheme` 留空时采用主题的默认值；内置 `default` 和 `compact`，自定义名称需由主题提供对应 CSS。`theme` 选择命名主题包，其模块路径和版本来自主题声明。原有仅包含 `themeModule` / `themeVersion` 的配置继续有效；界面选择“自定义 Hugo Module”可直接使用模块配置。任意第三方主题需要按第 5 节接入 tree partial。

## 3. 添加或继承主题

新增 `themes/ocean/theme.json`：

```json
{
  "id": "ocean",
  "label": "海蓝",
  "extends": "default",
  "treeTheme": "compact"
}
```

目录名与 `id` 必须相同，格式为小写字母开头，后接小写字母、数字或连字符，总长度不超过 64。`label` 为菜单显示名称。`extends` 可省略；继承链从父主题到子主题覆盖，循环继承和不存在的父主题会报错。

然后创建 `themes/ocean/static/mdoc/style.css`：

```css
:root {
  --preview-background: #f4f9ff;
  --preview-text: #24364b;
  --preview-link: #1766b5;
  --preview-border: #c7d9eb;
  --preview-surface: #e8f1fc;
  --preview-surface-strong: #dceafb;
  --preview-body-font: system-ui, "PingFang SC", sans-serif;
  --tree-width: 260px;
  --tree-indent: 16px;
}
```

继承的布局和 `base.css` 自动保留。可覆盖 `layouts/_default/baseof.html`、`single.html` 或任意 partial，按原生 Hugo 的方式使用 `.Content`、`.TableOfContents`、`resources.Get`、Sass、图片处理等能力。主题专属 Hugo 配置可放在 `config/_default/`，文件名避免使用 Mdoc 管理的 `hugo.json` 和 `module.json`。

开发时 `./dev.sh` 自动从仓库 `themes/` 读取主题，刷新预览或重新生成发布即可应用修改。也可显式设置主题库路径：

```bash
MDOC_THEME_DIR=/absolute/path/to/themes MDOC_THEME=ocean ./mdoc ./docs
```

外部主题库应包含所需父主题和 `_shared/`；可复制仓库 `themes/` 后扩展。`MDOC_PREVIEW_THEME_DIR` 是兼容别名，`MDOC_THEME_DIR` 优先。生产二进制默认读取内嵌主题；增加主题后重新构建，或使用外部主题库即可，无需改 Go 列表。

## 4. 独立 tree 组件与数据约定

页面通过 `{{ partial "mdoc/tree.html" . }}` 渲染树，通过 `{{ partial "mdoc/tree-head.html" . }}` 加载共用资源。小屏入口为 `{{ partial "mdoc/tree-mobile.html" . }}`。

主题的数据入口是 `.Site.Data.mdoc`：

| 字段 | 含义 |
| --- | --- |
| `Tree` | 递归节点数组 |
| `TreeID` | 当前目录或知识库的状态隔离标识 |
| `Themes` | 本地可用主题，元素字段为 `id`、`label` |
| `CurrentURL` | 当前预览请求地址，供主题生成当前页链接 |
| `Directory`、`Entries` | 文件夹没有首页时的目录列表 |
| `PreviewInput` | 未保存的本地预览输入，供主题切换保留正文 |

树节点字段：

| 字段 | 含义 |
| --- | --- |
| `Name` | 显示标题，自动转义 |
| `URL` | 预览的原始跳转地址，或发布站点的相对路由 |
| `TreeKey` | 折叠状态键 |
| `IsDir` | 是否包含目录 / 子节点结构 |
| `Active` | 本地预览的当前页状态 |
| `NameToggles` | 无正文的文件目录使用标题折叠 |
| `Children` | 子节点数组 |

发布时 tree partial 使用 `relURL` 生成链接，自动支持 `/docs/`、`/site/` 等子路径，并通过当前页 `.RelPermalink` 高亮。预览链接保留原文件路径。保留 `data-path-tree-id` 与 `details[data-path-tree-key]`，即可复用目录展开和滚动位置存储。

本地预览的 Markdown 图片和链接通过 `mdoc/preview-image.html`、`mdoc/preview-link.html` 保留原地址，避免模块主题将本地资源改写到临时 Hugo 路由；主题可覆盖这两个 partial。

`.Site.Params.mdocPreview` 区分本地预览与发布；`.Site.Params.mdocTheme` 为主题名，`.Site.Params.mdocTreeTheme` 为目录样式。可在自己的 `layouts/partials/mdoc/tree.html` / `tree-nodes.html` 中完全重写结构，或仅覆盖 `static/mdoc/tree.css`。共用树使用 `--tree-width`、`--tree-indent`、`--tree-font-size` 及 `--preview-*` 颜色变量；Stack 适配包将颜色映射到 Stack 变量。

## 5. 接入其他 Hugo Module

创建主题包并声明固定版本，例如：

```json
{
  "id": "my-blog",
  "label": "我的博客",
  "module": "github.com/example/hugo-theme-example",
  "version": "v1.2.3",
  "treeTheme": "default"
}
```

上例路径和版本为占位示例，需替换为实际模块。不继承 `default` 时，页面布局由上游主题提供。根据主题提供的 head、侧栏和 footer 扩展钩子，调用上述 `mdoc/*` partial；参考 `themes/stack/`。Hugo 构建期间禁止启动外部程序，Sass 等使用内置 Extended 引擎。

合并顺序为：上游已安装模块 → `_shared` → 父主题 → 当前主题 → 当前知识库 `overrides/`。覆盖目录支持 `layouts`、`assets`、`static`、`i18n` 和 `data`；`data/mdoc.json` 为应用生成的导航数据，不应自行覆盖。模块安装缓存与知识库配置仍位于 `~/.mdoc/publishing/`，不会写进主题源码目录。

本地预览 CSP 允许本地资源及 jsDelivr；主题生成的内联初始化脚本按实际内容 SHA256 授权。第三方主题应尽量使用本地静态资源，并避免依赖内联事件处理属性。Markdown 中的原始 HTML 不会执行。

## 6. 知识库固定文档树

知识库导航来自左侧栏“已固定”的文档，独立于笔记存储文件夹：

- 拖到节点上沿 / 下沿可前后排序，拖到中间可成为子节点；拖到列表下方的投放区可移到顶层末尾。
- 键盘聚焦文档后，`Alt + ↑/↓` 排序，`Alt + →` 放入前一个节点，`Alt + ←` 提升一级。
- 拖动父节点会带着整棵子树移动，不允许将节点放到自己的后代中。
- 删除、移入废纸篓或取消固定某节点时，其直接子节点按原顺序提升到同级，后代结构保留；恢复父文档不会重新收回这些子节点。
- 层级和顺序保存在知识库 SQLite 的 `pinned_trees` 中，并通过笔记空间的 `pinnedTree` 字段读写。旧知识库第一次读取时，将现有固定文档按原顺序转换为根节点。
- 发布目录只保留满足发布规则的固定文档。私密、未发布、草稿和废纸篓父节点不显示，符合发布条件的子节点提升，父节点标题不会泄露。公开但未固定的文章仍发布，可通过首页、搜索或正文链接访问。

## 7. 验证

```bash
go test -tags extended ./cmd/mdocman ./internal/hugoengine
node --import tsx --test tests/reflect-core.test.ts
MDOC_TEST_THEME_INSTALL=1 go test -tags extended ./cmd/mdocman -run TestPublicationHugoIntegration -count=1
```

最后一项在临时知识库中安装并验证实际 Stack 主题，需要首次安装时能访问网络。建议同时检查桌面和窄屏、相对链接、带空格及中文的文件名、子路径发布、目录折叠、拖拽后刷新、删除父节点和未发布节点过滤。
