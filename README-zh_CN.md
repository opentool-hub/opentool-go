# OpenTool Go Server SDK

一个使用 Go 标准库实现的 OpenTool HTTP 服务端 SDK，可把 Go 工具暴露为兼容 OpenTool 的服务。

## 安装

```sh
go get github.com/opentool-hub/opentool-go
```

## 快速开始

普通工具只需实现 `Tool`：

```go
type EchoTool struct{}

func (EchoTool) Call(
    ctx context.Context,
    name string,
    params json.RawMessage,
) (any, error) {
    if name != "echo" {
        return nil, opentool.MethodNotFound(name)
    }
    var input map[string]any
    if err := json.Unmarshal(params, &input); err != nil {
        return nil, opentool.InvalidParams(err.Error())
    }
    return map[string]any{"echo": input["message"]}, nil
}
```

启动服务：

```go
server, err := opentool.NewServer(
    EchoTool{},
    opentool.WithSpecFile("opentool.json"),
    opentool.WithConfig(opentool.DefaultConfig()),
)
if err != nil {
    log.Fatal(err)
}
log.Fatal(server.ListenAndServe())
```

完整示例见 [`examples/mock-server`](examples/mock-server)。

## 可选能力

`StreamCall`、`Load`、`Init`、`Dispose` 和 `InitArgs` 分别由 `StreamTool`、`SpecProvider`、`Initializer`、`Disposer` 和 `ArgsInitializer` 接口提供。实现一个接口不会要求实现其他接口。

`StreamCall` 是独立能力。没有实现 `StreamTool` 时，`POST /streamCall` 返回 JSON-RPC `405` 错误；服务端不会调用 `Call` 代替流式调用。

流式方法通过 `emit` 同步发送一个或多个数据项。每次发送都会立即写入并 flush SSE 响应，因此工具可通过 `ctx.Done()` 感知客户端断开或服务关闭。

```go
func (t MyTool) StreamCall(
    ctx context.Context,
    name string,
    params json.RawMessage,
    emit func(any) error,
) error {
    for _, item := range t.items {
        if err := emit(item); err != nil {
            return err
        }
    }
    return nil
}
```

`SpecProvider.Load` 返回 OpenTool JSON 描述；也可以通过 `WithSpec`、`WithSpecFile` 或 `go:embed` 配置静态描述。SDK 校验 JSON 语法和 object 顶层，返回时保留原始字段及 `$ref`。

## HTTP 协议

默认地址为 `0.0.0.0:9627`，前缀为 `/opentool`，tag 为 `latest`。接口如下：

| 方法 | 路径 | 作用 |
|---|---|---|
| `GET` | `/opentool/version` | 返回 `{"version":"latest"}` |
| `GET` | `/opentool/load` | 返回 OpenTool JSON 描述 |
| `POST` | `/opentool/call` | 普通 JSON-RPC 调用 |
| `POST` | `/opentool/streamCall` | 可选 SSE 调用 |
| `POST` | `/opentool/stop` | 返回 stopped 并触发关闭 |

请求示例：

```json
{"jsonrpc":"2.0.0","method":"echo","params":{"message":"hello"},"id":"call-1"}
```

响应的 `id` 保留输入 JSON 类型。缺省 `params` 按 `{}` 处理。错误响应包含空对象 `result` 和 `{ "code": ..., "message": ... }`。工具业务错误仍通过 HTTP `200` 的 JSON-RPC envelope 返回；请求格式错误使用 HTTP `400`，未授权使用 `401`，请求体超限使用 `413`。

SSE 事件名为小写 `start`、`data`、`error` 和 `done`。成功时服务端在工具返回后发送 `done`；工具返回错误时发送 `error` 并结束，不再发送 `done`。

## 鉴权、配置与生命周期

配置一组或多组 API key 后，所有 OpenTool 路由要求：

```http
Authorization: Bearer your-key
```

```go
config := opentool.DefaultConfig()
config.Host = "127.0.0.1"
config.APIKeys = []string{"key-one", "key-two"}
server, err := opentool.NewServer(tool, opentool.WithConfig(config))
```

默认请求体上限为 16 MiB，可通过 `WithMaxRequestBodyBytes` 修改。`Handler()` 可挂载到现有 `net/http` 服务；使用工具生命周期钩子时，先显式调用 `Init(ctx)`，宿主关闭时调用 `Shutdown(ctx)`。SDK 独立监听时可用 `ListenAndServe()` 或 `Serve(listener)`，`/stop` 和 `Shutdown` 都会触发一次 `Dispose`。

库不会自动解析进程参数。要启用标准 CLI 参数，显式传入 `WithCLIArguments` 或 `WithProcessCLIArguments`。标准参数为 `--opentoolServerHost`、`--opentoolServerPort`、`--opentoolServerTag` 和可重复/逗号分隔的 `--opentoolServerApiKeys`。工具可通过 `CLIArguments.AddCustomOption` / `AddCustomMultiOption` 注册自己的参数；`ArgsInitializer` 可读取或调整解析结果。

Go HTTP 服务会并发调用工具。工具需要自行保护共享状态，并遵守 context 取消信号。

## 开发

```sh
gofmt -w .
go test ./...
go test -race ./...
```
