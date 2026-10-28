package opentool

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Request is an OpenTool JSON-RPC request. ID and Params retain their original
// JSON representation to avoid coercion and floating-point precision loss.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// RPCError is the error object used by OpenTool JSON-RPC responses.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Response is an OpenTool JSON-RPC response. Error responses include an empty
// result object for compatibility with the other OpenTool SDKs.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result"`
	ID      json.RawMessage `json:"id"`
	Error   *RPCError       `json:"error,omitempty"`
}

func successResponse(result any, id json.RawMessage) Response {
	return Response{JSONRPC: JSONRPCVersion, Result: result, ID: cloneRaw(id)}
}

func errorResponse(err *RPCError, id json.RawMessage) Response {
	return Response{JSONRPC: JSONRPCVersion, Result: map[string]any{}, ID: cloneRaw(id), Error: err}
}

// ParseRequest validates an OpenTool JSON-RPC request. If the body is
// structurally invalid, the returned *RequestError carries any recoverable id.
func ParseRequest(data []byte) (Request, error) {
	var zero Request
	dec := json.NewDecoder(bytes.NewReader(data))
	var top json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return zero, &RequestError{Code: CodeParse, Message: err.Error()}
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return zero, &RequestError{Code: CodeInvalidRequest, Message: "request must contain exactly one JSON value"}
		}
		return zero, &RequestError{Code: CodeParse, Message: err.Error()}
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(top, &object); err != nil || object == nil {
		return zero, &RequestError{Code: CodeInvalidRequest, Message: "expected a JSON object"}
	}
	id, hasID := object["id"]
	requestErr := func(message string) error {
		return &RequestError{Code: CodeInvalidRequest, Message: message, ID: cloneRaw(id)}
	}
	if !hasID {
		return zero, requestErr("missing field 'id'")
	}

	version := JSONRPCVersion
	if rawVersion, ok := object["jsonrpc"]; ok {
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			return zero, requestErr("field 'jsonrpc' must be a string")
		}
	}
	var method string
	rawMethod, ok := object["method"]
	if !ok || json.Unmarshal(rawMethod, &method) != nil {
		return zero, requestErr("field 'method' must be a string")
	}
	if method == "" {
		return zero, requestErr("field 'method' must not be empty")
	}
	params := json.RawMessage(`{}`)
	if rawParams, ok := object["params"]; ok {
		params = cloneRaw(rawParams)
	}
	return Request{JSONRPC: version, Method: method, Params: params, ID: cloneRaw(id)}, nil
}

// RequestError describes a malformed request and preserves a recoverable id.
type RequestError struct {
	Code    int
	Message string
	ID      json.RawMessage
}

func (e *RequestError) Error() string { return e.Message }

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), raw...)
}
