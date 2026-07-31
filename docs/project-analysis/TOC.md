# 墨笺（Mdocman）项目结构与架构文档

- 项目名称：墨笺（Mdocman）
- 分析时间：2026-07-31
- 项目来源：`/Users/raoqu/mylab/mdoc`
- 分析范围：`full`
- 文档层级：2（项目 → 一级模块 → 二级子模块）
- 包含代码入口分析：是
- 包含外部依赖、数据流与部署分析：是
- 生成 Markdown 文档数量：19（含本总目录）

## 项目总览

1. [项目概述](./00-project-overview.md)
2. [整体架构](./01-architecture-overview.md)

## 02 核心知识模型

1. [核心知识模型总览](./02-core-knowledge-model/02-00-core-knowledge-model-overview.md)
2. [笔记层级与元数据](./02-core-knowledge-model/02-01-note-hierarchy-and-metadata.md)
3. [Markdown 编辑器与链接图谱](./02-core-knowledge-model/02-02-markdown-editor-and-link-graph.md)
4. [每日笔记与任务投影](./02-core-knowledge-model/02-03-daily-notes-and-task-projection.md)

## 03 应用接口层

1. [应用接口层总览](./03-application-interfaces/03-00-application-interfaces-overview.md)
2. [Web 工作区](./03-application-interfaces/03-01-web-workspace.md)
3. [HTTP API 与命令行接口](./03-application-interfaces/03-02-http-api-and-cli.md)
4. [路径预览与浏览器捕获](./03-application-interfaces/03-03-path-preview-and-browser-capture.md)

## 04 自动化与外部集成

1. [自动化与外部集成总览](./04-automation-and-integrations/04-00-automation-and-integrations-overview.md)
2. [AI 与资源增强](./04-automation-and-integrations/04-01-ai-and-asset-enrichment.md)
3. [音频备忘与转录](./04-automation-and-integrations/04-02-audio-memos-and-transcription.md)
4. [Git 备份与内容迁移](./04-automation-and-integrations/04-03-git-backup-and-content-transfer.md)

## 05 数据、发布与运行基础设施

1. [数据、发布与运行基础设施总览](./05-data-publication-and-runtime/05-00-data-publication-and-runtime-overview.md)
2. [SQLite 与本地文件存储](./05-data-publication-and-runtime/05-01-sqlite-and-local-files.md)
3. [渲染、静态站点与分享](./05-data-publication-and-runtime/05-02-rendering-static-site-and-sharing.md)
4. [构建、部署与测试](./05-data-publication-and-runtime/05-03-build-deployment-and-testing.md)

## 分析边界

本次分析以生产源码、构建脚本、配置和测试为依据。`node_modules/`、`.pnpm-store/`、`dist/`、`.wrangler/`、`public-site/`、`data/` 中的运行数据和构建产物未作为项目自有模块展开；`build/sites-vite-plugin.ts` 虽位于 `build/`，但被 `vite.config.ts` 直接导入，因此作为构建源码纳入分析。

