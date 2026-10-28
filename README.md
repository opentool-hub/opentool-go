# OpenTool Go Server SDK

A lightweight OpenTool-compatible HTTP server SDK implemented with the Go standard library.

## Install

```sh
go get github.com/opentool-hub/opentool-go
```

## Quick start

Implement `Tool` for ordinary calls:

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

```go
server, err := opentool.NewServer(
    EchoTool{},
    opentool.WithSpecFile("opentool.json"),
)
if err != nil {
    log.Fatal(err)
}
log.Fatal(server.ListenAndServe())
```

See [`examples/mock-server`](examples/mock-server) for a complete runnable server.

## Optional interfaces

Streaming, specification loading and lifecycle hooks are independent optional interfaces: `StreamTool`, `SpecProvider`, `Initializer`, `Disposer` and `ArgsInitializer`.

`StreamCall` is optional and independent from `Call`. If the tool does not implement `StreamTool`, `POST /streamCall` returns a JSON-RPC `405` error. The server never substitutes `Call` for streaming.

Streaming tools send zero or more values through `emit`. Each value is written and flushed as an SSE event. The call context is canceled when the client disconnects or the server shuts down.

`SpecProvider.Load` can return the OpenTool JSON description. Static descriptions can be configured using `WithSpec`, `WithSpecFile`, or `go:embed`. The SDK validates JSON syntax and requires an object at the top level, while retaining unknown fields and `$ref` values.

## HTTP protocol

Defaults are host `0.0.0.0`, port `9627`, prefix `/opentool`, and tag `latest`.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/opentool/version` | Return the server tag |
| `GET` | `/opentool/load` | Return the OpenTool description |
| `POST` | `/opentool/call` | JSON-RPC call |
| `POST` | `/opentool/streamCall` | Optional SSE call |
| `POST` | `/opentool/stop` | Acknowledge and begin shutdown |

Requests use `{"jsonrpc":"2.0.0","method":"echo","params":{},"id":"call-1"}`. The response preserves the JSON type of `id`; omitted params default to `{}`. Error responses include an empty `result` and an `{ "code": ..., "message": ... }` error. Tool errors use an HTTP `200` JSON-RPC response; malformed requests use `400`, authentication failures use `401`, and oversized request bodies use `413`.

SSE event names are lowercase: `start`, `data`, `error`, and `done`. The server emits `done` after a successful tool return. On tool failure, it emits `error` and closes the stream without `done`.

## Configuration and lifecycle

Pass API keys in `Config.APIKeys` to require `Authorization: Bearer <key>`. The default request-body limit is 16 MiB and can be changed with `WithMaxRequestBodyBytes`.

`Handler()` can be mounted in an existing `net/http` server. Call `Init(ctx)` before serving when using lifecycle hooks and `Shutdown(ctx)` when the host closes. For a standalone server, use `ListenAndServe()` or `Serve(listener)`. `/stop` and `Shutdown` invoke `Dispose` once.

The library does not parse process arguments implicitly. Use `WithCLIArguments` or `WithProcessCLIArguments` to enable `--opentoolServerHost`, `--opentoolServerPort`, `--opentoolServerTag`, and repeatable/comma-separated `--opentoolServerApiKeys`. Tool-owned options can be registered using `AddCustomOption` and `AddCustomMultiOption`.

The Go HTTP server invokes tools concurrently. Tool implementations are responsible for synchronizing shared state and observing context cancellation.

## Development

```sh
gofmt -w .
go test ./...
go test -race ./...
```
