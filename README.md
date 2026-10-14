# 墨笺 Mdocman

一个 Go + TypeScript 的本地 Markdown 笔记管理器。使用 SQLite 管理多个笔记本，支持递归多级目录、可拖拽文件树、Markdown 编辑与渲染预览，并可使用 Hugo 主题将标记为发布的文档生成纯静态站点。

增强 Markdown 能力包括 GFM 表格、图片上传、围栏代码块、KaTeX 数学公式和 Mermaid 图表。可以导入单个 `.md` 文件或整个目录，并按原目录层级导出 ZIP。

## 开发

需要 Go 1.24+、Node.js 22+、pnpm 11+ 和 C/C++ 编译器。项目脚本使用
`-tags extended` 编译内置 Hugo 的 Sass 与扩展图片处理。

```bash
./dev.sh
```

脚本会同时启动 Go API 与前端开发服务器，并把前端的 `/api`、`/uploads`、`/site`
请求自动代理到后端。按 `Ctrl+C` 会同时停止两个进程。数据目录固定为
`~/.mdoc/`：SQLite 数据库直接保存在该目录，上传图片和音频分别保存在
`~/.mdoc/uploads/` 与 `~/.mdoc/audio-memos/`。

## 本地语义搜索

语义搜索使用 `all-MiniLM-L6-v2` ONNX 模型在 Go 进程内生成 384 维句向量，
不需要 Python、云端 Embedding API 或单独安装 ONNX Runtime。启用后，模型用于
`⌘K`/`Ctrl+K` 全局混合搜索、AI Chat 检索和笔记详情中的“相似笔记”。私密笔记
不会进入语义索引。

默认模型目录为：

```text
~/.mdoc/models/Qdrant-all-MiniLM-L6-v2-onnx/
```

