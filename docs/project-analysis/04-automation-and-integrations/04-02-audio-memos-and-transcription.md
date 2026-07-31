# 音频备忘与转录

## 1. 功能说明

音频模块在浏览器中录音，将媒体文件保存到本地工作区，把回链追加到当天笔记，并可调用 OpenAI 或 Google 生成独立的转录文档。

## 2. 职责边界

它负责录音记录、文件和转录状态，不负责通用 AI Chat。音频实际字节存储在文件系统，SQLite 只保存文件名和状态。

## 3. 所属上级模块

[自动化与外部集成](./04-00-automation-and-integrations-overview.md)。

## 4. 对外接口

- `GET/POST /api/audio-memos`；
- `POST /api/audio-memos/{id}/transcribe`；
- `/audio/{file}` 文件服务；
- `AudioMemoPanel` 的开始/停止录音、列表和转录操作。

## 5. 主要实现组成

- 浏览器 `MediaRecorder` 选择 WebM/Opus 或 Ogg/Opus，并显示音量动画与时长。
- 后端按 MIME 选择扩展名，把文件写入 `~/.mdoc/audio-memos/`。
- `ensureDailyForAudio` 查找/创建当日文档。
- `appendAudioBacklink` 在 `## [[Audio memos]]` 下幂等添加回链。
- `transcriptionProvider` 只接受 OpenAI 或 Google。
- 成功转录后创建 `audio-note-{memoId}` 文档并更新 memo 状态。

## 6. 输入与输出

输入是浏览器音频 Blob、笔记本 ID 和可选 provider ID；输出是本地媒体文件、`audio_memos` 行、每日笔记链接、转录 Markdown 文档及状态 JSON。

## 7. 处理流程

```text
navigator.mediaDevices.getUserMedia
→ MediaRecorder
→ multipart POST /api/audio-memos
→ 保存音频文件
→ 事务创建 memo + 每日笔记回链
→ 用户触发 transcribe
→ 私密检查 + Keychain
→ OpenAI multipart 或 Google inline_data
→ 创建转录文档
→ memo status=done
```

## 8. 依赖关系

上游是 `AudioMemoPanel`；下游是浏览器媒体 API、文件系统、SQLite、每日笔记模型、AI provider 配置和 OpenAI/Google 网络端点。

## 9. 配置项

- 音频上传请求最大 32 MiB。
- OpenAI 转录模型在后端固定为 `gpt-4o-mini-transcribe`。
- Google 若配置模型不以 `gemini-` 开头，会回退到 `gemini-2.5-flash`。
- 文件名包含本地日期和毫秒时间。

## 10. 错误处理

不支持的 MIME、缺失知识库、写文件或事务失败会拒绝上传；事务失败时尝试删除已写文件。转录前若每日笔记为私密，会把状态设为 `blocked`。供应商错误将状态设为 `failed` 并保存错误文本。

## 11. 扩展与修改建议

- 使用明确的状态机约束 `pending/transcribing/done/failed/blocked` 转换。
- 为网络转录增加统一超时和请求取消；OpenAI 当前使用默认 HTTP 客户端。
- 从 `CreatedAt[11:16]` 提取时间前先验证格式，避免异常旧数据导致切片 panic。
- 将每日笔记创建与隐私判断复用为公共服务。
- 若支持删除录音，应原子清理文件、memo、回链和转录文档。

## 12. 代码入口与调用链

```text
AudioMemoPanel.startRecording
→ MediaRecorder.onstop
→ POST /api/audio-memos
→ server.audioMemos
→ ensureDailyForAudio

AudioMemoPanel.transcribe
→ POST /api/audio-memos/{id}/transcribe
→ server.audioMemo
→ transcriptionProvider
→ transcribeOpenAI / transcribeGoogle
→ SQLite 事务
```

## 13. 关键源代码位置

| 路径 | 关键符号 | 作用 |
|---|---|---|
| `app/reflect/audio-memo.tsx` | `AudioMemoPanel` | 录音与转录 UI |
| `cmd/mdocman/audio.go` | `audioMemos` | 上传、文件和每日回链 |
| `cmd/mdocman/audio.go` | `ensureDailyForAudio` | 每日笔记保证 |
| `cmd/mdocman/audio.go` | `audioMemo` | 转录状态与文档创建 |
| `cmd/mdocman/audio.go` | `transcribeOpenAI`、`transcribeGoogle` | 供应商调用 |
| `cmd/mdocman/audio_test.go` | 音频辅助测试 | MIME 和回链幂等验证 |

