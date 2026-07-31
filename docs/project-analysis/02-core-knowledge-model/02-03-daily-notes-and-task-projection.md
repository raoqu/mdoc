# 每日笔记与任务投影

## 1. 功能说明

每日笔记以约定 ID `daily-YYYY-MM-DD` 表示。Web 工作区、网页捕获和音频备忘都能按日期定位或创建它。任务不是独立数据库实体，而是从所有文档中符合 `+ [ ] 内容` 语法的 Markdown 行实时投影出来。

## 2. 职责边界

该子模块负责日期约定、日历导航、任务解析和原文回写，不负责提醒调度或独立任务存储。Git 同步只同步 Markdown，因此任务状态天然随正文同步。

## 3. 所属上级模块

[核心知识模型](./02-00-core-knowledge-model-overview.md)。

## 4. 对外接口

- `ensureDaily`、`appendDeepLinkLine`；
- `tasksIn`、`toggleTask`、`rescheduleTask`；
- `buildMonthGrid`、`dailyDatesFromDocuments`；
- CLI `today`；
- 捕获和音频模块的每日笔记创建逻辑。

## 5. 主要实现组成

- `ReflectWorkspace` 根据当前 `daily` 视图创建缺失的每日笔记。
- `DayCalendar` 与 `month-grid.ts` 构造按周补齐的月份网格。
- `tasksIn` 扫描文档行，继承每日笔记日期，并根据缩进维护面包屑。
- 显式 `[[YYYY-MM-DD]]` 覆盖每日笔记的隐含到期日。
- 勾选和改期通过文档 ID、行号与预期正文进行防陈旧校验。

## 6. 输入与输出

输入是当前日期、Markdown 文档集合和用户的勾选/改期操作；输出是每日文档、按逾期/今天/未来/未安排分组的任务视图，以及写回原 Markdown 的变更。

## 7. 处理流程

```text
导航到 daily(date)
→ ensureDaily 检查 daily-date
→ 必要时创建“每日笔记”目录和文档
→ ReflectEditor 编辑
→ tasksIn 扫描全部文档
→ 任务视图按日期分组
→ toggleTask/rescheduleTask 修改原始行
→ 单文档保存
```

浏览器深链的 `action=append|task` 在用户确认后把项目符号或任务行追加到当天笔记，并防止完全相同的行重复写入。

## 8. 依赖关系

上游是工作区导航、深链、浏览器捕获、音频备忘和 CLI；下游是笔记树持久化、浏览器日期 API 和 Markdown 解析纯函数。

## 9. 配置项

- `startWithBullet` 控制新每日笔记标题后是否自动出现项目符号。
- 日历以周一为每周第一天。
- 任务语法固定为以 `+ [ ]` 或 `+ [x]` 开头；其他 Markdown checkbox 形式不会进入任务投影。

## 10. 错误处理

任务所在行或内容已变化时，切换和改期函数抛出“任务已发生变化”并保留原文。每日笔记创建依赖目标笔记本有可用目录；捕获与音频后端在找不到目录时返回错误。

## 11. 扩展与修改建议

- 统一前端 `ensureDaily`、捕获 `capture` 和音频 `ensureDailyForAudio` 的创建逻辑，避免标题格式、目录选择和隐私行为分叉。
- 若支持同库多笔记本，为每日文档生成笔记本作用域内唯一 ID。
- 增加时区和跨日边界测试；当前日期取自各运行端的本地时间。
- 新增任务语法时保持行级回写的陈旧检查，避免改错正文。

## 12. 代码入口与调用链

```text
ReflectWorkspace view=daily
→ ensureDaily
→ addDocumentToFolder
→ mutateNotebooks
→ persist

任务视图
→ tasksIn
→ toggleDocumentTask / scheduleDocumentTask
→ toggleTask / rescheduleTask
→ persist
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `app/reflect/workspace.tsx` | `ensureDaily`、`appendDeepLinkLine`、`renderTasks` | 每日笔记、深链和任务 UI |
| `app/reflect/markdown.ts` | `tasksIn`、`toggleTask`、`rescheduleTask` | 任务投影与原文更新 |
| `app/reflect/month-grid.ts` | `buildMonthGrid`、`dailyDatesFromDocuments` | 日历数据模型 |
| `app/reflect/day-calendar.tsx` | `DayCalendar` | 每日笔记日历导航 |
| `cmd/mdocman/cli.go` | `runCLI` 的 `today` 分支 | 命令行读取今日笔记 |
| `cmd/mdocman/audio.go` | `ensureDailyForAudio` | 录音关联每日笔记 |
| `cmd/mdocman/capture.go` | `capture` | 网页内容写入每日笔记 |