首次启用时，内置的 getmodel 下载流程会同时探测
[Hugging Face](https://huggingface.co/Qdrant/all-MiniLM-L6-v2-onnx) 和
[ModelScope](https://modelscope.cn/models/sentence-transformers/all-MiniLM-L6-v2)，自动选择可用且更快的源，
并在设置界面显示文件名、已下载字节、速度和总进度。未完成文件使用 `.part`
保存，网络恢复或重启应用后可断点续传；也可以在界面中固定选择某一个源。

下载后的目录包含 `model.onnx`、`tokenizer.json`、`config.json`、
`special_tokens_map.json` 和 `tokenizer_config.json`。每个文件都会校验大小和
SHA256，全部完成后才加载模型。也可以手动准备模型，并通过环境变量指定模型
目录：

```bash
MDOC_SEMANTIC_MODEL_DIR=/path/to/Qdrant-all-MiniLM-L6-v2-onnx ./mdoc
```

在“设置 → AI Chat → 本地语义搜索”中启用后，后台会按标题和句子边界增量建立
索引；模型或分块版本变化时会自动重算，也可以手动点击重建按钮。

左侧栏底部显示当前知识库。点击后可切换已有知识库、调整知识库颜色、在 Finder
中定位或新建知识库；每项右侧的“发布配置”可设置该知识库的 Server IP、SSH 用户和发布目录。
当前选择记录在 `~/.mdoc/workspace.json`，重启后会自动恢复。

传入 Markdown 文件或目录时，`dev.sh` 会跳过前端构建和 pnpm，直接通过 Go
开发运行模式启动预览：

```bash
./dev.sh ./README.md
./dev.sh ./docs
```

生产构建支持两种模式：

```bash
./build.sh              # 默认 cli：内嵌前端的独立可执行文件
./build.sh cli          # 显式选择 CLI 模式
./build.sh split        # 前后端分离，供 Electron 等桌面壳集成
./build.sh electron     # split 的别名
```

CLI 模式在项目根目录生成 `./mdoc`，其中已经内嵌预渲染首页和全部浏览器资源。
部署管理端时只需复制这一个二进制文件。运行后在监听成功时自动打开默认浏览器，
访问 `http://localhost:2026/`（支持 macOS、Windows 和 Linux）；
无需额外部署前端目录，也不需要在运行机器上安装 Node.js。
可通过 `API_PORT` 指定端口，浏览器会访问实际监听的端口；通过
`MDOC_NO_BROWSER=1 ./mdoc` 禁止自动打开浏览器。端口占用时程序报错退出；
无法启动浏览器时保留服务并输出可手动访问的地址。

分离模式生成 `dist/frontend/`（预渲染首页和浏览器资源）与
`dist/bin/mdocman`（使用 `extended,noembed` 构建的 Go 后端）。后端不嵌入或提供
管理端前端，默认监听 `2026`，可通过 `API_PORT` 指定端口。Electron 等桌面壳
需要启动该后端，通过 HTTP 服务或自定义协议提供前端资源，并将 `/api`、
`/_mdoc`、`/uploads`、`/audio`、`/site`、`/s` 的请求代理到后端。前端使用根路径
资源和相对 API 地址，不能直接通过 `file://` 加载。本脚本生成集成所需产物，
Electron 安装包由桌面壳的打包流程生成。

`./dev.sh` 仍使用独立的 Vinext 开发服务器和 HMR。`./clean.sh` 用于清理构建产物
与开发缓存。

两种模式均生成 `dist/bin/mdocman-site`，它是无数据库依赖的只读发布端。文档站默认通过
内置 Hugo Extended 源码引擎 + Stack v3.31.0 构建，由远端 Caddy 托管。Hugo 运行时源码位于 `third_party/hugo/`，编译进 `mdoc`，无需另装 Hugo。
发布主题单独安装；只有
frontmatter 标记为 `publish: true` 且非私密、非回收站、非草稿的文档进入站点。
完整流程见 [文档发布与 Hugo 主题](docs/文档发布.md)。

首次使用固定数据目录时，如果 `~/.mdoc/mdocman.db` 尚不存在，程序会从启动目录
下旧版 `data/mdocman.db` 创建一致的 SQLite 快照，并复制旧附件、音频和
`notebooks.json`。旧目录不会被删除。

## 命令行预览

开发时无需完整构建即可预览一个 Markdown 文件或目录：

```bash
./dev.sh ./README.md
./dev.sh ./docs
```

生产构建后也可以使用 `./mdoc <路径>` 启动相同的预览。命令会启动仅监听本机的
临时服务并自动打开浏览器。目录优先显示其中的
`README.md` 或 `index.md`，否则显示可浏览的文件列表。Markdown 文件的 URL
保持原有目录结构，因此文档内指向其他本地 Markdown 文件的相对链接仍可继续
访问预览；图片等相对资源也会正常加载。可通过 `MDOC_PORT=8088` 指定端口，
通过 `MDOC_NO_BROWSER=1` 禁止自动打开浏览器。

预览页右上角的主题菜单与发布共用 Hugo 主题包。内置“默认 / 护眼 / 暗色 / 雅黑紧凑”可直接使用，Stack 等模块主题需先安装。可通过 `MDOC_THEME=dark ./mdoc ./docs` 指定初始主题，切换时布局、样式和目录树一起经过内置 Hugo 渲染。

主题按 `themes/<主题名>/` 组织，支持继承、原生 Hugo 布局与资源管线，目录树是独立组件，可单独定制。知识库的发布目录来自侧栏固定文档的可拖拽树；删除或取消固定父节点时，子节点按原顺序提升到同级。扩展方式与完整数据约定见 [docs/THEME.md](docs/THEME.md)。

数据库结构和数据流参见 [docs/architecture.md](docs/architecture.md)。
完整操作说明参见 [docs/使用说明.md](docs/使用说明.md)。
Reflect Open 的功能迁移状态参见
[docs/reflect-open-迁移.md](docs/reflect-open-%E8%BF%81%E7%A7%BB.md)。

## 静态发布

在笔记右侧勾选“标记为发布”，保存后打开“设置 → 文档发布”，依次安装主题、
生成预览、发布到服务器。默认目标为 `https://mdoc.raoqu.cc/`。

```bash
./publish.sh theme install
./publish.sh build
./deploy.sh
```

也可运行 `./mdoc publish help` 查看生产二进制命令。构建结果位于当前知识库的
`~/.mdoc/publishing/<知识库哈希>/current/public/`，管理端通过 `/site/` 预览。
每次部署都会重新筛选文档；取消发布后再次部署即可撤下对应页面和附件。
原 `public-site/` 目录不再作为发布来源。
