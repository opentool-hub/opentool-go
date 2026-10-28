package opentool

import (
	"errors"
	"fmt"
)

const (
	CodeInvalidRequest = 400
	CodeInvalidParams  = 400
	CodeMethodNotFound = 405
	CodeExecution      = 500
	CodeParse          = -32700
	CodeAuthentication = 401
	CodeUnavailable    = 503
	CodeNotConfigured  = 404
)

// ToolError is a structured error that the server sends in a JSON-RPC error
// object. Cause is retained for errors.Is and errors.As.
type ToolError struct {
	Code    int
	Message string
	Cause   error
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "tool error"
}

func (e *ToolError) Unwrap() error { return e.Cause }

func InvalidRequest(message string) *ToolError {
	return &ToolError{Code: CodeInvalidRequest, Message: message}
}
func InvalidParams(message string) *ToolError {
	return &ToolError{Code: CodeInvalidParams, Message: message}
}
func MethodNotFound(name string) *ToolError {
	return &ToolError{Code: CodeMethodNotFound, Message: fmt.Sprintf("Function Not Supported: %s", name)}
}
func ExecutionError(err error) *ToolError {
	if err == nil {
		return &ToolError{Code: CodeExecution, Message: "tool execution failed"}
	}
	return &ToolError{Code: CodeExecution, Message: err.Error(), Cause: err}
}
func StreamNotSupported() *ToolError {
	return &ToolError{Code: CodeMethodNotFound, Message: "streamCall is not supported by this tool"}
}

func asRPCError(err error) *RPCError {
	var toolErr *ToolError
	if errors.As(err, &toolErr) && toolErr != nil {
		code := toolErr.Code
		if code == 0 {
			code = CodeExecution
		}
		return &RPCError{Code: code, Message: toolErr.Error()}
	}
	message := "tool execution failed"
	if err != nil {
		message = err.Error()
	}
	return &RPCError{Code: CodeExecution, Message: message}
}
