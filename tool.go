package opentool

import (
	"context"
	"encoding/json"
)

// Tool handles ordinary function calls. Streaming, specification loading and
// lifecycle hooks are independent optional interfaces.
type Tool interface {
	Call(ctx context.Context, name string, params json.RawMessage) (any, error)
}

// StreamTool adds streaming support. A server never emulates this method by
// calling Tool.Call; a request to /streamCall fails when the tool does not
// implement StreamTool.
type StreamTool interface {
	StreamCall(ctx context.Context, name string, params json.RawMessage, emit func(any) error) error
}

// SpecProvider optionally supplies the tool's OpenTool JSON description.
type SpecProvider interface {
	Load(ctx context.Context) (json.RawMessage, error)
}

// Initializer optionally initializes resources before the server starts.
type Initializer interface {
	Init(ctx context.Context) error
}

// Disposer optionally releases resources during server shutdown.
type Disposer interface {
	Dispose(ctx context.Context) error
}

// ArgsInitializer may inspect parsed command-line arguments and adjust the
// standard server settings before Init is called.
type ArgsInitializer interface {
	InitArgs(ctx context.Context, args *ParsedArgs) error
}
