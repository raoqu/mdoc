# 墨笺 Mdocman

一个 Go + TypeScript 的本地 Markdown 笔记管理器。使用 SQLite 管理多个笔记本，支持递归多级目录、可拖拽文件树、Markdown 编辑与渲染预览，并可生成类似 Hugo 的纯静态站点。

增强 Markdown 能力包括 GFM 表格、图片上传、围栏代码块、KaTeX 数学公式和 Mermaid 图表。可以导入单个 `.md` 文件或整个目录，并按原目录层级导出 ZIP。

## 开发

需要 Go 1.22+、Node.js 22+、pnpm 11+。

```bash
./dev.sh
```

脚本会同时启动 Go API 与前端开发服务器，并把前端的 `/api`、`/uploads`、`/site` 请求自动代理到后端。按 `Ctrl+C` 会同时停止两个进程。笔记保存在 `data/mdocman.db`，上传图片保存在 `data/uploads/`。

生产构建与清理：

```bash
./build.sh
./clean.sh
```

构建会生成 `dist/bin/mdoc`，其中已经内嵌预渲染首页和全部浏览器资源。
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

旧版 `data/notebooks.json` 会在 SQLite 数据库为空时自动迁移，迁移完成后以数据库为准。

## 命令行预览

构建后可直接预览一个 Markdown 文件或目录：

```bash
dist/bin/mdoc ./README.md
dist/bin/mdoc ./docs
```

命令会启动仅监听本机的临时服务并自动打开浏览器。目录优先显示其中的
`README.md` 或 `index.md`，否则显示可浏览的文件列表。Markdown 文件的 URL
保持原有目录结构，因此文档内指向其他本地 Markdown 文件的相对链接仍可继续
访问预览；图片等相对资源也会正常加载。可通过 `MDOC_PORT=8088` 指定端口，
通过 `MDOC_NO_BROWSER=1` 禁止自动打开浏览器。

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
