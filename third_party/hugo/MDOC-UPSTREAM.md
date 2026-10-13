# Mdoc 中集成的 Hugo 源码

- 上游：<https://github.com/gohugoio/hugo>
- 版本：v0.130.0
- Commit：9b1b11c8a59a900458e9e460f197a44367c022ee
- Go Module ZIP SHA256（Go checksum 格式）：`h1:K4abLV4uru6YYLzq48kG5f3jXpTkPbDdBHIeZOixlhg=`
- 许可：Apache-2.0，见本目录 `LICENSE`；保留上游文件的版权声明。

此目录是上游模块的运行时源码和资源快照，保留 Go 包和嵌入模板，省略上游 `_test.go`、`testdata`、CI、文档及示例。主项目通过 `go.mod` 的本地 `replace` 编译此处代码，修改这些源码会直接改变 Mdoc 的站点生成行为。

Mdoc 的入口为 `internal/hugoengine/engine.go`，直接使用 `config/allconfig`、`modules`、`hugofs`、`hugolib`、`resources` 与模板引擎。Hugo 的 `commands` CLI 入口不参与 Mdoc 构建。

本地改动：

1. `config/allconfig.ConfigSourceDescriptor` 与 `modules.ClientConfig` 增加请求 Context，传入模块下载命令，支持取消和超时。
2. `hugolib.BuildCfg` 增加 Context，将它传入内容处理和页面组装；取消时终止后续渲染。
3. 主项目的 `build.sh`、`dev.sh`、`publish.sh` 使用 `extended` 构建标签，把 libsass 与扩展图片处理编译进 Mdoc。

升级时先准备新版本运行时源码，重放上述 Context 改动，再运行内置引擎和 Stack 发布集成测试；同步更新版本、commit 与 checksum 信息。
