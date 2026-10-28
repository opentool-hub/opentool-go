package opentool

const (
	// JSONRPCVersion is the version string used by OpenTool JSON-RPC messages.
	JSONRPCVersion = "2.0.0"

	// DefaultPrefix is the default HTTP route prefix.
	DefaultPrefix = "/opentool"
	// DefaultHost is the default interface address.
	DefaultHost = "0.0.0.0"
	// DefaultPort is the default listener port.
	DefaultPort = 9627
	// DefaultToolTag is the default version returned by GET /version.
	DefaultToolTag = "latest"
	// DefaultMaxRequestBodyBytes is the default body limit for call endpoints.
	DefaultMaxRequestBodyBytes int64 = 16 << 20
)

const (
	// CLIArgumentTag is the standard command-line option for the server tag.
	CLIArgumentTag = "opentoolServerTag"
	// CLIArgumentHost is the standard command-line option for the bind host.
	CLIArgumentHost = "opentoolServerHost"
	// CLIArgumentPort is the standard command-line option for the bind port.
	CLIArgumentPort = "opentoolServerPort"
	// CLIArgumentAPIKeys is the repeatable command-line option for API keys.
	CLIArgumentAPIKeys = "opentoolServerApiKeys"
)

const (
	// EventStart marks the beginning of an SSE call.
	EventStart = "start"
	// EventData carries a streamed result.
	EventData = "data"
	// EventError carries a streamed JSON-RPC error.
	EventError = "error"
	// EventDone marks successful completion of an SSE call.
	EventDone = "done"
)
