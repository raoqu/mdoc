# 墨笺 Mdocman

一个 Go + TypeScript 的本地 Markdown 笔记管理器。使用 SQLite 管理多个笔记本，支持递归多级目录、可拖拽文件树、Markdown 编辑与渲染预览，并可生成类似 Hugo 的纯静态站点。

增强 Markdown 能力包括 GFM 表格、图片上传、围栏代码块、KaTeX 数学公式和 Mermaid 图表。可以导入单个 `.md` 文件或整个目录，并按原目录层级导出 ZIP。

## 开发

需要 Go 1.24+、Node.js 22+、pnpm 11+。

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
中定位或新建知识库；当前选择记录在 `~/.mdoc/workspace.json`，重启后会自动恢复。

传入 Markdown 文件或目录时，`dev.sh` 会跳过前端构建和 pnpm，直接通过 Go
开发运行模式启动预览：

```bash
./dev.sh ./README.md
./dev.sh ./docs
```

生产构建与清理：

```bash
./build.sh
./clean.sh
```

构建会在项目根目录生成 `./mdoc`，其中已经内嵌预渲染首页和全部浏览器资源。
部署管理端时只需复制这一个二进制文件，运行后访问 `http://localhost:8080/`；
无需额外部署前端目录，也不需要在运行机器上安装 Node.js。`dev.sh` 仍使用独立的
Vinext 开发服务器和 HMR，不受生产内嵌方式影响。

构建还会生成 `mdocman-site`，它是无数据库依赖的只读发布端，仅托管
`public-site/`。发布端默认监听 `8090`：

```bash
SITE_PORT=8090 SITE_DIR=public-site ./dist/bin/mdocman-site
```

管理端采用内容哈希清单进行 Hugo 风格的增量生成，只重建内容或目录导航发生变化的页面。远程增量部署可使用：

```bash
DEPLOY_TARGET=user@host:/var/www/notes ./deploy.sh
```

管理界面的“发布含目录”开关决定静态页面是否生成左侧目录栏。

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

预览页右上角默认只显示主题图标，点击后可在弹出菜单中切换“默认 / 护眼 / 暗色 /
雅黑紧凑”主题，选择会保存在浏览器本地。“雅黑紧凑”在 Windows 上优先使用
微软雅黑，在 macOS 上回退到苹方；正文采用 14px 字号和 16px 总行高，表格、
代码和引用采用 12px 字号和 14px 总行高。
主题样式位于项目根目录的 `themes/`：`default.css` 定义完整的颜色与排版变量，
其他主题 CSS 只需覆盖需要变化的变量。新增主题时，同时在
`cmd/mdocman/preview_theme.go` 的 `previewThemes` 中登记名称即可。

数据库结构和数据流参见 [docs/architecture.md](docs/architecture.md)。
完整操作说明参见 [docs/使用说明.md](docs/使用说明.md)。
Reflect Open 的功能迁移状态参见
[docs/reflect-open-迁移.md](docs/reflect-open-%E8%BF%81%E7%A7%BB.md)。

## 静态发布

在界面点击“发布站点”，或运行：

```bash
make static
```

生成内容位于 `public-site/`，Go 服务可通过 `http://localhost:8080/site/` 预览。
